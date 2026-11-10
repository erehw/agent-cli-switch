package main

import (
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	old := version
	defer func() { version = old }()
	for in, want := range map[string]string{
		"v0.1.1":                  "0.1.1",
		"0.1.1":                   "0.1.1",
		"v0.1.1-3-gabc1234-dirty": "0.1.1-3-gabc1234-dirty",
		"abc1234":                 "abc1234",
	} {
		version = in
		if got := versionString(); got != want {
			t.Fatalf("%q → %q，应为 %q", in, got, want)
		}
	}
	// 没注入：退回 Go 记录的模块版本，没有就是 dev；都不带 v 前缀
	version = "dev"
	if got := versionString(); got == "" || strings.HasPrefix(got, "v") {
		t.Fatal(got)
	}
}
