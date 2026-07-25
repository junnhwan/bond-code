package llm

import (
	"context"
	"testing"
)

func TestFakeClientStreamsConfiguredChunks(t *testing.T) {
	client := NewFakeClient([]Chunk{
		{Content: "hello"},
		{Done: true},
	})

	chunks, errs := client.Stream(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	var got string
	for chunk := range chunks {
		got += chunk.Content
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}

	if got != "hello" {
		t.Fatalf("expected streamed hello, got %q", got)
	}
}
