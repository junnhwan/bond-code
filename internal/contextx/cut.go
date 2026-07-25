package contextx

import "github.com/junnhwan/bond-code/internal/llm"

// CutPoint is the Pi-style compaction boundary over a linear message list.
type CutPoint struct {
	// FirstKept is the index in body (messages without leading system) of the
	// first message retained after compaction.
	FirstKept int
	// TurnStart is the user-message index of a split turn, or -1.
	TurnStart int
	// IsSplitTurn is true when FirstKept lands mid-turn (assistant/tool), so the
	// turn prefix must be summarized separately from earlier complete turns.
	IsSplitTurn bool
}

// findCutPoint walks backwards from the end of body, accumulating token
// estimates until keepRecentTokens is reached, then snaps to a valid cut
// (user or assistant — never a bare tool result). Mirrors Pi findCutPoint.
func findCutPoint(body []Message, keepRecentTokens int, est *Estimator) CutPoint {
	if len(body) == 0 {
		return CutPoint{FirstKept: 0, TurnStart: -1}
	}
	if keepRecentTokens <= 0 {
		keepRecentTokens = 20_000
	}
	if est == nil {
		est = NewEstimator()
	}

	cutPoints := validCutIndices(body)
	if len(cutPoints) == 0 {
		return CutPoint{FirstKept: 0, TurnStart: -1}
	}

	accumulated := 0
	cutIndex := cutPoints[0]
	for i := len(body) - 1; i >= 0; i-- {
		accumulated += est.EstimateMessage(body[i])
		if accumulated < keepRecentTokens {
			continue
		}
		// Snap forward to the next valid cut at or after i.
		cutIndex = cutPoints[len(cutPoints)-1]
		for _, c := range cutPoints {
			if c >= i {
				cutIndex = c
				break
			}
		}
		break
	}

	isUser := body[cutIndex].Role == llm.RoleUser
	turnStart := -1
	if !isUser {
		turnStart = findTurnStartIndex(body, cutIndex)
	}
	return CutPoint{
		FirstKept:   cutIndex,
		TurnStart:   turnStart,
		IsSplitTurn: !isUser && turnStart >= 0,
	}
}

// validCutIndices returns indices where a cut is safe: user or assistant.
// Tool results must stay with their tool_use (never cut points).
func validCutIndices(body []Message) []int {
	out := make([]int, 0, len(body))
	for i, msg := range body {
		switch msg.Role {
		case llm.RoleUser, llm.RoleAssistant:
			out = append(out, i)
		}
	}
	return out
}

// findTurnStartIndex walks back to the user message that opened this turn.
func findTurnStartIndex(body []Message, entryIndex int) int {
	for i := entryIndex; i >= 0; i-- {
		if body[i].Role == llm.RoleUser {
			return i
		}
	}
	return -1
}

// splitSystemBody peels a leading system message so cuts never drop it.
func splitSystemBody(messages []Message) (system []Message, body []Message) {
	if len(messages) > 0 && messages[0].Role == llm.RoleSystem {
		return []Message{messages[0]}, messages[1:]
	}
	return nil, messages
}
