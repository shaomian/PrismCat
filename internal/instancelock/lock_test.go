package instancelock

import (
	"path/filepath"
	"testing"
)

func TestLockExclusionAndRelease(t *testing.T) {
	name := filepath.Join(t.TempDir(), "resource")
	first, err := Acquire(name)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Acquire(name); err == nil {
		second.Close()
		t.Fatal("duplicate lock allowed")
	}
	first.Close()
	third, err := Acquire(name)
	if err != nil {
		t.Fatal(err)
	}
	third.Close()
}
