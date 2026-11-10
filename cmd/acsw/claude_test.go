package main

import (
	"strings"
	"testing"
)

func TestParseClaudeArgs(t *testing.T) {
	cases := []struct {
		in     []string
		mode   string
		strict bool
		rest   string
	}{
		{nil, "", false, ""},
		{[]string{"--key-mode", "plain", "--strict", "-p", "hi"}, "plain", true, "-p|hi"},
		{[]string{"--key-mode=helper", "--", "--strict"}, "helper", false, "--strict"},
		{[]string{"-p", "--strict"}, "", false, "-p|--strict"},
		{[]string{"resume", "--key-mode", "x"}, "", false, "resume|--key-mode|x"},
	}
	for _, c := range cases {
		m, s, r, err := parseClaudeArgs(c.in)
		if err != nil || m != c.mode || s != c.strict || strings.Join(r, "|") != c.rest {
			t.Errorf("%q → %q %v %q %v", c.in, m, s, r, err)
		}
	}
	if _, _, _, err := parseClaudeArgs([]string{"--key-mode"}); err == nil {
		t.Fatal("缺值应报错")
	}
}

func TestTakeMultiAndParseEnvs(t *testing.T) {
	vals, rest := takeMulti([]string{"a", "--env", "A=1", "--env=B=x=y", "--model", "m", "--", "--env", "C=3"}, "env")
	if strings.Join(vals, ",") != "A=1,B=x=y" || strings.Join(rest, ",") != "a,--model,m,--,--env,C=3" {
		t.Fatal(vals, rest)
	}
	m, err := parseEnvs(vals)
	if err != nil || m["A"] != "1" || m["B"] != "x=y" {
		t.Fatal(m, err)
	}
	if _, err := parseEnvs([]string{"novalue"}); err == nil {
		t.Fatal("应报错")
	}
}
