package codexdb

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// MakeFakeDB 建一个假的状态库。注意：只是模拟，真实 Codex 表结构以目标机器实测为准。
func makeFakeDB(t *testing.T, path string, rows [][]any) {
	t.Helper()
	d, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	stmts := []string{`PRAGMA journal_mode=WAL`, `CREATE TABLE threads(id TEXT PRIMARY KEY, model_provider TEXT, cwd TEXT, updated_at INTEGER, title TEXT)`}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range rows {
		if _, err := d.Exec(`INSERT INTO threads VALUES(?,?,?,?,?)`, r...); err != nil {
			t.Fatal(err)
		}
	}
}

func providers(t *testing.T, path string) map[string]string {
	d, _ := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	defer d.Close()
	rs, err := d.Query(`SELECT id, model_provider FROM threads`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	out := map[string]string{}
	for rs.Next() {
		var a, b string
		rs.Scan(&a, &b)
		out[a] = b
	}
	return out
}

func TestRetag(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, DefaultDBName)
	makeFakeDB(t, db, [][]any{{"a", "openai", "/p", 1, "t"}, {"b", "api1", "/p", 2, "t"}, {"c", "custom", "/p", 3, "t"}})
	r, err := CountToRetag(db, "custom")
	if err != nil || r.Changed != 2 {
		t.Fatal(r, err)
	}
	r, err = Retag(db, "custom")
	if err != nil || r.Changed != 2 {
		t.Fatal(r, err)
	}
	for id, p := range providers(t, db) {
		if p != "custom" {
			t.Error(id, p)
		}
	}
}

func TestRetagSkips(t *testing.T) {
	dir := t.TempDir()
	r, err := Retag(filepath.Join(dir, "none.sqlite"), "openai")
	if err != nil || !r.Skipped {
		t.Fatal(r, err)
	}
	db := filepath.Join(dir, DefaultDBName)
	d, _ := sql.Open("sqlite", "file:"+filepath.ToSlash(db))
	d.Exec(`CREATE TABLE other(x)`)
	d.Close()
	r, err = Retag(db, "openai")
	if err != nil || !r.Skipped {
		t.Fatal(r, err)
	}
}

func TestFindStateDB(t *testing.T) {
	dir := t.TempDir()
	if _, ok := FindStateDB(dir); ok {
		t.Fatal("空目录不应找到")
	}
	os.WriteFile(filepath.Join(dir, "state_7.sqlite"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "state_6.sqlite"), nil, 0o644)
	if p, ok := FindStateDB(dir); !ok || filepath.Base(p) != "state_7.sqlite" {
		t.Fatal(p)
	}
}

var same = func(a, b string) bool { return a == b }

func TestRecentFromDB(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, DefaultDBName)
	var rows [][]any
	now := time.Now().Unix()
	for i := 0; i < 8; i++ {
		rows = append(rows, []any{string(rune('a' + i)), "openai", "/p1", now + int64(i), "标题\n第二行"})
	}
	rows = append(rows, []any{"z", "openai", "/p2", now + 100, "别的项目"})
	makeFakeDB(t, db, rows)
	ts, note, err := Recent(dir, "/p1", 5, same)
	if err != nil || note != "" {
		t.Fatal(err, note)
	}
	if len(ts) != 5 || ts[0].ID != "h" || ts[0].Title != "标题 第二行" || ts[0].Updated.Unix() != now+7 {
		t.Fatalf("%+v", ts)
	}
}

func TestRecentFallbackRollouts(t *testing.T) {
	dir := t.TempDir()
	// 库里缺 cwd 列 → 降级
	db := filepath.Join(dir, DefaultDBName)
	d, _ := sql.Open("sqlite", "file:"+filepath.ToSlash(db))
	d.Exec(`CREATE TABLE threads(id TEXT, model_provider TEXT)`)
	d.Close()
	sdir := filepath.Join(dir, "sessions", "2026", "11", "09")
	os.MkdirAll(sdir, 0o755)
	write := func(name, id, cwd string, mt time.Time) {
		p := filepath.Join(sdir, name)
		os.WriteFile(p, []byte(`{"timestamp":"x","type":"session_meta","payload":{"id":"`+id+`","cwd":"`+cwd+`"}}`+"\n{}\n"), 0o644)
		os.Chtimes(p, mt, mt)
	}
	base := time.Now().Add(-time.Hour)
	write("rollout-1.jsonl", "id1", "/p1", base)
	write("rollout-2.jsonl", "id2", "/p1", base.Add(time.Minute))
	write("rollout-3.jsonl", "id3", "/p2", base.Add(2*time.Minute))
	os.WriteFile(filepath.Join(sdir, "rollout-bad.jsonl"), []byte("not json\n"), 0o644)
	ts, note, err := Recent(dir, "/p1", 5, same)
	if err != nil || note == "" {
		t.Fatal(err, note)
	}
	if len(ts) != 2 || ts[0].ID != "id2" || ts[0].From != "rollout" {
		t.Fatalf("%+v", ts)
	}
}

func TestParseTime(t *testing.T) {
	if parseTime(int64(1700000000000)).Unix() != 1700000000 {
		t.Error("毫秒")
	}
	if parseTime("1700000000").Unix() != 1700000000 {
		t.Error("文本秒")
	}
	if parseTime("2026-11-09T10:00:00Z").IsZero() {
		t.Error("RFC3339")
	}
}
