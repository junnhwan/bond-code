package contextx

// EstimateTokens provides a fast chars/4 heuristic (aligned with Pi compaction).
// It slightly over-counts dense English and under-counts CJK; good enough for
// cut points and thresholds without a tokenizer dependency.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 3) / 4
}

// Estimator estimates tokens for messages and lists.
type Estimator struct{}

func NewEstimator() *Estimator {
	return &Estimator{}
}

func (e *Estimator) EstimateTokens(text string) int {
	return EstimateTokens(text)
}

func (e *Estimator) EstimateMessage(msg Message) int {
	total := e.EstimateTokens(msg.Content)
	for _, tc := range msg.ToolCalls {
		total += e.EstimateTokens(tc.Name)
		total += e.EstimateTokens(tc.Arguments)
	}
	// Small fixed overhead per message for role framing.
	total += 4
	return total
}

func (e *Estimator) EstimateMessages(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += e.EstimateMessage(msg)
	}
	return total
}
