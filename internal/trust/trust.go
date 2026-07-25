// Package trust 管理项目目录的信任状态。本地 agent 会加载项目内的 skill/MCP 配置，
// 未受信任的项目可能通过这些配置投毒（恶意 skill 指令、恶意 MCP server）。
// trust 门控：只有显式信任的目录（或其父目录被信任）才加载项目内资源。
package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/junnhwan/bond-code/internal/fsx"
)

// Manager 持有已信任目录集合，支持父目录继承与持久化。
type Manager struct {
	storePath string
	mu        sync.Mutex
	trusted   map[string]bool
	loaded    bool
}

func NewManager(storePath string) *Manager {
	return &Manager{storePath: storePath, trusted: map[string]bool{}}
}

// IsTrusted 报告 dir 或其任一父目录是否被信任。
func (m *Manager) IsTrusted(dir string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		m.load()
		m.loaded = true
	}
	abs := absDir(dir)
	for d := abs; ; d = filepath.Dir(d) {
		if m.trusted[d] {
			return true
		}
		if d == filepath.Dir(d) {
			break // 到达根
		}
	}
	return false
}

// Trust 标记 dir 为信任并持久化。
func (m *Manager) Trust(dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		m.load()
		m.loaded = true
	}
	m.trusted[absDir(dir)] = true
	return m.save()
}

// Revoke 移除 dir 的信任标记。
func (m *Manager) Revoke(dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		m.load()
		m.loaded = true
	}
	delete(m.trusted, absDir(dir))
	return m.save()
}

func (m *Manager) load() {
	if m.storePath == "" {
		return
	}
	b, err := os.ReadFile(m.storePath)
	if err != nil {
		return
	}
	var dirs []string
	if json.Unmarshal(b, &dirs) == nil {
		for _, d := range dirs {
			m.trusted[d] = true
		}
	}
}

func (m *Manager) save() error {
	if m.storePath == "" {
		return nil
	}
	dirs := make([]string, 0, len(m.trusted))
	for d := range m.trusted {
		dirs = append(dirs, d)
	}
	b, err := json.MarshalIndent(dirs, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(m.storePath, b, 0o600)
}

// StoreExists 报告 trust store 文件是否存在（用于区分"首次运行"与"已配置"）。
func (m *Manager) StoreExists() bool {
	if m.storePath == "" {
		return false
	}
	_, err := os.Stat(m.storePath)
	return err == nil
}

func absDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return filepath.Clean(abs)
}
