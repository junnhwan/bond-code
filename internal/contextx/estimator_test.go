package contextx

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/stretchr/testify/assert"
)

func TestEstimator_EstimateTokens(t *testing.T) {
	e := NewEstimator()

	// chars/4 heuristic
	assert.Equal(t, 75, e.EstimateTokens(string(make([]byte, 300))))
	assert.Equal(t, 0, e.EstimateTokens(""))
}

func TestEstimator_EstimateMessage(t *testing.T) {
	e := NewEstimator()

	msg := llm.Message{
		Role:    llm.RoleUser,
		Content: string(make([]byte, 300)), // 75 content + 4 framing
	}

	assert.Equal(t, 79, e.EstimateMessage(msg))
}

func TestEstimator_EstimateMessages(t *testing.T) {
	e := NewEstimator()

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: string(make([]byte, 300))},      // 79
		{Role: llm.RoleAssistant, Content: string(make([]byte, 600))}, // 150+4=154
	}

	assert.Equal(t, 79+154, e.EstimateMessages(messages))
}
