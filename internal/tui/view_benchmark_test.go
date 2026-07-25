package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
)

const benchmarkStreamWindow = 100

var benchmarkStreamTime = time.Unix(1_700_000_000, 0)

func newTUIBenchmarkModel(turns, bodyBytes int) Model {
	m := NewModel(Config{}).SetSize(120, 40)
	body := strings.Repeat("historical output line with enough words to wrap across the terminal width.\n", max(1, bodyBytes/76))
	for i := 0; i < turns; i++ {
		m.timeline = m.timeline.StartUserTurn(fmt.Sprintf("historical prompt %d", i))
		m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", body)
	}
	m.timeline = m.timeline.StartUserTurn("active prompt")
	m.agent.Busy = true
	_ = m.View()
	return m
}

func runFixedWindowStreamBenchmark(b *testing.B, setup func() Model, step func(Model) Model) {
	b.Helper()
	m := setup()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i > 0 && i%benchmarkStreamWindow == 0 {
			b.StopTimer()
			m = setup()
			b.StartTimer()
		}
		m = step(m)
	}
}

func BenchmarkTUIViewIdleLongSession(b *testing.B) {
	m := newTUIBenchmarkModel(300, 1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkTUIViewSpinnerTickLongSession(b *testing.B) {
	benchmarkTUIViewSpinnerTick(b, 300, 1024)
}

func BenchmarkTUIViewSpinnerTickVeryLongSession(b *testing.B) {
	benchmarkTUIViewSpinnerTick(b, 1000, 4096)
}

func benchmarkTUIViewSpinnerTick(b *testing.B, turns, bodyBytes int) {
	m := newTUIBenchmarkModel(turns, bodyBytes)
	m.timeline = m.timeline.MarkAgentStarted(time.Now())
	_ = m.View()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next, _ := m.Update(m.spinner.Tick())
		m = next.(Model)
		_ = m.View()
	}
}

func BenchmarkTUIViewStreamDeltaLongSession(b *testing.B) {
	chunk := strings.Repeat("streamed output word ", 16)
	runFixedWindowStreamBenchmark(b,
		func() Model {
			return newTUIBenchmarkModel(300, 1024)
		},
		func(m Model) Model {
			m = m.applyAssistantChunk(chunk, benchmarkStreamTime)
			_ = m.View()
			return m
		},
	)
}

func BenchmarkTUIViewStreamDeltaVeryLongSession(b *testing.B) {
	chunk := strings.Repeat("streamed output word ", 16)
	runFixedWindowStreamBenchmark(b,
		func() Model {
			return newTUIBenchmarkModel(1000, 4096)
		},
		func(m Model) Model {
			m = m.applyAssistantChunk(chunk, benchmarkStreamTime)
			_ = m.View()
			return m
		},
	)
}

func BenchmarkTUIViewHugeActiveResponse(b *testing.B) {
	chunk := strings.Repeat("streamed output word ", 16)
	runFixedWindowStreamBenchmark(b,
		func() Model {
			m := newTUIBenchmarkModel(0, 0)
			m = m.applyAssistantChunk(strings.Repeat("active streamed line with words to wrap across the terminal.\n", 16000), benchmarkStreamTime)
			_ = m.View()
			return m
		},
		func(m Model) Model {
			m = m.applyAssistantChunk(chunk, benchmarkStreamTime)
			_ = m.View()
			return m
		},
	)
}

func BenchmarkTUIViewStreamDeltaToolRichActiveTurn(b *testing.B) {
	chunk := strings.Repeat(" streamed", 16)
	runFixedWindowStreamBenchmark(b,
		func() Model {
			m := NewModel(Config{}).SetSize(120, 40)
			m.timeline = m.timeline.StartUserTurn("active multi-step prompt")
			body := strings.Repeat("completed assistant analysis with markdown words. ", 24)
			for i := 0; i < 200; i++ {
				m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", body)
				m.timeline = m.timeline.AppendBlock(BlockTool, fmt.Sprintf("tool %d", i), "completed tool output")
			}
			m.agent.Busy = true
			m = m.applyAssistantChunk("streaming", benchmarkStreamTime)
			_ = m.View()
			return m
		},
		func(m Model) Model {
			m = m.applyAssistantChunk(chunk, benchmarkStreamTime)
			_ = m.View()
			return m
		},
	)
}

func BenchmarkTUIStreamUpdateAndViewHugeActiveResponse(b *testing.B) {
	chunkEvent := agentEventMsg{event: agent.Event{
		Type:      agent.EventModelChunk,
		Message:   strings.Repeat("streamed output word ", 16),
		CreatedAt: benchmarkStreamTime,
	}}
	runFixedWindowStreamBenchmark(b,
		func() Model {
			m := newTUIBenchmarkModel(0, 0)
			m = m.applyAssistantChunk(strings.Repeat("active streamed line with words to wrap across the terminal.\n", 16000), benchmarkStreamTime)
			_ = m.View()
			return m
		},
		func(m Model) Model {
			next, _ := m.Update(chunkEvent)
			m = next.(Model)
			_ = m.View()
			return m
		},
	)
}

func BenchmarkTUIViewHugeLiveReasoningFolded(b *testing.B) {
	benchmarkTUIViewHugeLiveReasoning(b, false)
}

func BenchmarkTUIViewHugeLiveReasoningExpanded(b *testing.B) {
	benchmarkTUIViewHugeLiveReasoning(b, true)
}

func benchmarkTUIViewHugeLiveReasoning(b *testing.B, expanded bool) {
	chunk := strings.Repeat(" continued reasoning", 16)
	runFixedWindowStreamBenchmark(b,
		func() Model {
			m := newTUIBenchmarkModel(0, 0)
			m.showThinking = expanded
			m = m.ApplyAgentEvent(agent.Event{
				Type:      agent.EventReasoningChunk,
				Message:   strings.Repeat("reasoning line with enough content to display.\n", 16000),
				CreatedAt: benchmarkStreamTime,
			})
			_ = m.View()
			return m
		},
		func(m Model) Model {
			m = m.ApplyAgentEvent(agent.Event{
				Type:      agent.EventReasoningChunk,
				Message:   chunk,
				CreatedAt: benchmarkStreamTime,
			})
			_ = m.View()
			return m
		},
	)
}

func BenchmarkTUIToolRequestAfterLargeStream(b *testing.B) {
	largeBody := strings.Repeat("active streamed markdown line with **bold** text.\n", 1024)
	toolEvent := agent.Event{
		Type:       agent.EventToolRequested,
		ToolName:   "read_file",
		ToolCallID: "benchmark-tool",
		Input:      `{"path":"README.md"}`,
		CreatedAt:  benchmarkStreamTime,
	}
	setup := func() Model {
		m := newTUIBenchmarkModel(300, 1024)
		m = m.applyAssistantChunk(largeBody, benchmarkStreamTime)
		_ = m.View()
		return m
	}
	guard := setup().ApplyAgentEvent(toolEvent)
	blocks := guard.timeline.Turns[len(guard.timeline.Turns)-1].Blocks
	if len(blocks) < 2 || blocks[len(blocks)-2].Kind != BlockAssistant || blocks[len(blocks)-1].Kind != BlockTool {
		b.Fatalf("benchmark fixture lost assistant/tool ordering: %#v", blocks)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		m := setup()
		b.StartTimer()
		m = m.ApplyAgentEvent(toolEvent)
		_ = m.View()
	}
}

func BenchmarkTUIStreamUpdateAndView(b *testing.B) {
	for _, tc := range []struct {
		name  string
		turns int
		chunk string
	}{
		{name: "NoNewline300Turns", turns: 300, chunk: " partial"},
		{name: "NoNewline1000Turns", turns: 1000, chunk: " partial"},
		{name: "CompletesLine300Turns", turns: 300, chunk: " completed\n"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			setup := func() Model {
				m := newTUIBenchmarkModel(tc.turns, 1024)
				m = m.applyAssistantChunk("already visible\nunfinished", benchmarkStreamTime)
				_ = m.View()
				return m
			}
			guard := setup()
			beforeVersion := guard.timeline.Version
			beforeGeneration := guard.agent.LiveStream.generation
			next, _ := guard.Update(agentEventMsg{event: agent.Event{
				Type: agent.EventModelChunk, Message: tc.chunk, CreatedAt: benchmarkStreamTime,
			}})
			guard = next.(Model)
			if guard.timeline.Version != beforeVersion {
				b.Fatalf("delta changed timeline version: got %d, want %d", guard.timeline.Version, beforeVersion)
			}
			if guard.agent.LiveStream == nil || guard.agent.LiveStream.kind != BlockAssistant ||
				guard.agent.LiveStream.generation != beforeGeneration {
				b.Fatalf("delta lost live stream identity: %#v", guard.agent.LiveStream)
			}
			if !strings.Contains(guard.agent.LiveStream.body, tc.chunk) {
				b.Fatalf("delta missing from live body")
			}
			if strings.Contains(tc.chunk, "\n") && !strings.Contains(strings.Join(guard.renderLiveStreamLines(120), "\n"), "unfinished completed") {
				b.Fatalf("line-completing delta did not become visible")
			}

			event := agentEventMsg{event: agent.Event{
				Type: agent.EventModelChunk, Message: tc.chunk, CreatedAt: benchmarkStreamTime,
			}}
			runFixedWindowStreamBenchmark(b, setup, func(m Model) Model {
				next, _ := m.Update(event)
				m = next.(Model)
				_ = m.View()
				return m
			})
		})
	}
}
