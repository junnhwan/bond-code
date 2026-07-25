package session

import (
	"fmt"
	"strings"
)

// TreeNode 是 session-tree 的一个节点（事件 + 子节点）。
type TreeNode struct {
	Event    Event
	Children []*TreeNode
}

// BuildTree 从扁平事件列表重建父子树。ParentID 指向已存在事件的 EventID；
// ParentID 为空或指向不存在事件的，作为根。线性会话（无 ParentID）退化为多个独立根，
// 即兼容旧线性 JSONL。
func BuildTree(events []Event) []*TreeNode {
	byID := make(map[string]*TreeNode, len(events))
	order := []string{}
	for _, e := range events {
		if e.EventID == "" {
			continue
		}
		byID[e.EventID] = &TreeNode{Event: e}
		order = append(order, e.EventID)
	}
	var roots []*TreeNode
	for _, id := range order {
		node := byID[id]
		if parent, ok := byID[node.Event.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node)
		}
	}
	return roots
}

// FindNode 在树森林中按 EventID 查找节点。
func FindNode(roots []*TreeNode, eventID string) *TreeNode {
	for _, r := range roots {
		if n := findNode(r, eventID); n != nil {
			return n
		}
	}
	return nil
}

func findNode(node *TreeNode, eventID string) *TreeNode {
	if node.Event.EventID == eventID {
		return node
	}
	for _, c := range node.Children {
		if n := findNode(c, eventID); n != nil {
			return n
		}
	}
	return nil
}

// PathTo 返回从某根到 eventID 的祖先链（根在前，目标在尾）。用于 navigate 回溯到
// 历史节点：沿这条路径重建上下文，即可从分叉点换思路继续。
func PathTo(roots []*TreeNode, eventID string) []*TreeNode {
	for _, r := range roots {
		if path := pathTo(r, eventID); path != nil {
			return path
		}
	}
	return nil
}

func pathTo(node *TreeNode, eventID string) []*TreeNode {
	if node.Event.EventID == eventID {
		return []*TreeNode{node}
	}
	for _, c := range node.Children {
		if sub := pathTo(c, eventID); sub != nil {
			return append([]*TreeNode{node}, sub...)
		}
	}
	return nil
}

// BranchSummary 为被放弃的分支事件生成规则摘要（navigate 回溯时压缩放弃的路径）。
// 非语义摘要（LLM 摘要可后续注入）；保留每事件类型与内容首段，让模型回溯后仍知道放弃
// 路径做了什么。
func BranchSummary(branch []Event) string {
	var b strings.Builder
	b.WriteString("Abandoned branch:\n")
	for _, e := range branch {
		switch {
		case e.Message != nil && strings.TrimSpace(e.Message.Content) != "":
			c := e.Message.Content
			if len(c) > 120 {
				c = c[:120] + "..."
			}
			fmt.Fprintf(&b, "- [%s] %s\n", e.Message.Role, c)
		case e.ToolCall != nil:
			fmt.Fprintf(&b, "- [tool:%s] %s\n", e.ToolCall.Name, truncStr(e.ToolCall.Output, 80))
		case e.AgentEvent != nil && e.AgentEvent.Message != "":
			fmt.Fprintf(&b, "- [%s] %s\n", e.AgentEvent.Type, truncStr(e.AgentEvent.Message, 80))
		}
	}
	return b.String()
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// MessagesAlongPath 返回从根到 eventID 路径上所有事件的 Message（按序）。navigate
// 回溯到历史节点后，用这条消息序列重建对话上下文（app 层转 llm.Message 后续）。
func MessagesAlongPath(events []Event, eventID string) []Message {
	path := PathTo(BuildTree(events), eventID)
	if path == nil {
		return nil
	}
	var msgs []Message
	for _, node := range path {
		if node.Event.Message != nil {
			msgs = append(msgs, *node.Event.Message)
		}
	}
	return msgs
}
