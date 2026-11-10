package core

import (
	"strings"
	"testing"
)

func TestParseModelIDs(t *testing.T) {
	for in, want := range map[string]string{
		`{"data":[{"id":"b"},{"id":"a"}]}`:           "a,b",
		`{"models":[{"name":"x"},{"id":"y"}]}`:       "x,y",
		`["m2","m1","m2"]`:                           "m1,m2",
		`{"data":[]}`:                                "",
		`{"object":"list","data":[{"id":"claude"}]}`: "claude",
	} {
		ids, ok := ParseModelIDs([]byte(in))
		if !ok || strings.Join(ids, ",") != want {
			t.Errorf("%s → %v %v", in, ids, ok)
		}
	}
	for _, bad := range []string{`<html>`, `{"foo":1}`, `42`} {
		if _, ok := ParseModelIDs([]byte(bad)); ok {
			t.Errorf("%s 应判为非标准", bad)
		}
	}
}
