package home

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestMissingFile(t *testing.T) {
	d := t.TempDir()
	have := filepath.Join(d, "a.txt")
	if err := os.WriteFile(have, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := &journal{dir: filepath.Join(d, "bk")}
	if err := j.snapshot(have, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := j.snapshot(filepath.Join(d, "none-wal"), "none-wal"); err != nil {
		t.Fatal(err)
	}
	j.writeManifest()
	b, err := os.ReadFile(filepath.Join(j.dir, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "01-a.txt") || !strings.Contains(s, "none-wal ← （原来没有这个文件）") || strings.Contains(s, "← .\n") {
		t.Fatal(s)
	}
}
