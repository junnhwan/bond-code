package session

import (
	"strings"
	"testing"
)

// 线性事件（无 ParentID）退化为多个独立根，兼容旧 JSONL。
func TestBuildTreeLinearEventsAreRoots(t *testing.T) {
	events := []Event{
		{EventID: "a"},
		{EventID: "b"},
	}
	roots := BuildTree(events)
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots for linear events, got %d", len(roots))
	}
}

// ParentID 建立 parent→children 链；分叉点有多个子节点。
func TestBuildTreeParentChildLinks(t *testing.T) {
	events := []Event{
		{EventID: "a"},
		{EventID: "b", ParentID: "a"},
		{EventID: "c", ParentID: "a"},
		{EventID: "d", ParentID: "b"},
	}
	roots := BuildTree(events)
	if len(roots) != 1 || roots[0].Event.EventID != "a" {
		t.Fatalf("expected single root a, got %d roots", len(roots))
	}
	if len(roots[0].Children) != 2 {
		t.Fatalf("expected a to have 2 children (fan-out), got %d", len(roots[0].Children))
	}
	b := FindNode(roots, "b")
	if b == nil || len(b.Children) != 1 || b.Children[0].Event.EventID != "d" {
		t.Fatalf("expected b -> d chain, got b=%v", b)
	}
}

// Append 自动补 EventID，使旧调用方无需关心 tree 节点标识。
func TestAppendAssignsEventID(t *testing.T) {
	store := NewJSONLStore(t.TempDir())
	if err := store.Append(Event{SessionID: "s1", Type: "user"}); err != nil {
		t.Fatal(err)
	}
	events, err := store.Load("s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventID == "" {
		t.Fatalf("expected Append to assign EventID, got %+v", events)
	}
}

// Fork 复制 session 为独立分支，保留 EventID/ParentID 树结构。
func TestStoreForkDuplicatesBranch(t *testing.T) {
	store := NewJSONLStore(t.TempDir())
	if err := store.Append(Event{SessionID: "src", Type: "user", Message: &Message{Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Event{SessionID: "src", Type: "assistant"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Fork("src", "fork"); err != nil {
		t.Fatal(err)
	}
	src, _ := store.Load("src")
	fork, _ := store.Load("fork")
	if len(src) != len(fork) {
		t.Fatalf("fork should duplicate events: src=%d fork=%d", len(src), len(fork))
	}
	for i := range src {
		if src[i].EventID != fork[i].EventID || fork[i].SessionID != "fork" {
			t.Fatalf("fork should preserve EventID and rebind SessionID at %d: src=%+v fork=%+v", i, src[i], fork[i])
		}
	}
}

// BranchSummary 为分支事件生成规则摘要，保留类型与内容首段。
func TestBranchSummary(t *testing.T) {
	branch := []Event{
		{Message: &Message{Role: RoleUser, Content: "try approach A"}},
		{ToolCall: &ToolCall{Name: "edit_file", Output: "edited foo.go"}},
		{Message: &Message{Role: RoleAssistant, Content: strings.Repeat("x", 200)}},
	}
	summary := BranchSummary(branch)
	if !strings.Contains(summary, "Abandoned branch") {
		t.Fatalf("expected header, got %q", summary)
	}
	if !strings.Contains(summary, "try approach A") {
		t.Fatalf("expected user content, got %q", summary)
	}
	if !strings.Contains(summary, "tool:edit") {
		t.Fatalf("expected tool summary, got %q", summary)
	}
	// 长内容截断
	if !strings.Contains(summary, "...") {
		t.Fatalf("expected truncation, got %q", summary)
	}
}

// MessagesAlongPath 沿 PathTo 提取消息，按序重建对话上下文。
func TestMessagesAlongPath(t *testing.T) {
	events := []Event{
		{EventID: "a", Message: &Message{Role: RoleUser, Content: "q1"}},
		{EventID: "b", Message: &Message{Role: RoleAssistant, Content: "a1"}, ParentID: "a"},
		{EventID: "c", Message: &Message{Role: RoleUser, Content: "q2"}, ParentID: "b"},
	}
	msgs := MessagesAlongPath(events, "c")
	if len(msgs) != 3 || msgs[0].Content != "q1" || msgs[2].Content != "q2" {
		contents := []string{}
		for _, m := range msgs {
			contents = append(contents, m.Content)
		}
		t.Fatalf("expected [q1 a1 q2], got %v", contents)
	}
	if MessagesAlongPath(events, "missing") != nil {
		t.Fatal("missing event should return nil")
	}
}

// PathTo 返回根到目标的祖先链（navigate 回溯基础）。
func TestPathToAncestorChain(t *testing.T) {
	events := []Event{
		{EventID: "a"},
		{EventID: "b", ParentID: "a"},
		{EventID: "c", ParentID: "b"},
	}
	roots := BuildTree(events)
	path := PathTo(roots, "c")
	if len(path) != 3 || path[0].Event.EventID != "a" || path[1].Event.EventID != "b" || path[2].Event.EventID != "c" {
		ids := []string{}
		for _, n := range path {
			ids = append(ids, n.Event.EventID)
		}
		t.Fatalf("expected path [a b c], got %v", ids)
	}
	if PathTo(roots, "missing") != nil {
		t.Fatal("PathTo for missing id should return nil")
	}
}
