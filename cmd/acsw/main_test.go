package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"acsw/internal/core"
	"acsw/internal/exitcode"
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

func TestFailExitCodes(t *testing.T) {
	cases := []struct {
		err  error
		want exitcode.Code
	}{
		{core.Usage("用法：…"), exitcode.BadArgs},
		{errors.New("别的错"), exitcode.Other},
		{exitcode.With(exitcode.NotMapped, core.Usage("当前目录没有映射")), exitcode.NotMapped},
		{fmt.Errorf("包一层：%w", exitcode.With(exitcode.KeyUnreadable, errors.New("k"))), exitcode.KeyUnreadable},
	}
	for _, c := range cases {
		var buf strings.Builder
		got := failTo(&buf, c.err)
		if got != c.want.Exit() || !strings.HasPrefix(buf.String(), "acsw 错误：") {
			t.Fatal(c.err, got, buf.String())
		}
		if runtime.GOOS == "windows" && got != int(c.want) {
			t.Fatal("windows 上应是大码", got)
		}
	}
	if _, _, _, err := parseArgs([]string{"--nope"}, nil, nil); codeOf(err) != exitcode.BadArgs {
		t.Fatal("参数错误应是 BadArgs")
	}
}
