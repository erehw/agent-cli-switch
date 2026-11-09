//go:build !windows

package cred

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// fileStore 仅供非 Windows 平台开发测试：key 以 0600 文件存放在 <root>/.acsw-cred-dev/。
type fileStore struct{ dir string }

// Default 在非 Windows 平台返回文件实现。
func Default(root string) Store { return fileStore{dir: filepath.Join(root, ".acsw-cred-dev")} }

func (s fileStore) path(t string) string {
	return filepath.Join(s.dir, strings.NewReplacer("/", "_", `\`, "_", ":", "_").Replace(t))
}

func (s fileStore) Get(t string) (string, error) {
	b, err := os.ReadFile(s.path(t))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	}
	return string(b), err
}

func (s fileStore) Set(t, v string) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.path(t), []byte(v), 0o600)
}

func (s fileStore) Delete(t string) error {
	err := os.Remove(s.path(t))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (fileStore) Kind() string { return "本地文件（仅开发测试用，不安全）" }
