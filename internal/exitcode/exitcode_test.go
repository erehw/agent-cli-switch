package exitcode

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodesUniqueAndInRange(t *testing.T) {
	all := []Code{NotMapped, ExpectMismatch, NoSource, BadArgs, HomeConflict, HomeNotFound, SourceInvalid, ReadonlyAlias,
		KeyUnreadable, CodexNotFound, LockBusy, CodexStart, Upstream, Other}
	seen := map[Code]bool{}
	for _, c := range all {
		if seen[c] || c < 99994000 || c > 99995999 || c > 1<<31-1 {
			t.Fatal(c)
		}
		seen[c] = true
	}
}

func TestExitFor(t *testing.T) {
	if exitFor(NotMapped, "windows") != 99994001 || exitFor(Other, "windows") != 99995999 {
		t.Fatal("windows 应用大码")
	}
	if exitFor(ExpectMismatch, "linux") != 2 || exitFor(KeyUnreadable, "darwin") != 1 {
		t.Fatal("非 windows 保留旧码")
	}
}

func TestWithOf(t *testing.T) {
	base := errors.New("x")
	err := fmt.Errorf("外层：%w", With(NoSource, base))
	if c, ok := Of(err); !ok || c != NoSource || !errors.Is(err, base) {
		t.Fatal(c, ok)
	}
	if _, ok := Of(base); ok {
		t.Fatal("未附码")
	}
	if With(Other, nil) != nil {
		t.Fatal("nil 应保持 nil")
	}
}
