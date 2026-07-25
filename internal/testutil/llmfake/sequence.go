// Package llmfake provides reusable LLM clients for tests outside the llm
// package. It deliberately stays out of the production llm API.
package llmfake

import (
	"context"
	"sync"

	"github.com/junnhwan/bond-code/internal/llm"
)

// Sequence returns one configured chunk stream per call. Calls beyond the
// configured responses return an empty successful stream.
type Sequence struct {
	responses [][]llm.Chunk
	errors    []error

	mu    sync.Mutex
	calls int
	last  []llm.Message
}

// New constructs a successful sequence client.
func New(responses [][]llm.Chunk) *Sequence {
	return NewWithErrors(responses, nil)
}

// NewWithErrors constructs a sequence client that emits errors[i] after
// responses[i]. A nil entry represents a successful stream.
func NewWithErrors(responses [][]llm.Chunk, errors []error) *Sequence {
	return &Sequence{responses: responses, errors: errors}
}

func (f *Sequence) Stream(ctx context.Context, messages []llm.Message, _ []llm.ToolSpec) (<-chan llm.Chunk, <-chan error) {
	f.mu.Lock()
	responseIndex := f.calls
	f.calls++
	f.last = append([]llm.Message(nil), messages...)
	f.mu.Unlock()

	chunks := make(chan llm.Chunk)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)

		if responseIndex < len(f.responses) {
			for _, chunk := range f.responses[responseIndex] {
				select {
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				case chunks <- chunk:
				}
			}
		}
		if responseIndex < len(f.errors) && f.errors[responseIndex] != nil {
			errs <- f.errors[responseIndex]
			return
		}
		errs <- nil
	}()
	return chunks, errs
}

// Calls reports the number of Stream calls.
func (f *Sequence) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// LastMessages returns a copy of the latest Stream input.
func (f *Sequence) LastMessages() []llm.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llm.Message(nil), f.last...)
}
