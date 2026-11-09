package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchHomeLongestPrefixAndBoundary(t *testing.T) {
	m := &Map{Projects: []Project{
		{Dir: `C:\proj\prj1`, Home: "home1"},
		{Dir: `C:\proj\prj10`, Home: "home2"},
		{Dir: `C:\proj`, Home: "home0"},
		{Dir: `C:\proj\prj1\sub`, Home: "home3"},
	}}
	cases := map[string]string{
		`C:\proj\prj1`:       "home1",
		`c:\PROJ\prj1\src`:   "home1",
		`C:\proj\prj10\a`:    "home2",
		`C:\proj\prj1\sub\x`: "home3",
		`C:\proj\prj2`:       "home0",
		`\\?\C:\proj\prj1\`:  "home1",
		`C:/proj/prj10`:      "home2",
	}
	for dir, want := range cases {
		pr, ok := m.MatchHome(dir, true)
		if !ok || pr.Home != want {
			t.Errorf("%s: 得到 %v %v，期望 %s", dir, pr.Home, ok, want)
		}
	}
	if _, ok := m.MatchHome(`N:\other`, true); ok {
		t.Error("不相关目录不应匹配")
	}
	// 区分大小写的平台
	if _, ok := m.MatchHome(`c:\PROJ\prj1`, false); ok {
		t.Error("大小写敏感时不应匹配")
	}
}

func TestUnderDirRoot(t *testing.T) {
	if !UnderDir(`C:\a`, `C:\`, true) || !UnderDir(`/a/b`, `/`, false) {
		t.Error("根目录前缀应匹配")
	}
	if UnderDir(`/ab`, `/a`, false) {
		t.Error("/ab 不在 /a 下")
	}
}

func TestMapRoundTripAndSources(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	m, err := LoadMap(p)
	if err != nil || len(m.Homes) != 0 {
		t.Fatal(err)
	}
	m.SetProject("/w/prj1", "home1")
	m.SetProject("/w/prj1", "home2") // 覆盖
	m.Homes["home2"] = HomeEntry{Source: "plus1"}
	if err := SaveMap(p, m); err != nil {
		t.Fatal(err)
	}
	m2, err := LoadMap(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.Projects) != 1 || m2.Projects[0].Home != "home2" || m2.Homes["home2"].Source != "plus1" {
		t.Fatalf("回读不一致：%+v", m2)
	}
	if got := m2.HomesUsingSource("plus1"); len(got) != 1 || got[0] != "home2" {
		t.Fatal(got)
	}
	if err := SaveSource(p, "x1", &SourceMeta{Type: "custom", BaseURL: "https://a", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSource(p, "x1")
	if err != nil || s.Provider() != ProviderCustom {
		t.Fatal(s, err)
	}
	if _, err := LoadSource(p, "nope"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatal(err)
	}
	names, _ := ListSources(p)
	if len(names) != 1 || names[0] != "x1" {
		t.Fatal(names)
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"plus1", "xx-api_2", "a.b"} {
		if ValidName("s", ok) != nil {
			t.Error(ok)
		}
	}
	for _, bad := range []string{"", "../x", "a/b", "-x", `a\b`} {
		if ValidName("s", bad) == nil {
			t.Error(bad)
		}
	}
}

func TestChildEnv(t *testing.T) {
	base := []string{"PATH=/bin", "OPENAI_API_KEY=leak", "CODEX_API_KEY=leak2", "CODEX_HOME=/old", "KEEP=1"}
	env := ChildEnv(base, DropEnv, map[string]string{"CODEX_HOME": "/new", KeyEnv: "k"})
	j := strings.Join(env, "\n")
	if strings.Contains(j, "leak") || strings.Contains(j, "/old") {
		t.Fatal(j)
	}
	for _, want := range []string{"PATH=/bin", "KEEP=1", "CODEX_HOME=/new", "ACSW_API_KEY=k"} {
		if !strings.Contains(j, want) {
			t.Fatal("缺少", want)
		}
	}
}

func TestLock(t *testing.T) {
	p := Paths{Root: filepath.Join(t.TempDir(), "r")}
	u, err := Lock(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	u()
}
