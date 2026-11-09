package cred

import (
	"errors"
	"testing"
)

func testStore(t *testing.T, s Store) {
	if _, err := s.Get(Target("a")); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Set(Target("a"), "v"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get(Target("a")); err != nil || v != "v" {
		t.Fatal(v, err)
	}
	if err := s.Delete(Target("a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(Target("a")); err != nil {
		t.Fatal("重复删除不应报错", err)
	}
}

func TestMemory(t *testing.T) { testStore(t, NewMemory()) }

func TestTarget(t *testing.T) {
	if Target("api1") != "acsw/codex/api1" {
		t.Fatal(Target("api1"))
	}
}
