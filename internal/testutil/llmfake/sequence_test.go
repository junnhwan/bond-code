package llmfake

import (
	"context"
	"errors"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
)

func TestSequenceStreamsResponsesInCallOrder(t *testing.T) {
	fake := New([][]llm.Chunk{
		{{Content: "first"}},
		{{Content: "second"}},
	})

	firstMessages := []llm.Message{{Role: llm.RoleUser, Content: "one"}}
	if got, err := drain(t, fake, firstMessages); err != nil || got != "first" {
		t.Fatalf("first stream = %q, %v; want first, nil", got, err)
	}
	secondMessages := []llm.Message{{Role: llm.RoleUser, Content: "two"}}
	if got, err := drain(t, fake, secondMessages); err != nil || got != "second" {
		t.Fatalf("second stream = %q, %v; want second, nil", got, err)
	}

	if got := fake.Calls(); got != 2 {
		t.Fatalf("Calls() = %d, want 2", got)
	}
	last := fake.LastMessages()
	if len(last) != 1 || last[0].Content != "two" {
		t.Fatalf("LastMessages() = %#v, want second call messages", last)
	}
	last[0].Content = "mutated"
	if got := fake.LastMessages()[0].Content; got != "two" {
		t.Fatalf("LastMessages leaked internal slice: got %q", got)
	}
}

func TestSequenceEmitsConfiguredErrorAfterChunks(t *testing.T) {
	wantErr := errors.New("stream failed")
	fake := NewWithErrors(
		[][]llm.Chunk{{{Content: "partial"}}},
		[]error{wantErr},
	)

	got, err := drain(t, fake, nil)
	if got != "partial" {
		t.Fatalf("content = %q, want partial", got)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func drain(t *testing.T, client llm.Client, messages []llm.Message) (string, error) {
	t.Helper()
	chunks, errs := client.Stream(context.Background(), messages, nil)
	var content string
	for chunk := range chunks {
		content += chunk.Content
	}
	return content, <-errs
}
