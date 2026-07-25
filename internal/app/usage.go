package app

import "github.com/junnhwan/bond-code/internal/agent"

// recordMeasuredUsage stores the most recent real token counts reported by the
// model API, so /status can show true context-window occupancy instead of the
// governor's chars/3 estimate. input_tokens (message_start) and output_tokens
// (message_delta) arrive in separate events, so each only overwrites its half.
func (a *App) recordMeasuredUsage(event agent.Event) {
	a.measuredMu.Lock()
	defer a.measuredMu.Unlock()
	if event.MeasuredInputTokens > 0 {
		a.lastInputTokens = event.MeasuredInputTokens
	}
	if event.MeasuredOutputTokens > 0 {
		a.lastOutputTokens = event.MeasuredOutputTokens
	}
	if event.MeasuredUsageFinal {
		a.measuredCalls++
		a.totalInputTokens += event.MeasuredInputTokens
		a.totalOutputTokens += event.MeasuredOutputTokens
	}
}

// MeasuredUsage returns the most recent real token counts the model reported,
// if any. Input is the true current context-window occupancy.
func (a *App) MeasuredUsage() (input, output int) {
	a.measuredMu.Lock()
	defer a.measuredMu.Unlock()
	return a.lastInputTokens, a.lastOutputTokens
}

func (a *App) UsageSnapshot() UsageStatus {
	a.measuredMu.Lock()
	defer a.measuredMu.Unlock()
	return UsageStatus{
		ModelCalls:        a.measuredCalls,
		LastInputTokens:   a.lastInputTokens,
		LastOutputTokens:  a.lastOutputTokens,
		TotalInputTokens:  a.totalInputTokens,
		TotalOutputTokens: a.totalOutputTokens,
	}
}

func (a *App) resetMeasuredUsage() {
	a.measuredMu.Lock()
	defer a.measuredMu.Unlock()
	a.lastInputTokens = 0
	a.lastOutputTokens = 0
	a.totalInputTokens = 0
	a.totalOutputTokens = 0
	a.measuredCalls = 0
}
