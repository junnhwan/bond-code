package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderMarkdownTerminalCJKProseStaysReadable(t *testing.T) {
	// Regression for the pink-soup screenshot: Chinese prose + mixed fences
	// must not explode into over-wide reverse-colored fragments.
	src := "## 关键点实现\n\n" +
		"- **`todo/todo.go`**: Item 定义和 List。Load 在文件不存在时返回空列表。\n\n" +
		"```text\n=== RUN   TestAdd\n--- PASS: TestAdd\nok  \ttodo\n```\n"
	out := renderMarkdownTerminal(src, 72)
	plain := ansi.Strip(out)
	for _, want := range []string{"关键点实现", "todo/todo.go", "TestAdd", "PASS"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in:\n%s", want, plain)
		}
	}
	if markdownOutputBroken(out, 72) {
		t.Fatalf("output flagged broken:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if ansi.StringWidth(line) > 74 {
			t.Fatalf("line too wide (%d): %q", ansi.StringWidth(line), ansi.Strip(line))
		}
	}
}

func TestLooksLikeCodeRejectsCJKHeavyFence(t *testing.T) {
	prose := "这是一段很长的中文说明，夹杂 Load 和 Save 几个英文词，但不应该被当成源代码高亮。"
	if looksLikeCode(prose) {
		t.Fatal("CJK-heavy prose must not be treated as code for chroma")
	}
	if !looksLikeCode("func main() {\n\tfmt.Println(\"hi\")\n}\n") {
		t.Fatal("normal Go should look like code")
	}
}

func TestMergeTurnReasoningOnePerTurnKeepsTools(t *testing.T) {
	// Product rule: one thinking header per turn; tools stay as their own rows.
	// think → tool → think must NOT spawn a second thinking block, and must
	// NOT drop the tool (user-reported "tools swallowed" + "thinking multiplies").
	s := TimelineState{}.StartUserTurn("hi")
	s = s.MergeTurnReasoning("first thought")
	s = s.MergeTurnReasoning("more of first")
	s = s.UpsertToolBlock(&ToolBlock{ID: "c1", Name: "read_file", Status: ToolDone, Input: `{"path":"a.go"}`})
	s = s.MergeTurnReasoning("second thought after tool")
	s = s.UpsertToolBlock(&ToolBlock{ID: "c2", Name: "edit_file", Status: ToolDone, Input: `{"path":"b.go"}`})
	s = s.MergeTurnReasoning("third thought after second tool")

	blocks := s.Turns[0].Blocks
	var reasoning, tools int
	var reasonBody string
	var toolNames []string
	for _, b := range blocks {
		switch b.Kind {
		case BlockReasoning:
			reasoning++
			reasonBody = b.Body
		case BlockTool:
			tools++
			if b.Tool != nil {
				toolNames = append(toolNames, b.Tool.Name)
			}
		}
	}
	if reasoning != 1 {
		t.Fatalf("want exactly 1 reasoning block per turn, got %d in %#v", reasoning, blocks)
	}
	if tools != 2 {
		t.Fatalf("want both tools visible, got %d names=%v blocks=%#v", tools, toolNames, blocks)
	}
	for _, want := range []string{"first thought", "more of first", "second thought", "third thought"} {
		if !strings.Contains(reasonBody, want) {
			t.Fatalf("merged reasoning missing %q in %q", want, reasonBody)
		}
	}
	// Tools remain after the single thinking block in commit order.
	if len(blocks) < 3 || blocks[0].Kind != BlockReasoning || blocks[1].Kind != BlockTool || blocks[2].Kind != BlockTool {
		t.Fatalf("want thinking → tool → tool, got %#v", blocks)
	}
	if blocks[1].Tool.Name != "read_file" || blocks[2].Tool.Name != "edit_file" {
		t.Fatalf("tool order wrong: %#v", blocks)
	}
}
