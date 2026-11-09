package main

import (
	"errors"
	"strings"
	"testing"

	"acsw/internal/core"
)

func TestParseArgs(t *testing.T) {
	f, pos, after, err := parseArgs([]string{"add", "x1", "--type", "custom", "--base-url=https://a", "--key-stdin", "--", "-m", "y"},
		set("type", "base-url"), set("key-stdin"))
	if err != nil || f["type"] != "custom" || f["base-url"] != "https://a" || f["key-stdin"] != "1" ||
		strings.Join(pos, ",") != "add,x1" || strings.Join(after, ",") != "-m,y" {
		t.Fatal(f, pos, after, err)
	}
	if _, _, _, err := parseArgs([]string{"--nope"}, nil, nil); err == nil {
		t.Fatal("未知参数应报错")
	}
	if _, _, _, err := parseArgs([]string{"--type"}, set("type"), nil); err == nil {
		t.Fatal("缺值应报错")
	}
}

func TestParseCodexArgs(t *testing.T) {
	h, e, args, err := parseCodexArgs([]string{"--home", "home2", "--expect-home=home2", "--", "resume", "abc"})
	if err != nil || h != "home2" || e != "home2" || strings.Join(args, ",") != "resume,abc" {
		t.Fatal(h, e, args, err)
	}
	h, e, args, err = parseCodexArgs([]string{"--version"})
	if err != nil || h != "" || e != "" || strings.Join(args, ",") != "--version" {
		t.Fatal(h, e, args, err)
	}
	h, e, args, err = parseCodexArgs([]string{"--home", "home1", "exec", "--help"})
	if err != nil || h != "home1" || e != "" || strings.Join(args, ",") != "exec,--help" {
		t.Fatal(h, e, args, err)
	}
	h, e, args, err = parseCodexArgs([]string{"--expect-home", "home2", "-m", "gpt-x"})
	if err != nil || h != "" || e != "home2" || strings.Join(args, ",") != "-m,gpt-x" {
		t.Fatal(h, e, args, err)
	}
	if _, _, _, err := parseCodexArgs([]string{"--home"}); err == nil {
		t.Fatal("缺值应报错")
	}
	if _, _, _, err := parseCodexArgs([]string{"--expect-home"}); err == nil {
		t.Fatal("缺值应报错")
	}
	_, _, args, err = parseCodexArgs(nil)
	if err != nil || args != nil {
		t.Fatal(args, err)
	}
}

func TestFailUsageExitCode(t *testing.T) {
	if code := fail(core.Usage("当前目录没有映射，先运行 acsw map <home>")); code != 2 {
		t.Fatal(code)
	}
	if code := fail(errors.New("别的错")); code != 1 {
		t.Fatal(code)
	}
}
