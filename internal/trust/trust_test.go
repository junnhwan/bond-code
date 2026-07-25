package trust

import (
	"path/filepath"
	"testing"
)

// 子目录继承父目录的信任。
func TestTrustInheritsFromParent(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "sub", "deep")
	m := NewManager("")
	if err := m.Trust(parent); err != nil {
		t.Fatal(err)
	}
	if !m.IsTrusted(child) {
		t.Fatal("child dir should inherit trust from parent")
	}
	if m.IsTrusted(filepath.Join(t.TempDir(), "unrelated")) {
		t.Fatal("unrelated dir must not be trusted")
	}
}

// 信任状态持久化到 store，新 Manager 可重新加载。
func TestTrustPersistsAcrossManagers(t *testing.T) {
	store := filepath.Join(t.TempDir(), "trust.json")
	dir := t.TempDir()

	m1 := NewManager(store)
	if err := m1.Trust(dir); err != nil {
		t.Fatal(err)
	}
	m2 := NewManager(store)
	if !m2.IsTrusted(dir) {
		t.Fatal("trust should persist to store and reload")
	}
}

// 默认不信任。
func TestTrustUntrustedByDefault(t *testing.T) {
	m := NewManager("")
	if m.IsTrusted(t.TempDir()) {
		t.Fatal("dir must not be trusted by default")
	}
}

// Revoke 移除信任。
func TestTrustRevoke(t *testing.T) {
	dir := t.TempDir()
	m := NewManager("")
	if err := m.Trust(dir); err != nil {
		t.Fatal(err)
	}
	if !m.IsTrusted(dir) {
		t.Fatal("should be trusted after Trust")
	}
	if err := m.Revoke(dir); err != nil {
		t.Fatal(err)
	}
	if m.IsTrusted(dir) {
		t.Fatal("should not be trusted after Revoke")
	}
}
