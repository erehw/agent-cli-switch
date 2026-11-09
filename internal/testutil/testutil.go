// Package testutil 只给单测用：造假的 auth.json、假的状态库和目录结构。
// 假库结构只是模拟，不代表真实 Codex 状态库。
package testutil

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"acsw/internal/config"
)

// FakeAuth 返回一个假的 ChatGPT auth.json（不含真实 token）。
func FakeAuth(account, marker string) []byte {
	return []byte(`{"OPENAI_API_KEY":null,"tokens":{"id_token":"fake-id-` + marker + `","access_token":"fake-access-` + marker + `","refresh_token":"fake-refresh-` + marker + `","account_id":"` + account + `"},"last_refresh":"2026-11-09T00:00:00Z"}`)
}

// MakeDB 建一个含 threads(id, model_provider, cwd, updated_at, title) 的假库（WAL 模式）。
func MakeDB(t *testing.T, path string, rows [][]any) {
	t.Helper()
	d, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, s := range []string{`PRAGMA journal_mode=WAL`, `CREATE TABLE threads(id TEXT PRIMARY KEY, model_provider TEXT, cwd TEXT, updated_at INTEGER, title TEXT)`} {
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

// Providers 读出 id→model_provider。
func Providers(t *testing.T, path string) map[string]string {
	t.Helper()
	d, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	rs, err := d.Query(`SELECT id, model_provider FROM threads`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	out := map[string]string{}
	for rs.Next() {
		var a, b string
		if err := rs.Scan(&a, &b); err != nil {
			t.Fatal(err)
		}
		out[a] = b
	}
	return out
}

// Fixture 建好 source：plus1(openai)、pro1(openai)、api1(custom)、api2(custom)，以及 home1、home2。
func Fixture(t *testing.T) config.Paths {
	t.Helper()
	p := config.Paths{Root: t.TempDir()}
	for _, s := range []struct{ name, acct string }{{"plus1", "acct-plus"}, {"pro1", "acct-pro"}} {
		Must(t, os.MkdirAll(p.SourceDir(s.name), 0o700))
		Must(t, os.WriteFile(filepath.Join(p.SourceDir(s.name), "auth.json"), FakeAuth(s.acct, s.name+"-v1"), 0o600))
		Must(t, config.SaveSource(p, s.name, &config.SourceMeta{Type: "openai"}))
	}
	for _, n := range []string{"api1", "api2"} {
		Must(t, os.MkdirAll(p.SourceDir(n), 0o700))
		Must(t, config.SaveSource(p, n, &config.SourceMeta{Type: "custom", BaseURL: "https://" + n + ".example/v1", Model: "gpt-x"}))
	}
	m := &config.Map{Homes: map[string]config.HomeEntry{}}
	for _, h := range []string{"home1", "home2"} {
		Must(t, os.MkdirAll(p.HomeDir(h), 0o700))
		m.Homes[h] = config.HomeEntry{}
	}
	Must(t, config.SaveMap(p, m))
	return p
}

// Must 出错即失败。
func Must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Read 读文件，不存在返回空串。
func Read(path string) string { b, _ := os.ReadFile(path); return string(b) }
