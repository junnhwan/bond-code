package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/hook"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

type LoopConfig struct {
	MaxSteps             int
	MaxRepeatedToolCalls int
	MaxToolCallsPerStep  int
	StopOnNoProgress     bool
	// MaxEmptyResponseRetries caps how many times a single step re-rolls the
	// model after an empty response (no text, no tool call). An empty response
	// almost always means max_tokens truncation while the model was still
	// thinking, or a provider hiccup — the loop nudges and retries instead of
	// silently ending a long task. Resets each step. <= 0 defaults to 2.
	MaxEmptyResponseRetries int
	// MaxRepeatedTextChunks trips the text-degeneration circuit breaker after
	// this many consecutive identical text chunks — a token-level stuck loop
	// (e.g. one token replayed hundreds of times, streaming for minutes until
	// the provider stops). loopGuard only covers repeated *tool calls*, so
	// without this a degenerate pure-text response runs unbounded. <= 0
	// defaults to 16.
	MaxRepeatedTextChunks int
	// MaxRepeatedTextSubstrings trips the breaker after this many sliding-window
	// probe hits — a phrase/sentence-level loop where chunks differ slightly
	// but a ~48-char window keeps re-appearing earlier in the answer. Catches
	// repetition the exact-chunk gate misses. <= 0 defaults to 3.
	MaxRepeatedTextSubstrings int
}

// emptyResponseNudge is appended (as a user turn) when the model emits a
// totally empty response — no text, no tool call, and no truncation signal.
// This usually means thinking consumed the whole budget with nothing left to
// show; the nudge asks it to actually emit its answer or next tool call.
const emptyResponseNudge = "[system] Your previous response produced no visible output and no tool call. Pick up exactly where you left off and emit your text answer or the next tool call now — do not restate progress, just continue."

// truncatedResponseNudge is appended when the provider signals the response was
// cut off by max_tokens (stop_reason=max_tokens / length), possibly mid-text.
// The wording tells the model to continue from where it stopped without
// re-deriving what it already output, so the retry spends its budget on
// finishing rather than re-thinking.
const truncatedResponseNudge = "[system] Your previous response was cut off by max_tokens before you finished. Continue from exactly where you stopped — do NOT re-derive or restate what you already output above; just emit the remaining text answer or the next tool call now."

// isTruncatedStopReason reports whether a provider stop_reason means the
// response was cut off mid-output rather than naturally ending. Anthropic uses
// "max_tokens"; some OpenAI-compatible gateways use "length". Both mean the
// model had more to say and the loop must not treat the partial output as a
// final answer.
func isTruncatedStopReason(reason string) bool {
	return reason == "max_tokens" || reason == "length"
}

// isIncompleteResponse reports whether a no-tool-call response is incomplete
// and should be retried within the same step. It is incomplete when the
// provider signals truncation (max_tokens / length — e.g. thinking ate the
// budget and the text was cut mid-sentence) OR when it is totally empty (no
// text, no tool call, no signal — usually thinking ate the whole budget). A
// genuine end_turn with a real textual answer is complete. A response that
// made a tool call is always treated as complete: the loop will execute the
// tool and that carries the work forward, so truncation of surrounding text is
// not recoverable there.
func isIncompleteResponse(stopReason string, requested []llm.ToolCall, text string) bool {
	if len(requested) > 0 {
		return false
	}
	if isTruncatedStopReason(stopReason) || stopReason == "tool_use" {
		return true
	}
	return strings.TrimSpace(text) == ""
}

// responseRetryNudge picks the nudge wording for an incomplete response: a
// targeted "continue from where you stopped" message for truncation, the
// generic empty-response nudge otherwise.
func responseRetryNudge(stopReason string) string {
	if isTruncatedStopReason(stopReason) {
		return truncatedResponseNudge
	}
	return emptyResponseNudge
}

type Loop struct {
	cfg       LoopConfig
	client    llm.Client
	tools     *tool.Registry
	policy    safety.Policy
	confirmer safety.Confirmer

	contextManager   *contextx.Manager
	maxContextTokens int
	contextSummary   *contextx.SummaryStore
	// disabledTools are tool names blocked for the current run (e.g. the
	// mutating tools while in plan mode). executeTool rejects them as blocked.
	disabledTools map[string]bool
	// hooks 注册表；nil 时无钩子，loop 行为不变。钩子在 safety 决策通过之后运行，
	// 是 safety 之外的扩展点，不替代 safety 的 blocked 不可绕过不变量。
	hooks *hook.Registry
	// llmSummaryEnabled is retained for bootstrap API compatibility; per-turn
	// snip-triggered LLM summary was removed. Compaction is App-level + reactive.
	llmSummaryEnabled bool
	// steering 是运行中插队指令的只读通道；step 循环每轮非阻塞读，有则注入为 user
	// message，实现"运行中纠偏"而无需中断重启（hands-off）。
	steering <-chan string
	// debugLogger records model-decision facts (governed request, response with
	// cache breakdown, tool/decide) to an opt-in per-session trace. nil = off,
	// so the hot-path guard is the only cost when debug tracing is disabled.
	debugLogger observe.Logger
}

type RunResult struct {
	FinalAnswer string
	Trace       Trace
	Messages    []llm.Message
}

type EventSink func(Event)

func NewLoop(cfg LoopConfig, client llm.Client, registry *tool.Registry, policy safety.Policy, confirmer safety.Confirmer) *Loop {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 8
	}
	if cfg.MaxRepeatedToolCalls <= 0 {
		cfg.MaxRepeatedToolCalls = 3
	}
	if cfg.MaxToolCallsPerStep <= 0 {
		cfg.MaxToolCallsPerStep = 8
	}
	if cfg.MaxEmptyResponseRetries <= 0 {
		cfg.MaxEmptyResponseRetries = 2
	}
	if cfg.MaxRepeatedTextChunks <= 0 {
		cfg.MaxRepeatedTextChunks = 16
	}
	if cfg.MaxRepeatedTextSubstrings <= 0 {
		cfg.MaxRepeatedTextSubstrings = 3
	}
	if confirmer == nil {
		confirmer = safety.StaticConfirmer(false)
	}
	cfg.StopOnNoProgress = true
	return &Loop{cfg: cfg, client: client, tools: registry, policy: policy, confirmer: confirmer}
}

// confirm prefers the Phase 5A DetailedConfirmer (reject reason + Allow-always
// persistence on the confirmer's own side) and falls back to the base Confirmer
// for legacy/test confirmers. This keeps the loop compatible with both kinds
// while letting the TUI surface richer confirmation UX. The reject reason, when
// present, is folded into the tool-result envelope so the model can adjust.
func (l *Loop) confirm(ctx context.Context, req safety.ConfirmationRequest) (safety.Response, error) {
	if dc, ok := l.confirmer.(safety.DetailedConfirmer); ok {
		return dc.ConfirmDetailed(ctx, req)
	}
	approved, err := l.confirmer.Confirm(ctx, req)
	return safety.Response{Approved: approved}, err
}

func (l *Loop) SetContextManager(manager *contextx.Manager, maxTokens int) {
	l.contextManager = manager
	l.maxContextTokens = maxTokens
}

func (l *Loop) SetContextSummaryStore(store *contextx.SummaryStore) {
	l.contextSummary = store
}

// SetDisabledTools blocks the named tools for subsequent runs; executeTool
// rejects them as "blocked" before they run. Pass nil/empty to clear.
func (l *Loop) SetDisabledTools(names []string) {
	l.disabledTools = nil
	for _, name := range names {
		if l.disabledTools == nil {
			l.disabledTools = map[string]bool{}
		}
		l.disabledTools[name] = true
	}
}

func (l *Loop) isToolDisabled(name string) bool {
	return l.disabledTools != nil && l.disabledTools[name]
}

// parallelSafeTools 是无副作用、只读、policy 必 Allow 的工具，可安全并发执行
// （guard 已串行预检、无 confirm、emit 加锁保护 trace）。
var parallelSafeTools = map[string]bool{
	"read_file":   true,
	"list_dir":    true,
	"search_text": true,
}

func isParallelSafeTool(name string) bool { return parallelSafeTools[name] }

// SetHooks 注入生命周期钩子注册表；nil 表示无钩子（loop 行为不变）。
func (l *Loop) SetHooks(r *hook.Registry) { l.hooks = r }

// SetLLMSummaryEnabled 控制上下文摘要是否走 LLM 语义压缩（L4）。默认 false（规则截断），
// bootstrap 对真实 client 启用；失败时自动降级回规则截断。
func (l *Loop) SetLLMSummaryEnabled(enabled bool) { l.llmSummaryEnabled = enabled }

// SetSteering 注入一个运行中指令通道；loop 每轮非阻塞读，收到则注入为 user message，
// 让长任务跑偏时能即时纠偏而不必中断重启。
func (l *Loop) SetSteering(ch <-chan string) { l.steering = ch }

// SetDebugLogger injects an opt-in debug trace logger; nil keeps loop behavior
// unchanged (every debug site guards on l.debugLogger != nil, so the hot path
// pays nothing when tracing is off).
func (l *Loop) SetDebugLogger(logger observe.Logger) { l.debugLogger = logger }

// SetClient swaps the LLM client used for subsequent steps, enabling runtime
// model reconfiguration (/model) without restarting the loop: the replacement
// carries its own model name, so the next Stream call uses it. An in-flight step
// is unaffected (Stream is already running); the swap lands on the next
// govern→stream cycle. Callers must not run this concurrently with
// RunMessagesWithEvents — app.SwitchModel guards with the app mutex so a
// mid-turn swap fails fast (TryLock) instead of racing the active stream.
func (l *Loop) SetClient(client llm.Client) { l.client = client }

func (l *Loop) Run(ctx context.Context, prompt string) (*RunResult, error) {
	return l.RunWithEvents(ctx, prompt, nil)
}

func (l *Loop) RunWithEvents(ctx context.Context, prompt string, sink EventSink) (*RunResult, error) {
	return l.RunMessagesWithEvents(ctx, buildMessages(prompt), sink)
}

func (l *Loop) RunMessagesWithEvents(ctx context.Context, initialMessages []llm.Message, sink EventSink) (*RunResult, error) {
	trace := Trace{}
	var emitMu sync.Mutex
	emit := func(event Event) {
		emitMu.Lock()
		added := trace.AddEvent(event)
		emitMu.Unlock()
		if sink != nil {
			sink(added)
		}
	}
	emit(Event{Type: EventAgentStarted, Message: lastUserMessage(initialMessages)})
	if l.hooks != nil {
		defer l.hooks.RunStop(ctx)
	}
	messages := append([]llm.Message(nil), initialMessages...)
	// UserPromptSubmit 钩子：改写最后一条 user message。
	if l.hooks != nil {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == llm.RoleUser {
				in := l.hooks.RunUserPromptSubmit(ctx, hook.UserPromptSubmitInput{Prompt: messages[i].Content})
				if in.Prompt != messages[i].Content {
					messages[i].Content = in.Prompt
				}
				break
			}
		}
	}
	guard := newLoopGuard(l.cfg)
	toolStepsWithoutTodo := 0
	planningReminderInjected := false
	stepBudgetFallback := ""

	for step := 0; step < l.cfg.MaxSteps; step++ {
		// steering：非阻塞读运行中插队指令，注入为 user message（运行中纠偏）。
		if l.steering != nil {
			select {
			case msg := <-l.steering:
				if strings.TrimSpace(msg) != "" {
					messages = append(messages, llm.Message{Role: llm.RoleUser, Content: "[steering] " + msg})
				}
			default:
			}
		}
		if !planningReminderInjected && toolStepsWithoutTodo >= 3 && !hasTaskContext(messages) {
			messages = appendPlanningReminder(messages)
			planningReminderInjected = true
		}
		var response modelResponse
		reactiveCompacted := false
		emptyRetries := 0
		// prepare (tool-result governance) → stream → collect。
		// prompt_too_long 时 emergency compact 一次后重试本步；语义 compact 在 App
		// 层（turn 前阈值 / /compact）完成，loop 内不做 snip。
		for {
			modelMessages := l.prepareModelMessages(ctx, messages, emit, step)
			l.debugLLMReq(step, modelMessages)
			var err error
			response, err = l.collectModelResponse(ctx, modelMessages, l.toolSpecs(), emit)
			if err == nil {
				if !response.degenerate && isTruncatedStopReason(response.stopReason) && len(response.toolCalls) == 0 && strings.TrimSpace(response.text) == "" && response.sawReasoning {
					msg := "model response hit max_tokens during reasoning before producing visible output. BondCode cannot safely resume provider reasoning blocks, so it stopped instead of restarting the same hidden trajectory; increase model.max_tokens or reduce thinking.budget_tokens."
					emit(Event{Type: EventAgentError, Message: msg, Error: msg})
					return &RunResult{Trace: trace, Messages: append([]llm.Message(nil), messages...)}, fmt.Errorf("%s", msg)
				}
				// Incomplete response: the provider cut the output off
				// mid-stream (stop_reason=max_tokens / length — e.g. thinking
				// ate the budget and the text was truncated mid-sentence) or
				// produced nothing at all. Either way it is not a real
				// end_turn; treating it as finished silently loses long tasks.
				// Nudge and retry within the same step so the model continues
				// from where it stopped instead of re-deriving.
				if emptyRetries < l.cfg.MaxEmptyResponseRetries && isIncompleteResponse(response.stopReason, response.toolCalls, response.text) {
					emptyRetries++
					emit(Event{
						Type:    EventContextUpdated,
						Message: fmt.Sprintf("incomplete model response (stop_reason=%q, text_bytes=%d, tool_calls=%d) — nudging and retrying (%d/%d)", response.stopReason, len(response.text), len(response.toolCalls), emptyRetries, l.cfg.MaxEmptyResponseRetries),
					})
					// Replay the partial assistant text before the nudge so the
					// model sees what it already emitted and continues instead
					// of restating it.
					messages = append(messages,
						llm.Message{Role: llm.RoleAssistant, Content: response.text},
						llm.Message{Role: llm.RoleUser, Content: responseRetryNudge(response.stopReason)})
					continue
				}
				break
			}
			if llm.IsPromptTooLong(err) && l.contextManager != nil && !reactiveCompacted {
				reactiveCompacted = true
				messages = l.recoverFromPromptTooLong(messages, emit)
				continue
			}
			emit(Event{Type: EventAgentError, Message: err.Error(), Error: err.Error()})
			return &RunResult{Trace: trace, Messages: append([]llm.Message(nil), messages...)}, err
		}
		l.debugLLMResp(step, response.text, response.toolCalls, response.stopReason, response.usage)
		assistantText := response.text
		if response.degenerate {
			assistantText = truncateDegenerate(assistantText, response.healthyTextLen)
		}
		if len(response.toolCalls) == 0 {
			if response.degenerate {
				// Breaker tripped on pure text: drop the degenerated text and
				// give the model one no-tools recovery turn; fall back to a
				// truncated notice if recovery yields nothing.
				if final, finalErr := l.finalizeAfterTextDegeneration(ctx, messages, emit, step); finalErr == nil && strings.TrimSpace(final) != "" {
					emit(Event{Type: EventAgentFinished, Message: final})
					messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: final})
					return &RunResult{FinalAnswer: final, Trace: trace, Messages: append([]llm.Message(nil), messages...)}, nil
				}
				final := textDegenerationFallback(assistantText)
				emit(Event{Type: EventAgentFinished, Message: final})
				messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: final})
				return &RunResult{FinalAnswer: final, Trace: trace, Messages: append([]llm.Message(nil), messages...)}, nil
			}
			final := response.text
			if isIncompleteResponse(response.stopReason, nil, final) {
				// Retries exhausted and the response is still incomplete
				// (truncated by max_tokens, or totally empty). Surface this as
				// an explicit error with a fix hint instead of silently
				// agent_finished-ing with a half/empty message — the old
				// behavior made long tasks vanish mid-run. The usual cause is
				// thinking.budget_tokens consuming most of model.max_tokens.
				msg := fmt.Sprintf("model response still incomplete after %d retries; stop_reason=%q, text_bytes=%d. This usually means thinking.budget_tokens consumed most of model.max_tokens — raise model.max_tokens well above thinking.budget_tokens so the model has room to finish its output after thinking.", emptyRetries, response.stopReason, len(final))
				emit(Event{Type: EventAgentError, Message: msg, Error: msg})
				return &RunResult{Trace: trace, Messages: append([]llm.Message(nil), messages...)}, fmt.Errorf("%s", msg)
			}
			emit(Event{Type: EventAgentFinished, Message: final})
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: final})
			return &RunResult{FinalAnswer: final, Trace: trace, Messages: append([]llm.Message(nil), messages...)}, nil
		}
		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: assistantText, ToolCalls: append([]llm.ToolCall(nil), response.toolCalls...)})
		replay, err := l.executeAndReplayTools(ctx, messages, response.toolCalls, guard, emit, step, stepBudgetFallback)
		messages = replay.messages
		stepBudgetFallback = replay.stepBudgetFallback
		if err != nil {
			return &RunResult{Trace: trace, Messages: append([]llm.Message(nil), messages...)}, err
		}
		if replay.noProgressGuard != "" {
			if final, finalErr := l.finalizeAfterNoProgressGuard(ctx, messages, emit, step); finalErr == nil {
				emit(Event{Type: EventAgentFinished, Message: final})
				messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: final})
				return &RunResult{FinalAnswer: final, Trace: trace, Messages: append([]llm.Message(nil), messages...)}, nil
			} else {
				err := fmt.Errorf("%s finalization failed: %w", replay.noProgressGuard, finalErr)
				emit(Event{Type: EventAgentError, Message: err.Error(), Error: err.Error()})
				return &RunResult{Trace: trace, Messages: append([]llm.Message(nil), messages...)}, err
			}
		}
		if replay.stepUsedTodo {
			toolStepsWithoutTodo = 0
		} else {
			toolStepsWithoutTodo++
		}

	}
	if l.cfg.MaxSteps > 1 && strings.TrimSpace(stepBudgetFallback) != "" {
		if final, finalErr := l.finalizeAfterStepBudget(ctx, messages, emit, stepBudgetFallback); finalErr == nil {
			emit(Event{Type: EventAgentFinished, Message: final})
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: final})
			return &RunResult{FinalAnswer: final, Trace: trace, Messages: append([]llm.Message(nil), messages...)}, nil
		} else {
			err := fmt.Errorf("agent step budget exhausted and finalization failed: %w", finalErr)
			emit(Event{Type: EventAgentError, Message: err.Error(), Error: err.Error()})
			return &RunResult{Trace: trace, Messages: append([]llm.Message(nil), messages...)}, err
		}
	}
	err := fmt.Errorf("agent stopped after max steps")
	emit(Event{Type: EventAgentError, Message: err.Error(), Error: err.Error()})
	return &RunResult{Trace: trace, Messages: append([]llm.Message(nil), messages...)}, err
}

func lastUserMessage(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			return messages[i].Content
		}
	}
	return ""
}
