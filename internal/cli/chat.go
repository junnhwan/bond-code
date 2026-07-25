package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/command"
	commandbuiltin "github.com/junnhwan/bond-code/internal/command/builtin"
	"github.com/junnhwan/bond-code/internal/command/custom"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/tui"
)

type bootstrapFunc func(app.Options) (*app.App, error)

// parseDebugVerbose resolves the --debug flag (falling back to BONDCODE_DEBUG)
// into an observe.Verbose level. Empty/off/0/false -> 0 (off); full/2 -> full;
// anything else (default/1/true/on) -> default.
func parseDebugVerbose(flagLevel string) observe.Verbose {
	level := strings.ToLower(strings.TrimSpace(flagLevel))
	if level == "" {
		level = strings.ToLower(strings.TrimSpace(os.Getenv("BONDCODE_DEBUG")))
	}
	switch level {
	case "", "off", "0", "false":
		return 0
	case "full", "2":
		return observe.VerboseFull
	default:
		return observe.VerboseDefault
	}
}

func newTUIConfirmer() *tui.Confirmer {
	return tui.NewConfirmer()
}

func newTUIQuestioner() *tui.Questioner {
	return tui.NewQuestioner()
}

func commandEnvForApp(application *app.App) command.Env {
	wd, _ := os.Getwd()
	env := command.Env{
		SessionID:      "local",
		ProjectRoot:    wd,
		PermissionMode: "confirm",
	}
	if application == nil {
		return env
	}
	if application.SessionID != "" {
		env.SessionID = application.SessionID
	}
	if application.Config != nil {
		env.Model = application.Config.Model.Model
	}
	env.PermissionMode = permissionMode(application.Policy)
	if application.Tools != nil {
		env.ToolCount = len(application.Tools.Names())
	}
	env.MemoryStore = application.MemoryStore
	env.MemoryMaxChars = application.MemoryMaxChars
	env.SkillLoader = application.SkillLoader
	env.SkillMaxChars = application.SkillMaxChars
	env.TaskStore = application.TaskStore
	env.StatusProvider = application
	env.Sessions = application.Sessions
	env.SetPermissionMode = application.SetPermissionMode
	return env
}

func withCollaborationStatus(status tui.Status, application *app.App) tui.Status {
	if application == nil || application.Collaboration == nil {
		return status
	}
	teams := application.Collaboration.ListTeams(application.SessionID)
	sort.Slice(teams, func(i, j int) bool { return teams[i].Name < teams[j].Name })
	status.Teams = make([]tui.TeamView, 0, len(teams))
	for _, team := range teams {
		view := tui.TeamView{ID: team.ID, Name: team.Name, State: string(team.State)}
		members := application.Collaboration.ListMembers(team.ID)
		sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
		for _, member := range members {
			unread, err := application.Collaboration.Inbox(team.ID, member.ID, 0, true)
			if err != nil {
				continue
			}
			view.Unread += len(unread)
			view.Members = append(view.Members, tui.TeamMemberView{
				ID: member.ID, Name: member.Name, Role: member.Role, State: string(member.State),
				Backend: member.Backend, PermissionMode: member.PermissionMode,
				TaskID: member.PrimaryTaskID, Unread: len(unread),
			})
		}
		status.Teams = append(status.Teams, view)
	}
	return status
}

func withTaskStatus(status tui.Status, application *app.App) tui.Status {
	if application == nil || application.TaskStore == nil {
		return status
	}
	if summary, err := application.TaskStore.Summary(); err == nil {
		status.PlanningSummary = summary
	}
	if tasks, err := application.TaskStore.List(); err == nil {
		status.Tasks = make([]tui.TaskView, 0, len(tasks))
		for _, task := range tasks {
			status.Tasks = append(status.Tasks, tui.TaskView{
				ID: task.ID, Subject: task.Subject, Status: string(task.Status),
				Owner: task.Owner, ActiveForm: task.ActiveForm,
			})
		}
	}
	return status
}

func runTUI(ctx context.Context, application *app.App) error {
	registry := command.NewRegistry()
	if err := commandbuiltin.RegisterAll(registry); err != nil {
		return err
	}
	// Custom prompt-injecting commands from .bondcode/commands/*.md (project +
	// user level). Load errors are non-fatal: a malformed file is skipped, never
	// breaks startup.
	_ = custom.Load(registry)
	wd, _ := os.Getwd()
	sessionID := application.SessionID
	if sessionID == "" {
		sessionID = "local"
	}
	status := tui.Status{
		SessionID:      sessionID,
		ProjectRoot:    wd,
		Model:          application.Config.Model.Model,
		PermissionMode: permissionMode(application.Policy),
		ToolCount:      len(application.Tools.Names()),
		GitBranch:      currentGitBranch(wd),
	}
	status = withCollaborationStatus(withTaskStatus(status, application), application)
	snap := application.StatusSnapshot()
	status.ContextSummary = runtimeContextSummary(snap)
	status.MemorySummary = runtimeMemorySummary(snap)
	status.SubagentSummary = runtimeSubagentSummary(snap)
	status.MCPStatus = runtimeMCPSummary(snap)
	status.ContextBreakdown = breakdownView(snap.Context.Breakdown)
	var agentInputSequence atomic.Uint64
	return tui.Run(ctx, tui.Config{
		Status:            status,
		MouseCapture:      application.Config.TUI.Mouse(),
		PromptHistoryPath: filepath.Join(application.Config.Session.Dir, "prompt-history.json"),
		StashPath:         filepath.Join(application.Config.Session.Dir, "prompt-stash.json"),
		PreferencesPath:   filepath.Join(application.Config.Session.Dir, "tui-preferences.json"),
		Accent:            application.Config.TUI.Accent,
		Commands:          registry,
		Chat:              application,
		Confirmer:         asTUIConfirmer(application.Confirmer),
		RuleSource:        application.RuleSource,
		Questioner:        asTUIQuestioner(application.Questioner),
		PlanMode:          application,
		SessionHistory:    sessionHistoryAdapter{app: application},
		SessionManager:    sessionManagerAdapter{app: application},
		SeedHistory:       seedHistoryFromApp(application),
		// ReloadSessionSeed re-projects the app's history after a /resume switch.
		// SwitchSession has already swapped application.History() onto the target
		// session, so this yields exactly the target's conversation for the TUI to
		// rebuild its timeline (mirrors sessionHistoryAdapter.ResumeFromEvent).
		ReloadSessionSeed: func(sessionID string) []tui.SeedMessage {
			return seedHistoryFromApp(application)
		},
		RefreshStatus: func() tui.Status {
			snap := application.StatusSnapshot()
			fresh := tui.Status{
				ContextSummary:   runtimeContextSummary(snap),
				ContextBreakdown: breakdownView(snap.Context.Breakdown),
				Usage: tui.UsageView{
					ModelCalls:        snap.Usage.ModelCalls,
					TotalInputTokens:  snap.Usage.TotalInputTokens,
					TotalOutputTokens: snap.Usage.TotalOutputTokens,
				},
			}
			return withCollaborationStatus(withTaskStatus(fresh, application), application)
		},
		CancelSubagent: func(taskID string) bool {
			if application.SubagentManager == nil {
				return false
			}
			return application.SubagentManager.CancelTask(taskID)
		},
		SendSubagentInput: func(taskID, input string) error {
			if application.AgentTasks == nil {
				return fmt.Errorf("agent task service is unavailable")
			}
			key := fmt.Sprintf("tui-agent-input-%d", agentInputSequence.Add(1))
			_, err := application.AgentTasks.ContinueInput(context.Background(), taskID, input, key)
			return err
		},
		CommandEnv: command.Env{
			SessionID:         status.SessionID,
			ProjectRoot:       status.ProjectRoot,
			PermissionMode:    status.PermissionMode,
			Model:             status.Model,
			ToolCount:         status.ToolCount,
			MemoryStore:       application.MemoryStore,
			MemoryMaxChars:    application.MemoryMaxChars,
			SkillLoader:       application.SkillLoader,
			SkillMaxChars:     application.SkillMaxChars,
			TaskStore:         application.TaskStore,
			StatusProvider:    application,
			Sessions:          application.Sessions,
			SwitchSession:     application.SwitchSession,
			NewSession:        application.NewSession,
			SwitchModel:       application.SwitchModel,
			ModelSuggestions:  application.Config.Model.Retry.FallbackModels,
			SetPermissionMode: application.SetPermissionMode,
		},
	})
}

func runtimeContextSummary(status app.RuntimeStatus) string {
	if status.Context.Summary != "" {
		return "summary " + status.Context.Summary
	}
	if stats := strings.TrimSpace(status.Context.Stats); strings.Contains(stats, "warning") {
		return stats
	}
	if status.Context.MaxTokens > 0 {
		return fmt.Sprintf("max %d tokens", status.Context.MaxTokens)
	}
	return ""
}

// breakdownView mirrors app.ContextBreakdown into the neutral TUI view used by
// the /context proportional breakdown, keeping the TUI free of an app import.
func breakdownView(b app.ContextBreakdown) tui.ContextBreakdownView {
	return tui.ContextBreakdownView{
		System:       b.SystemTokens,
		Conversation: b.ConversationTokens,
		ToolResult:   b.ToolResultTokens,
		Summary:      b.SummaryTokens,
	}
}

func runtimeMemorySummary(status app.RuntimeStatus) string {
	if status.Memory.Error != "" {
		return "error: " + status.Memory.Error
	}
	if !status.Memory.Enabled {
		return "memory off"
	}
	return fmt.Sprintf("memory %d chars", status.Memory.Chars)
}

func runtimeSubagentSummary(status app.RuntimeStatus) string {
	if !status.Subagents.Enabled {
		return "subagents off"
	}
	if status.Subagents.Active > 0 {
		return fmt.Sprintf("%d active", status.Subagents.Active)
	}
	return strings.TrimSpace(status.Subagents.Latest)
}

func runtimeMCPSummary(status app.RuntimeStatus) string {
	if !status.MCP.Enabled && status.MCP.Servers == 0 {
		return "mcp off"
	}
	base := fmt.Sprintf("%d server %d tools", status.MCP.Servers, status.MCP.Tools)
	if status.MCP.Errors > 0 {
		base += fmt.Sprintf(" %d errors", status.MCP.Errors)
	}
	return base
}

// seedHistoryFromApp converts the app's accumulated message history into the
// neutral SeedMessage view the TUI uses to rebuild the timeline on resume.
func seedHistoryFromApp(application *app.App) []tui.SeedMessage {
	if application == nil {
		return nil
	}
	var seed []tui.SeedMessage
	for _, msg := range application.History() {
		switch string(msg.Role) {
		case "system":
			continue
		case "assistant":
			if strings.TrimSpace(msg.Content) != "" {
				seed = append(seed, tui.SeedMessage{Role: "assistant", Content: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				seed = append(seed, tui.SeedMessage{Role: "tool", ToolName: tc.Name, Content: tc.Arguments})
			}
		default:
			seed = append(seed, tui.SeedMessage{Role: string(msg.Role), Content: msg.Content, ToolName: msg.ToolName})
		}
	}
	return seed
}

func currentGitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	out, err = exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		return ""
	}
	return branch
}

func permissionMode(policy safety.Policy) string {
	mode, err := safety.ParsePermissionMode(string(policy.EffectiveMode()))
	if err != nil {
		return string(safety.ModeDefault)
	}
	return string(mode)
}

func asTUIConfirmer(confirmer safety.Confirmer) *tui.Confirmer {
	if c, ok := confirmer.(*tui.Confirmer); ok {
		return c
	}
	if c, ok := confirmer.(safety.AutoApproveConfirmer); ok {
		return asTUIConfirmer(c.Fallback)
	}
	return nil
}

func asTUIQuestioner(questioner any) *tui.Questioner {
	if q, ok := questioner.(*tui.Questioner); ok {
		return q
	}
	return nil
}

// sessionHistoryAdapter adapts app.App to the TUI's SessionHistoryController.
// LoadEvents reads the raw event log; ResumeFromEvent forks the session via
// app.ForkAndResume (which switches the app onto the new branch and rebuilds
// its history) and seeds the TUI timeline from that rebuilt, neutral view, so
// the TUI never imports the llm package.
type sessionHistoryAdapter struct {
	app *app.App
}

func (a sessionHistoryAdapter) LoadEvents(sessionID string) ([]session.Event, error) {
	return a.app.Sessions.Load(sessionID)
}

func (a sessionHistoryAdapter) ResumeFromEvent(sessionID, eventID string) (string, []tui.SeedMessage, error) {
	newID, _, err := a.app.ForkAndResume(sessionID, eventID)
	if err != nil {
		return "", nil, err
	}
	// ForkAndResume replaced app.History() with the rebuilt path messages, so
	// seedHistoryFromApp yields exactly the forked branch's conversation.
	return newID, seedHistoryFromApp(a.app), nil
}

// sessionManagerAdapter adapts app.App to the TUI's SessionManagerController.
// It is the bridge for the session-manager overlay + quick switch: List derives
// each session's title/preview, pin state, message count, and last activity
// from the audit log + meta sidecar; the mutators hand through to the store
// (title/pin via the meta sidecar, delete via the store) and Switch reuses the
// same SwitchSession the /resume command path exercises.
type sessionManagerAdapter struct {
	app *app.App
}

func (a sessionManagerAdapter) List() ([]tui.SessionInfo, error) {
	store := a.app.Sessions
	if store == nil {
		return nil, nil
	}
	ids, err := store.List()
	if err != nil {
		return nil, err
	}
	active := a.app.SessionID
	infos := make([]tui.SessionInfo, 0, len(ids))
	for _, id := range ids {
		meta, _ := store.LoadMeta(id)
		preview, count, last := session.SessionPreview(store, id)
		isActive := id == active
		// Hide empty abandoned sessions; always keep active/pinned/renamed ones.
		if !session.KeepInResumeList(isActive, meta.Pinned, meta.Title, count) {
			continue
		}
		title := meta.Title
		if strings.TrimSpace(title) == "" {
			title = preview
		}
		infos = append(infos, tui.SessionInfo{
			ID:         id,
			Title:      title,
			Pinned:     meta.Pinned,
			Active:     isActive,
			Messages:   count,
			LastActive: last,
		})
	}
	// Pinned first, then newest by last activity (ids embed a UTC timestamp so a
	// tiebreak on id is chronological too).
	sortSessionInfos(infos)
	return infos, nil
}

func (a sessionManagerAdapter) Delete(id string) error {
	if a.app.Sessions == nil {
		return nil
	}
	return a.app.Sessions.Delete(id)
}

func (a sessionManagerAdapter) SetTitle(id, title string) error {
	if a.app.Sessions == nil {
		return nil
	}
	meta, _ := a.app.Sessions.LoadMeta(id)
	meta.Title = strings.TrimSpace(title)
	return a.app.Sessions.SaveMeta(id, meta)
}

func (a sessionManagerAdapter) SetPinned(id string, pinned bool) error {
	if a.app.Sessions == nil {
		return nil
	}
	meta, _ := a.app.Sessions.LoadMeta(id)
	meta.Pinned = pinned
	return a.app.Sessions.SaveMeta(id, meta)
}

// sortSessionInfos orders pinned sessions first, then by last activity (newest
// first), with the session id as a stable chronological tiebreak.
func sortSessionInfos(infos []tui.SessionInfo) {
	sort.SliceStable(infos, func(i, j int) bool {
		if infos[i].Pinned != infos[j].Pinned {
			return infos[i].Pinned
		}
		if !infos[i].LastActive.Equal(infos[j].LastActive) {
			return infos[i].LastActive.After(infos[j].LastActive)
		}
		return infos[i].ID > infos[j].ID
	})
}
