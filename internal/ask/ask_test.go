package ask

import (
	"context"
	"strings"
	"testing"
)

type fakeQuestioner struct {
	answer Answer
	err    error
	asked  Question
	calls  int
}

func (f *fakeQuestioner) Ask(_ context.Context, q Question) (Answer, error) {
	f.calls++
	f.asked = q
	return f.answer, f.err
}

func TestAskUserToolReturnsSelectedLabel(t *testing.T) {
	q := &fakeQuestioner{answer: Answer{1}}
	tool := NewAskUserTool(q)
	res, err := tool.Execute(context.Background(), []byte(`{"prompt":"which?","options":[{"label":"A"},{"label":"B"},{"label":"C"}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK result, got %#v", res)
	}
	if res.Output != "B" {
		t.Fatalf("expected output \"B\", got %q", res.Output)
	}
	if q.asked.Prompt != "which?" {
		t.Fatalf("prompt not forwarded to questioner: %#v", q.asked)
	}
	if q.asked.Multi {
		t.Fatalf("expected Multi=false by default")
	}
}

func TestAskUserToolMultiForwardsAndJoinsLabels(t *testing.T) {
	q := &fakeQuestioner{answer: Answer{0, 2}}
	tool := NewAskUserTool(q)
	res, _ := tool.Execute(context.Background(), []byte(`{"prompt":"pick","options":[{"label":"A"},{"label":"B"},{"label":"C"}],"multi":true}`))
	if res.Output != "A\nC" {
		t.Fatalf("expected \"A\\nC\", got %q", res.Output)
	}
	if !q.asked.Multi {
		t.Fatalf("expected Multi to be forwarded as true")
	}
}

func TestAskUserToolRejectsMissingPrompt(t *testing.T) {
	tool := NewAskUserTool(&fakeQuestioner{})
	res, _ := tool.Execute(context.Background(), []byte(`{"options":[{"label":"A"}]}`))
	if res.OK {
		t.Fatalf("expected error result for missing prompt")
	}
	if !strings.Contains(res.Error, "prompt") {
		t.Fatalf("expected prompt-related error, got %q", res.Error)
	}
}

func TestAskUserToolRejectsMissingOptions(t *testing.T) {
	tool := NewAskUserTool(&fakeQuestioner{})
	res, _ := tool.Execute(context.Background(), []byte(`{"prompt":"x"}`))
	if res.OK {
		t.Fatalf("expected error result for missing options")
	}
}

func TestAskUserToolNilQuestionerErrors(t *testing.T) {
	tool := NewAskUserTool(nil)
	res, _ := tool.Execute(context.Background(), []byte(`{"prompt":"x","options":[{"label":"A"}]}`))
	if res.OK {
		t.Fatalf("expected error result when questioner is nil")
	}
}

func TestAskUserToolAskErrorPropagates(t *testing.T) {
	tool := NewAskUserTool(&fakeQuestioner{err: context.Canceled})
	res, _ := tool.Execute(context.Background(), []byte(`{"prompt":"x","options":[{"label":"A"}]}`))
	if res.OK {
		t.Fatalf("expected error result when Ask fails")
	}
}
