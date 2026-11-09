package source

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"acsw/internal/config"
	"acsw/internal/cred"
	tu "acsw/internal/testutil"
)

type failStore struct{ *cred.Memory }

func (failStore) Set(string, string) error { return errors.New("模拟写凭据失败") }

func TestEditCustom(t *testing.T) {
	p := tu.Fixture(t)
	st := cred.NewMemory()
	tu.Must(t, st.Set(cred.Target("api1"), "old-key"))

	// 什么都不填 = 不改
	r, err := EditCustom(p, st, "api1", "", "", "")
	tu.Must(t, err)
	if r.MetaChanged || r.KeyChanged {
		t.Fatal(r)
	}
	// 只改 key
	r, err = EditCustom(p, st, "api1", "", "", " new-key ")
	tu.Must(t, err)
	if r.MetaChanged || !r.KeyChanged {
		t.Fatal(r)
	}
	if v, _ := st.Get(cred.Target("api1")); v != "new-key" {
		t.Fatal("key 没更新")
	}
	// 改 base_url/model
	r, err = EditCustom(p, st, "api1", "https://new.example/v2/", "gpt-y", "")
	tu.Must(t, err)
	m, _ := config.LoadSource(p, "api1")
	if !r.MetaChanged || r.KeyChanged || m.BaseURL != "https://new.example/v2" || m.Model != "gpt-y" {
		t.Fatal(r, m)
	}
	if strings.Contains(tu.Read(p.SourceMetaFile("api1")), "new-key") {
		t.Fatal("key 不应写进文件")
	}
	// 非法输入
	for _, c := range [][2]string{{"ftp://x", ""}, {"not a url", ""}, {"", "bad\nmodel"}} {
		if _, err := EditCustom(p, st, "api1", c[0], c[1], ""); err == nil {
			t.Fatal("应拒绝", c)
		}
	}
	// openai 类型不能改
	if _, err := EditCustom(p, st, "plus1", "https://x.example", "m", ""); err == nil || !strings.Contains(err.Error(), "custom") {
		t.Fatal(err)
	}
	// 不存在
	if _, err := EditCustom(p, st, "nope", "", "", ""); err == nil {
		t.Fatal("不存在应报错")
	}
	// key 写失败：元数据不变
	if _, err := EditCustom(p, failStore{cred.NewMemory()}, "api1", "https://other.example", "", "k"); err == nil {
		t.Fatal("应报错")
	}
	if m, _ := config.LoadSource(p, "api1"); m.BaseURL != "https://new.example/v2" {
		t.Fatal("key 写失败时不应改元数据", m.BaseURL)
	}
}

func TestRemoveCustom(t *testing.T) {
	p := tu.Fixture(t)
	st := cred.NewMemory()
	tu.Must(t, st.Set(cred.Target("api1"), "k1"))
	tu.Must(t, st.Set(cred.Target("api2"), "k2"))
	m, _ := config.LoadMap(p)
	m.Homes["home2"] = config.HomeEntry{Source: "api1"}

	_, err := RemoveCustom(p, st, m, "api1", time.Now())
	if !errors.Is(err, ErrInUse) || !strings.Contains(err.Error(), "home2") {
		t.Fatal(err)
	}
	if _, err := RemoveCustom(p, st, m, "plus1", time.Now()); err == nil {
		t.Fatal("openai 不能在这里删")
	}
	dir, err := RemoveCustom(p, st, m, "api2", time.Now())
	tu.Must(t, err)
	if config.Exists(p.SourceDir("api2")) || !config.Exists(filepath.Join(dir, "acsw-source.toml")) {
		t.Fatal("目录没移到备份区", dir)
	}
	if filepath.Dir(dir) != p.BackupDir() {
		t.Fatal(dir)
	}
	if _, err := st.Get(cred.Target("api2")); !errors.Is(err, cred.ErrNotFound) {
		t.Fatal("凭据没删")
	}
	if _, err := st.Get(cred.Target("api1")); err != nil {
		t.Fatal("误删了别的凭据")
	}
	names, _ := config.ListSources(p)
	if strings.Join(names, ",") != "api1,plus1,pro1" {
		t.Fatal(names)
	}
}
