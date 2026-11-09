// Package codexdb 读写 Codex 的状态库 state_*.sqlite：备份、批量改 model_provider、查最近会话。
//
// 注意：Codex 状态库的表结构没有公开承诺，这里的列名是按 0.167 实测和源码推断的，
// 读之前一律先用 PRAGMA table_info 探测，缺列时降级或跳过。
package codexdb

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DefaultDBName 是 Codex 0.167 的状态库文件名。
const DefaultDBName = "state_5.sqlite"

var stateRe = regexp.MustCompile(`^state_(\d+)\.sqlite$`)

// FindStateDB 返回 home 下的状态库路径：优先 state_5.sqlite，否则取编号最大的 state_N.sqlite。
// 都不存在时返回 state_5.sqlite 的路径和 false。
func FindStateDB(home string) (string, bool) {
	def := filepath.Join(home, DefaultDBName)
	if fileExists(def) {
		return def, true
	}
	ents, _ := os.ReadDir(home)
	best, bestN := "", -1
	for _, e := range ents {
		m := stateRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if n > bestN {
			best, bestN = filepath.Join(home, e.Name()), n
		}
	}
	if best != "" {
		return best, true
	}
	return def, false
}

// DBFiles 返回状态库及其 -wal/-shm 文件路径。
func DBFiles(db string) []string { return []string{db, db + "-wal", db + "-shm"} }

func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }

func open(db string) (*sql.DB, error) {
	d, err := sql.Open("sqlite", "file:"+filepath.ToSlash(db)+"?_pragma=busy_timeout(3000)")
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	return d, nil
}

// Columns 返回表的列名集合；表不存在返回 nil。
func Columns(d *sql.DB, table string) (map[string]bool, error) {
	rows, err := d.Query(`PRAGMA table_info(` + quoteIdent(table) + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   sql.NullString
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[strings.ToLower(name)] = true
	}
	if len(cols) == 0 {
		return nil, rows.Err()
	}
	return cols, rows.Err()
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// RetagResult 是批量改 provider 的结果。
type RetagResult struct {
	Skipped bool   // 库或表或列不存在而跳过
	Reason  string // 跳过原因
	Changed int64
}

// CountToRetag 统计需要改的行数（dry-run 用）。
func CountToRetag(db, provider string) (RetagResult, error) {
	return retag(db, provider, true)
}

// Retag 执行 UPDATE threads SET model_provider=? WHERE model_provider<>?（在事务里）。
func Retag(db, provider string) (RetagResult, error) { return retag(db, provider, false) }

func retag(db, provider string, dry bool) (RetagResult, error) {
	if !fileExists(db) {
		return RetagResult{Skipped: true, Reason: "状态库不存在"}, nil
	}
	d, err := open(db)
	if err != nil {
		return RetagResult{}, fmt.Errorf("打开状态库失败：%w", err)
	}
	defer d.Close()
	cols, err := Columns(d, "threads")
	if err != nil {
		return RetagResult{}, fmt.Errorf("读 threads 表结构失败：%w", err)
	}
	if cols == nil {
		return RetagResult{Skipped: true, Reason: "没有 threads 表"}, nil
	}
	if !cols["model_provider"] {
		return RetagResult{Skipped: true, Reason: "threads 表没有 model_provider 列"}, nil
	}
	if dry {
		var n int64
		err := d.QueryRow(`SELECT COUNT(*) FROM threads WHERE model_provider IS NULL OR model_provider <> ?`, provider).Scan(&n)
		return RetagResult{Changed: n}, err
	}
	tx, err := d.Begin()
	if err != nil {
		return RetagResult{}, fmt.Errorf("开事务失败（可能有 codex 正在用这个库）：%w", err)
	}
	res, err := tx.Exec(`UPDATE threads SET model_provider = ? WHERE model_provider IS NULL OR model_provider <> ?`, provider, provider)
	if err != nil {
		_ = tx.Rollback()
		return RetagResult{}, fmt.Errorf("改 model_provider 失败：%w", err)
	}
	if err := tx.Commit(); err != nil {
		return RetagResult{}, fmt.Errorf("提交失败：%w", err)
	}
	n, _ := res.RowsAffected()
	// 把 WAL 合并回主库，便于之后备份/复制。
	_, _ = d.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return RetagResult{Changed: n}, nil
}

// Thread 是最近会话的一条显示记录。
type Thread struct {
	ID      string
	Updated time.Time
	Title   string
	Cwd     string
	From    string // "db" 或 "rollout"
}

// SamePathFunc 用于比较 cwd，由调用方提供（处理大小写和分隔符）。
type SamePathFunc func(a, b string) bool

// Recent 返回 cwd 下最近的会话（最多 limit 条）。库结构不够时降级扫 sessions 目录。
func Recent(home, cwd string, limit int, same SamePathFunc) ([]Thread, string, error) {
	db, ok := FindStateDB(home)
	if ok {
		ts, note, err := recentFromDB(db, cwd, limit, same)
		if err == nil && note == "" {
			return ts, "", nil
		}
		if err != nil {
			note = "读状态库失败：" + err.Error()
		}
		ts2, err2 := recentFromRollouts(home, cwd, limit, same)
		return ts2, note + "；已改为扫描 sessions 目录", err2
	}
	ts, err := recentFromRollouts(home, cwd, limit, same)
	return ts, "没有状态库，已改为扫描 sessions 目录", err
}

func pick(cols map[string]bool, names ...string) string {
	for _, n := range names {
		if cols[n] {
			return n
		}
	}
	return ""
}

func recentFromDB(db, cwd string, limit int, same SamePathFunc) ([]Thread, string, error) {
	d, err := open(db)
	if err != nil {
		return nil, "", err
	}
	defer d.Close()
	cols, err := Columns(d, "threads")
	if err != nil {
		return nil, "", err
	}
	if cols == nil {
		return nil, "状态库里没有 threads 表", nil
	}
	idC := pick(cols, "id", "thread_id")
	cwdC := pick(cols, "cwd")
	tC := pick(cols, "updated_at", "updated_at_ms", "last_updated_at", "created_at")
	titleC := pick(cols, "title", "name", "first_user_message", "preview")
	if idC == "" || cwdC == "" || tC == "" {
		return nil, "threads 表缺少 id/cwd/时间列", nil
	}
	titleExpr := "''"
	if titleC != "" {
		titleExpr = quoteIdent(titleC)
	}
	q := fmt.Sprintf(`SELECT %s, %s, %s, %s FROM threads ORDER BY %s DESC`,
		quoteIdent(idC), quoteIdent(cwdC), quoteIdent(tC), titleExpr, quoteIdent(tC))
	rows, err := d.Query(q)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []Thread
	for rows.Next() {
		var id, c, title sql.NullString
		var t any
		if err := rows.Scan(&id, &c, &t, &title); err != nil {
			return nil, "", err
		}
		if !same(c.String, cwd) {
			continue
		}
		out = append(out, Thread{ID: id.String, Cwd: c.String, Updated: parseTime(t), Title: oneLine(title.String), From: "db"})
		if len(out) >= limit {
			break
		}
	}
	return out, "", rows.Err()
}

// parseTime 兼容秒、毫秒、文本时间。
func parseTime(v any) time.Time {
	switch x := v.(type) {
	case int64:
		return epoch(x)
	case float64:
		return epoch(int64(x))
	case []byte:
		return parseTime(string(x))
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return epoch(n)
		}
		for _, l := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
			if t, err := time.Parse(l, x); err == nil {
				return t
			}
		}
	case time.Time:
		return x
	}
	return time.Time{}
}

func epoch(n int64) time.Time {
	if n > 1e12 {
		return time.UnixMilli(n)
	}
	return time.Unix(n, 0)
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 60 {
		s = string(r[:60]) + "…"
	}
	return s
}

// recentFromRollouts 扫 sessions/**/rollout-*.jsonl，读首行 session_meta。
func recentFromRollouts(home, cwd string, limit int, same SamePathFunc) ([]Thread, error) {
	var out []Thread
	root := filepath.Join(home, "sessions")
	err := filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipAll
			}
			return nil
		}
		if de.IsDir() || !strings.HasPrefix(de.Name(), "rollout-") || !strings.HasSuffix(de.Name(), ".jsonl") {
			return nil
		}
		t, ok := readSessionMeta(p)
		if !ok || !same(t.Cwd, cwd) {
			return nil
		}
		if info, err := de.Info(); err == nil {
			t.Updated = info.ModTime()
		}
		out = append(out, t)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, err
}

func readSessionMeta(p string) (Thread, bool) {
	f, err := os.Open(p)
	if err != nil {
		return Thread{}, false
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Thread{}, false
	}
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			ID  string `json:"id"`
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &rec) != nil || rec.Type != "session_meta" || rec.Payload.ID == "" {
		return Thread{}, false
	}
	return Thread{ID: rec.Payload.ID, Cwd: rec.Payload.Cwd, From: "rollout"}, true
}
