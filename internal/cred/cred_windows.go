//go:build windows

package cred

import (
	"errors"
	"fmt"

	"github.com/danieljoos/wincred"
)

type winStore struct{}

// Default 返回 Windows 凭据管理器实现。root 参数在 Windows 上不用。
func Default(root string) Store { return winStore{} }

func (winStore) Get(t string) (string, error) {
	c, err := wincred.GetGenericCredential(t)
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("读凭据管理器失败：%w", err)
	}
	return string(c.CredentialBlob), nil
}

func (winStore) Set(t, v string) error {
	c := wincred.NewGenericCredential(t)
	c.CredentialBlob = []byte(v)
	c.UserName = "acsw"
	c.Comment = "acsw API key（勿手改）"
	c.Persist = wincred.PersistLocalMachine
	if err := c.Write(); err != nil {
		return fmt.Errorf("写凭据管理器失败：%w", err)
	}
	return nil
}

func (winStore) Delete(t string) error {
	c, err := wincred.GetGenericCredential(t)
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return nil
		}
		return err
	}
	return c.Delete()
}

func (winStore) Kind() string { return "Windows 凭据管理器" }
