// Package cred 负责 API key 的存取。Windows 用凭据管理器；其他平台用仅供测试的文件实现。
package cred

import (
	"errors"
	"sync"
)

// ErrNotFound 表示没有存这个 key。
var ErrNotFound = errors.New("凭据不存在")

// Store 是凭据存储接口。
type Store interface {
	Get(target string) (string, error)
	Set(target, secret string) error
	Delete(target string) error
	// Kind 说明实现类型，用于提示。
	Kind() string
}

// Target 返回某个 source 的凭据目标名：acsw/codex/<name>。
func Target(source string) string { return "acsw/codex/" + source }

// TargetFor 返回某个工具的 source 的凭据目标名：acsw/<tool>/<name>（tool 为空按 codex）。
func TargetFor(tool, source string) string {
	if tool == "" {
		tool = "codex"
	}
	return "acsw/" + tool + "/" + source
}

// Memory 是内存实现，用于单测。
type Memory struct {
	mu sync.Mutex
	m  map[string]string
}

func NewMemory() *Memory { return &Memory{m: map[string]string{}} }

func (s *Memory) Get(t string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[t]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (s *Memory) Set(t, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[t] = v
	return nil
}
func (s *Memory) Delete(t string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, t)
	return nil
}
func (s *Memory) Kind() string { return "内存（测试用）" }
