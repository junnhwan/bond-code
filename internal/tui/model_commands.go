package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/command/custom"
	"github.com/junnhwan/bond-code/internal/contextx"
)

func (m Model) runCommand(ctx context.Context, prompt string) (Model, tea.Cmd) {
	name, args := parseSlash(prompt)
	if name == "" {
		return m, nil
	}
	registryName := name
	if descriptor, ok := command.LookupSurfaceDescriptor(name); ok {
		switch descriptor.ExecutionTarget {
		case "tui-local.copy":
			next, _ := m.runCopyCommand(args)
			return next, nil
		case "tui-local.mouse":
			return m.runMouseCommand(args)
		case "tui-local.theme":
			return m.runThemeCommand(args)
		case "tui-local.compact":
			// Compaction is an async agent-style operation because model
			// summarization can take seconds and must not freeze the TUI.
			return m.startCompact()
		case "tui-local.retry":
			return m.retryLatestFailedTurn()
		case "overlay.diff":
			return m.openDiffViewer(), nil
		case "overlay.history":
			return m.enterHistory(), nil
		case "exit":
			return m, tea.Quit
		default:
			if command.ClassifyExecutionTarget(descriptor.ExecutionTarget) == command.ExecutionTargetRegistry {
				registryName = strings.TrimPrefix(string(descriptor.ExecutionTarget), "registry.")
			}
		}
	}
	if m.cfg.Commands == nil {
		body := "slash commands are not configured"
		m.timeline = m.timeline.AppendBlock(BlockError, "command", body)
		return m.markNewOutputBelow(), nil
	}
	cmd, ok := m.cfg.Commands.Get(registryName)
	if !ok {
		// Claude Code: /skill-name expands a user-invocable SKILL.md into the
		// turn (including disable-model-invocation / user-only skills).
		if next, teaCmd, handled := m.trySkillSlash(name, args); handled {
			return next, teaCmd
		}
		body := fmt.Sprintf("unknown command: /%s", name)
		m.timeline = m.timeline.AppendBlock(BlockError, "command", body)
		return m.markNewOutputBelow(), nil
	}
	if m.agent.Busy && isSessionSwitchCommand(registryName) {
		body := "cannot switch sessions while agent is running"
		m.timeline = m.timeline.AppendBlock(BlockError, "/"+name, body)
		return m.markNewOutputBelow(), nil
	}
	if cmd.PromptTemplate != "" {
		// Custom prompt-injecting command (.bondcode/commands/*.md): substitute
		// args into the template and submit the expanded prompt as a user turn.
		// This reuses the normal agent path, so safety / contextx apply exactly
		// as if the user had typed the prompt — the .md file is only a template,
		// never a shell-execution backdoor.
		expanded := custom.SubstituteArgs(cmd.PromptTemplate, args)
		return m.submitExpandedPrompt(expanded)
	}
	result, err := cmd.Run(ctx, commandEnv(m.cfg.CommandEnv, m.cfg.Status), args)
	if err != nil {
		m.timeline = m.timeline.AppendBlock(BlockError, "/"+name, err.Error())
		return m.markNewOutputBelow(), nil
	}
	// A session switch (e.g. /resume <id>) is a side-effect signal, not text
	// output: rebuild the timeline from the app's freshly-switched history,
	// track the new session id in both the header (cfg.Status) and the live,
	// then return without appending a command block — the switched-to session's
	// own conversation is what the user should see, not a "switched to X" line.
	// Mirrors forkAndResumeFromCursor's reset.
	if result.SessionSwitched != nil && m.cfg.ReloadSessionSeed != nil {
		newID := *result.SessionSwitched
		if cur := m.cfg.Status.SessionID; cur != "" {
			m.sessionScrolls[cur] = m.scroll
		}
		m = m.reloadSessionView(newID)
		m = m.pushSessionHistory(newID)
		return m, nil
	}
	// /resume with no args asks to open the interactive session-manager overlay
	// (switch / rename / pin / delete) instead of printing a text session list.
	// The overlay is TUI-only; when it isn't wired (headless / --once) we fall
	// through and render Output — the text list — as before.
	if result.OpenSessionManager && m.cfg.SessionManager != nil {
		return m.openSessionManager(), nil
	}
	// A model switch (/model <name>) is a side-effect signal like a session
	// switch, but lighter: it only updates the header's model name and the env
	// handed to later commands. Unlike a session switch it does NOT rebuild the
	// timeline, so the "switched to X" output still renders as a normal command
	// line via the fall-through below.
	if result.PermissionModeChanged != nil {
		m.cfg.Status.PermissionMode = *result.PermissionModeChanged
		m.cfg.CommandEnv.PermissionMode = *result.PermissionModeChanged
		m = m.pushToast("permissions: "+*result.PermissionModeChanged, toastInfo)
	}
	if result.ModelSwitched != nil {
		m.cfg.Status.Model = *result.ModelSwitched
		m.cfg.CommandEnv.Model = *result.ModelSwitched
		m = m.pushToast("model: "+*result.ModelSwitched, toastInfo)
	}
	if result.Panel != nil {
		m.timeline = m.timeline.AppendCommandBlock("/"+name, result.Output, result.Panel)
	} else {
		m.timeline = m.timeline.AppendBlock(BlockCommand, "/"+name, result.Output)
	}
	return m.markNewOutputBelow(), nil
}

// runMouseCommand handles /mouse [on|off|toggle]. With no arg, toggles capture.
// Off restores native terminal drag-select/copy; on enables click/wheel.
func (m Model) runMouseCommand(args []string) (Model, tea.Cmd) {
	want := "toggle"
	if len(args) > 0 {
		want = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch want {
	case "", "toggle":
		return m.toggleMouseCapture()
	case "on", "1", "true", "enable":
		if m.mouseEnabled {
			m = m.pushToast("mouse already on · shift+drag selects", toastInfo)
			return m, nil
		}
		return m.toggleMouseCapture()
	case "off", "0", "false", "disable":
		if !m.mouseEnabled {
			m = m.pushToast("mouse already off · drag to select/copy", toastInfo)
			return m, nil
		}
		return m.toggleMouseCapture()
	default:
		body := "usage: /mouse [on|off|toggle]"
		m.timeline = m.timeline.AppendBlock(BlockError, "/mouse", body)
		return m.markNewOutputBelow(), nil
	}
}

// runThemeCommand handles /theme: with no argument it lists the accent presets
// in a structured panel; with a preset name it recolors the Accent role live
// and persists the choice. theme is TUI-local (like /copy).
func (m Model) runThemeCommand(args []string) (Model, tea.Cmd) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		// Structured dark panel (active row + swatches), not a flat CSV dump.
		body := formatThemePanel(m.accent)
		m.timeline = m.timeline.AppendBlock(BlockCommand, "/theme", body)
		return m.markNewOutputBelow(), nil
	}
	preset := LookupAccentPreset(args[0])
	if preset == nil {
		body := fmt.Sprintf("unknown accent %q; /theme lists options", args[0])
		m.timeline = m.timeline.AppendBlock(BlockError, "/theme", body)
		m = m.pushToast(body, toastError)
		return m.markNewOutputBelow(), nil
	}
	ApplyAccent(preset.Color)
	m.accent = preset.Name
	m = m.persistPreferences()
	body := fmt.Sprintf("accent: %s", preset.Name)
	m.timeline = m.timeline.AppendBlock(BlockCommand, "/theme", body)
	m = m.pushToast(body, toastSuccess)
	return m.markNewOutputBelow(), nil
}

// trySkillSlash expands /<skill-name> [args] into a normal user turn when the
// name matches a skill. Returns handled=false when no skill exists so the
// caller can fall through to "unknown command".
func (m Model) trySkillSlash(name string, args []string) (Model, tea.Cmd, bool) {
	loader := m.cfg.CommandEnv.SkillLoader
	if loader == nil {
		return m, nil, false
	}
	argStr := strings.Join(args, " ")
	content, s, err := loader.ExpandForUser(name, argStr)
	if err != nil {
		// Skill exists but is model-only: surface that, don't say "unknown".
		if s != nil && !s.SlashInvocable() {
			body := fmt.Sprintf("skill /%s is model-only (user-invocable: false)", name)
			m.timeline = m.timeline.AppendBlock(BlockError, "/"+name, body)
			return m.markNewOutputBelow(), nil, true
		}
		return m, nil, false
	}
	if s == nil || content == "" {
		return m, nil, false
	}
	// Mirror Claude Code command-name markers so the model knows a skill was
	// already expanded and should not re-call the skill tool for the same name.
	expanded := formatSkillSlashPrompt(s.Name, argStr, content)
	next, cmd := m.submitExpandedPrompt(expanded)
	return next, cmd, true
}

func formatSkillSlashPrompt(name, args, content string) string {
	var b strings.Builder
	b.WriteString("<command-message>The \"/")
	b.WriteString(name)
	b.WriteString("\" skill is running</command-message>\n")
	b.WriteString("<command-name>/")
	b.WriteString(name)
	b.WriteString("</command-name>\n")
	if strings.TrimSpace(args) != "" {
		b.WriteString("<command-args>")
		b.WriteString(args)
		b.WriteString("</command-args>\n")
	}
	b.WriteString("\n")
	b.WriteString(content)
	return b.String()
}

// submitExpandedPrompt queues or starts an agent turn from an expanded template
// or skill body (shared by custom commands and skill slash invoke).
func (m Model) submitExpandedPrompt(expanded string) (Model, tea.Cmd) {
	if m.agent.Busy {
		m.agent.QueuedPrompts = append(m.agent.QueuedPrompts, expanded)
		return m, nil
	}
	if m.cfg.Chat == nil {
		body := "agent is not configured"
		m.timeline = m.timeline.AppendBlock(BlockError, "command", body)
		return m.markNewOutputBelow(), nil
	}
	m = m.beginUserTurn(expanded)
	agentPrompt := contextx.ExpandPathMentions(expanded, m.cfg.Status.ProjectRoot)
	return m, func() tea.Msg {
		return runAgentMsg{prompt: agentPrompt}
	}
}

func parseSlash(input string) (string, []string) {
	fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(input), "/"))
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

func isExitSlashCommand(input string) bool {
	if !strings.HasPrefix(strings.TrimSpace(input), "/") {
		return false
	}
	name, _ := parseSlash(input)
	descriptor, ok := command.LookupSurfaceDescriptor(name)
	return ok && command.ClassifyExecutionTarget(descriptor.ExecutionTarget) == command.ExecutionTargetExit
}

func isRetrySlashCommand(input string) bool {
	if !strings.HasPrefix(strings.TrimSpace(input), "/") {
		return false
	}
	name, _ := parseSlash(input)
	descriptor, ok := command.LookupSurfaceDescriptor(name)
	return ok && descriptor.ExecutionTarget == "tui-local.retry"
}

func commandEnv(env command.Env, status Status) command.Env {
	if env.SessionID == "" {
		env.SessionID = status.SessionID
	}
	if env.ProjectRoot == "" {
		env.ProjectRoot = status.ProjectRoot
	}
	if env.PermissionMode == "" {
		env.PermissionMode = status.PermissionMode
	}
	if env.Model == "" {
		env.Model = status.Model
	}
	if env.ToolCount == 0 {
		env.ToolCount = status.ToolCount
	}
	return env
}

func isSessionSwitchCommand(name string) bool {
	return name == "new" || name == "resume"
}
