//go:build !windows

package cred

import "testing"

func TestFileStore(t *testing.T) { testStore(t, Default(t.TempDir())) }
