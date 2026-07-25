package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/subagent"
)

func main() {
	if os.Getenv("BONDCODE_API_KEY") == "" {
		die("BONDCODE_API_KEY not set; bondbench needs the real LLM.")
	}
	mode := "context"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	switch mode {
	case "context":
		runContext()
	case "subagent":
		runSubagent()
	default:
		die("unknown mode %q (want context|subagent)", mode)
	}
}

// Multi-turn accumulation: each turn is light (2 files + short answer) so the
// slow model actually completes it, but history grows turn over turn into a
// genuinely long real conversation.
var pressurePrompts = []string{
	"用 read_file 完整读取 internal/agent/loop.go 和 internal/contextx/governor.go，对比两者的并发模型，200 字内。",
	"接着用 read_file 完整读取 internal/subagent/manager.go 和 internal/safety/policy.go，对比子任务边界与安全分级，200 字内。",
	"接着用 read_file 完整读取 internal/safety/command_guard.go 和 internal/app/app.go，总结安全分级与装配流程，200 字内。",
	"基于你前面读过的全部 6 个文件，归纳它们共同遵守的不变量和各自的边界控制，300 字内。",
}

func runContext() {
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	a, err := app.Bootstrap(app.Options{AutoYes: true})
	if err != nil {
		die("bootstrap: %v", err)
	}
	window := a.MaxContextTokens
	fmt.Printf("[setup] model=%s context_window=%d\n", a.Config.Model.Model, window)

	var (
		govUpdates  int
		toolCalls   int
		guardHits   int
		reactive    int
		peakIn      int
		toolSizes   []int
		toolOutputs = map[string]string{}
		perStepIn   []int
		lastMsgs    []llm.Message
	)
	sink := func(e agent.Event) {
		switch e.Type {
		case agent.EventContextUpdated:
			govUpdates++
			if strings.Contains(e.Message, "reactive") {
				reactive++
			}
		case agent.EventToolResult:
			toolCalls++
			toolSizes = append(toolSizes, len(e.Output))
			if e.ToolCallID != "" && len(e.Output) > len(toolOutputs[e.ToolCallID]) {
				toolOutputs[e.ToolCallID] = e.Output
			}
		case agent.EventLoopGuard:
			guardHits++
		case agent.EventContextMeasured:
			if e.MeasuredInputTokens > 0 {
				perStepIn = append(perStepIn, e.MeasuredInputTokens)
				if e.MeasuredInputTokens > peakIn {
					peakIn = e.MeasuredInputTokens
				}
			}
		}
	}

	for i, p := range pressurePrompts {
		if ctx.Err() != nil {
			fmt.Fprintf(os.Stderr, "[turn %d] skipped (ctx done)\n", i+1)
			break
		}
		tt := time.Now()
		r, runErr := a.RunWithEvents(ctx, p, sink)
		fmt.Fprintf(os.Stderr, "[turn %d/%d] elapsed=%s err=%v\n", i+1, len(pressurePrompts), time.Since(tt).Round(time.Millisecond), runErr)
		if r != nil && len(r.Messages) > 0 {
			lastMsgs = r.Messages
		}
	}

	msgs := lastMsgs
	if len(msgs) == 0 {
		die("no history captured")
	}
	rawMsgs := rebuildRaw(msgs, toolOutputs)
	liveTokens := a.ContextManager.EstimateTokens(msgs)
	rawTokens := a.ContextManager.EstimateTokens(rawMsgs)
	fmt.Printf("[run] governance_turns=%d tool_calls=%d guard_hits=%d reactive=%d peak_real_input=%d\n",
		govUpdates, toolCalls, guardHits, reactive, peakIn)
	fmt.Printf("[run] per_step_real_input_tokens=%v\n", perStepIn)
	fmt.Printf("[run] tool_result_raw_chars_sum=%d\n", sumInt(toolSizes))
	fmt.Printf("[run] history_messages=%d  live_tokens=%d  RAW_tokens=%d (%.2fx the %d window)\n",
		len(msgs), liveTokens, rawTokens, ratioF(rawTokens, window), window)

	fmt.Println("[pressure] governance A/B on the RAW (ungoverned) conversation:")
	fmt.Println("  budget     before   -> after    reduction  micro  spill  breaks")
	for _, b := range []int{4000, 8000, 16000, 32000, 64000, window} {
		if b > window {
			continue
		}
		g := a.ContextManager.GovernDetailed(rawMsgs, b)
		fmt.Printf("  %-7d   %-7d -> %-7d  %5.1f%%    %-3d   %-3d   %d\n",
			b, g.BeforeTokens, g.AfterTokens, pct(g.BeforeTokens, g.AfterTokens),
			g.CompactedToolResults, g.TruncatedToolResults, countUnpaired(g.Messages))
	}
}

func runSubagent() {
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	a, err := app.Bootstrap(app.Options{AutoYes: true})
	if err != nil {
		die("bootstrap: %v", err)
	}
	if a.SubagentManager == nil {
		die("subagent manager not configured")
	}
	fmt.Printf("[setup] model=%s subagent_concurrency_limit=3\n", a.Config.Model.Model)

	tasks := []subagent.TaskRequest{
		{SubagentType: subagent.AgentTypeResearch, Description: "loop.go concurrency+guard", Prompt: "用 read_file 读取 internal/agent/loop.go，用不超过 150 字总结它的并发工具调度和 loop guard 机制。"},
		{SubagentType: subagent.AgentTypeResearch, Description: "governor.go 4-layer", Prompt: "用 read_file 读取 internal/contextx/governor.go，用不超过 150 字总结它的四层治理流程。"},
		{SubagentType: subagent.AgentTypeResearch, Description: "manager.go bounds", Prompt: "用 read_file 读取 internal/subagent/manager.go，用不超过 150 字总结它如何控制子任务的执行边界。"},
	}

	fmt.Println("[subagent] running 3 research tasks SERIALLY...")
	t0 := time.Now()
	for i, t := range tasks {
		r, err := a.SubagentManager.RunTask(ctx, t)
		status := "ok"
		if err != nil {
			status = "err:" + err.Error()
		} else if r != nil {
			status = r.Status
		}
		fmt.Printf("  [%d %s] cumulative=%s\n", i+1, status, time.Since(t0).Round(time.Millisecond))
	}
	serial := time.Since(t0)

	fmt.Println("[subagent] running the SAME 3 tasks IN PARALLEL...")
	t1 := time.Now()
	states := make([]string, len(tasks))
	var wg sync.WaitGroup
	for i, t := range tasks {
		i, t := i, t
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := a.SubagentManager.RunTask(ctx, t)
			switch {
			case err != nil:
				states[i] = "err:" + err.Error()
			case r != nil:
				states[i] = r.Status
			default:
				states[i] = "no-result"
			}
		}()
	}
	wg.Wait()
	parallel := time.Since(t1)
	for i, s := range states {
		fmt.Printf("  [%d %s]\n", i+1, s)
	}

	fmt.Println("[subagent] makespan A/B (real LLM, 3 research subtasks)")
	fmt.Printf("  serial   = %s\n", serial.Round(time.Millisecond))
	fmt.Printf("  parallel = %s\n", parallel.Round(time.Millisecond))
	if parallel > 0 {
		fmt.Printf("  speedup  = %.2fx\n", float64(serial)/float64(parallel))
	}
}

func rebuildRaw(msgs []llm.Message, toolOutputs map[string]string) []llm.Message {
	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		out[i] = m
		if m.Role == llm.RoleTool && m.ToolCallID != "" {
			if raw, ok := toolOutputs[m.ToolCallID]; ok && len(raw) > len(m.Content) {
				out[i].Content = raw
			}
		}
	}
	return out
}

func pct(before, after int) float64 {
	if before == 0 {
		return 0
	}
	return float64(before-after) * 100.0 / float64(before)
}

func ratioF(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func sumInt(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

func countUnpaired(msgs []llm.Message) int {
	have := map[string]bool{}
	for _, m := range msgs {
		if m.Role == llm.RoleTool {
			have[m.ToolCallID] = true
		}
	}
	missing := 0
	for _, m := range msgs {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if !have[tc.ID] {
				missing++
			}
		}
	}
	return missing
}

func die(f string, args ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", args...)
	os.Exit(1)
}
