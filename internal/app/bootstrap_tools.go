package app

import (
	"path/filepath"
	"time"

	"github.com/junnhwan/bond-code/internal/agenttask"
	"github.com/junnhwan/bond-code/internal/ask"
	"github.com/junnhwan/bond-code/internal/collaboration"
	execution "github.com/junnhwan/bond-code/internal/collaboration/backend"
	"github.com/junnhwan/bond-code/internal/collaboration/backendipc"
	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/memory"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/skill"
	"github.com/junnhwan/bond-code/internal/subagent"
	"github.com/junnhwan/bond-code/internal/todo"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/tool/builtin"
	"github.com/junnhwan/bond-code/internal/undo"
)

type runtimeToolDeps struct {
	registry            *tool.Registry
	client              llm.Client
	sessionID           string
	sessionDir          string
	memoryStore         *memory.MemoryStore
	taskStore           *todo.TaskStore
	skillLoader         *skill.Loader
	memoryConfig        config.MemoryConfig
	subagentConfig      config.SubagentConfig
	collaborationConfig config.CollaborationConfig
	skillsConfig        config.SkillsConfig
	questioner          ask.Questioner
	eventSink           subagent.EventSink
	loopFactory         subagent.LoopFactory
	policy              safety.Policy
}

func registerRuntimeTools(d runtimeToolDeps) (*subagent.SubagentManager, *agenttask.Service, *collaboration.Store, *execution.Registry, *backendipc.Supervisor, error) {
	// Registration order remains explicit: coding primitives, then model-facing
	// runtime capabilities (ask / memory / todo / skill / task / collaboration).
	// Thin shell wrappers (git_*, go_*, project_inspect) are intentionally not
	// registered — the model uses run_command for those workflows.
	observations := builtin.NewObservationStore()
	observations.BindSession(d.sessionID)
	coreTools, err := coreBuiltinTools(observations, undo.Default)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := registerTools(d.registry, coreTools); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	manager := newSubagentManager(d)
	runtimeTools := baseRuntimeTools(d)

	var taskService *agenttask.Service
	var collaborationStore *collaboration.Store
	var backendRegistry *execution.Registry
	var supervisor *backendipc.Supervisor
	if d.collaborationConfig.IsEnabled() {
		// Multi-Agent collaboration is on by default; set collaboration.enabled: false
		// to drop agent_task lifecycle, team/mailbox, and optional terminal backends.
		var err error
		ledger, err := agenttask.Open(filepath.Join(d.sessionDir, "collaboration", "agent_tasks.json"), agenttask.NewRuntimeLease())
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		runner := subagent.NewAgentTaskRunner(manager)
		backendRegistry, err = execution.NewRegistry(execution.NewInProcess(runner), execution.NewTmux(nil), execution.NewITerm(nil))
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		collaborationStore, err = collaboration.Open(filepath.Join(d.sessionDir, "collaboration", "collaboration.json"))
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		supervisor, err = backendipc.Start(filepath.Join(d.sessionDir, "collaboration", "launch-tokens"), 15*time.Second)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		backendRunner := collaboration.NewBackendRunner(runner, backendRegistry, supervisor)
		taskService = agenttask.NewService(ledger, backendRunner)
		taskService.SetInputJournal(collaboration.NewTaskInputJournal(collaborationStore))
		taskService.SetLaunchResolver(collaboration.NewLaunchResolver(collaborationStore, backendRegistry))
		runtimeTools = append(runtimeTools, agenttask.LifecycleToolsWithBackend(taskService, d.sessionID, backendRunner)...)
		runtimeTools = append(runtimeTools, collaboration.ToolsWithBackendsModeSource(collaborationStore, d.sessionID, d.policy.Mode, d.policy.BypassEnabled, d.policy.RuntimeModeSource, backendRegistry)...)
	}

	if d.skillsConfig.Enabled && d.skillLoader != nil {
		// Single Skill tool (Claude Code style). Listing is injected via
		// DynamicReminder, not a separate skill_list tool.
		runtimeTools = append(runtimeTools, skill.NewTool(d.skillLoader))
	}
	if d.subagentConfig.Enabled {
		runtimeTools = append(runtimeTools, subagentRuntimeTools(d, manager)...)
	}
	if err := registerTools(d.registry, runtimeTools); err != nil {
		if supervisor != nil {
			_ = supervisor.Close()
		}
		return nil, nil, nil, nil, nil, err
	}
	return manager, taskService, collaborationStore, backendRegistry, supervisor, nil
}

func registerTools(registry *tool.Registry, tools []tool.Tool) error {
	for _, candidate := range tools {
		if err := registry.Register(candidate); err != nil {
			return err
		}
	}
	return nil
}

func coreBuiltinTools(observations *builtin.ObservationStore, history *undo.Store) ([]tool.Tool, error) {
	readFile, err := builtin.NewReadFileToolWithObservations(observations)
	if err != nil {
		return nil, err
	}
	writeFile, err := builtin.NewWriteFileToolWithObservations(observations, history)
	if err != nil {
		return nil, err
	}
	editFile, err := builtin.NewEditFileToolWithObservations(observations, history)
	if err != nil {
		return nil, err
	}
	return []tool.Tool{
		readFile, writeFile, editFile,
		builtin.NewListDirTool(), builtin.NewSearchTextTool(), builtin.NewRunCommandTool(),
	}, nil
}

func newSubagentManager(d runtimeToolDeps) *subagent.SubagentManager {
	return subagent.NewSubagentManagerWithOptions(d.client, d.registry, subagent.ManagerOptions{
		MaxChildrenPerTurn:    d.subagentConfig.MaxChildrenPerTurn,
		MaxDepth:              d.subagentConfig.MaxDepth,
		DefaultTimeoutSeconds: d.subagentConfig.DefaultTimeoutSeconds,
		EventSink:             d.eventSink,
		LoopFactory:           d.loopFactory,
	})
}

// baseRuntimeTools is the consolidated model surface (ask / memory / todo).
func baseRuntimeTools(d runtimeToolDeps) []tool.Tool {
	return []tool.Tool{
		ask.NewAskUserTool(d.questioner),
		memory.NewMemorySearchTool(d.memoryStore, d.memoryConfig.MaxChars),
		memory.NewMemorySaveTool(d.memoryStore),
		todo.NewTodoReadTool(d.taskStore),
		todo.NewTodoWriteTool(d.taskStore),
	}
}

func subagentRuntimeTools(d runtimeToolDeps, manager *subagent.SubagentManager) []tool.Tool {
	taskTool := subagent.NewTaskTool(manager)
	taskTool.BindSession(d.sessionID)
	tools := []tool.Tool{taskTool}
	if d.subagentConfig.EnableSpawn {
		tools = append(tools, subagent.NewSpawnTool(manager, d.sessionID))
	}
	return tools
}
