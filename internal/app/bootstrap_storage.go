package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/memory"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/skill"
	"github.com/junnhwan/bond-code/internal/todo"
	"github.com/junnhwan/bond-code/internal/trust"
)

type bootstrapStores struct {
	sessions   *session.JSONLStore
	sessionID  string
	ruleSource *session.RuleSource
	memory     *memory.MemoryStore
	tasks      *todo.TaskStore
	skills     *skill.Loader
}

func openBootstrapStores(cfg *config.Config, resumeSessionID, projectRoot string) (bootstrapStores, error) {
	store := session.NewJSONLStore(cfg.Session.Dir)
	sessionID, err := resolveBootstrapSessionID(store, resumeSessionID)
	if err != nil {
		return bootstrapStores{}, err
	}
	memoryStore, err := memory.NewMemoryStore(cfg.Session.Dir)
	if err != nil {
		return bootstrapStores{}, err
	}
	taskStore, err := todo.NewSessionTaskStore(cfg.Session.Dir, sessionID)
	if err != nil {
		return bootstrapStores{}, err
	}
	return bootstrapStores{
		sessions:   store,
		sessionID:  sessionID,
		ruleSource: session.NewRuleSource(store, sessionID),
		memory:     memoryStore,
		tasks:      taskStore,
		skills:     trustedSkillLoader(cfg, projectRoot),
	}, nil
}

func resolveBootstrapSessionID(store *session.JSONLStore, resumeSessionID string) (string, error) {
	if resumeSessionID == "" {
		return newSessionID(), nil
	}
	ids, err := store.List()
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	if !containsString(ids, resumeSessionID) {
		return "", fmt.Errorf("session %q not found", resumeSessionID)
	}
	return resumeSessionID, nil
}

func trustedSkillLoader(cfg *config.Config, projectRoot string) *skill.Loader {
	if !cfg.Skills.Enabled {
		return nil
	}
	// A fresh trust store trusts the current project. Existing stores gate
	// project-provided skills so an untrusted repository cannot inject prompts.
	manager := trust.NewManager(filepath.Join(cfg.Session.Dir, "trust.json"))
	if !manager.StoreExists() {
		_ = manager.Trust(projectRoot)
	}
	projectTrusted := manager.IsTrusted(projectRoot)

	userDir := filepath.Join(bondcodeHome(), "skills")
	var projectDir string
	if projectTrusted {
		projectDir = filepath.Join(projectRoot, ".bondcode", "skills")
	}
	var extra []string
	if root := strings.TrimSpace(cfg.Skills.Root); root != "" && projectTrusted {
		if filepath.IsAbs(root) {
			extra = append(extra, root)
		} else {
			extra = append(extra, filepath.Join(projectRoot, root))
		}
	}
	return skill.NewLoader(skill.Paths{
		UserDir:       userDir,
		ProjectDir:    projectDir,
		ExtraDirs:     extra,
		MaxChars:      cfg.Skills.MaxChars,
		ListingBudget: cfg.Skills.ListingBudgetChars,
	})
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
