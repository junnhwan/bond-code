// Package rpc 提供 headless JSON-line 协议，让 BondCode 可被 IDE / 外部程序嵌入驱动
// （区别于纯 TUI 启动）。客户端从 stdin 发命令 JSON，服务端从 stdout 写响应 JSON。
//
// 这是协议骨架层。Command/Response 类型 + Serve 循环；cmd 层接入（实际驱动 agent）
// 与 UI 确认往返后续。
package rpc

import (
	"bufio"
	"encoding/json"
	"io"
)

// Command 是客户端发来的请求。
type Command struct {
	Type    string          `json:"type"` // "run" / "send" / "stop" / ...
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Response 是服务端的回执。
type Response struct {
	Type    string `json:"type"` // "result" / "error" / "ui_request"
	OK      bool   `json:"ok"`
	Payload any    `json:"payload,omitempty"`
	Error   string `json:"error,omitempty"`
}

// UIRequest 是 server 发给 client 的 UI 确认/选择请求（headless 模式下 agent 需用户确认
// 高危操作时）。server 作为 Type="ui_request" 的 Response 写出（Payload 是 UIRequest）；
// client 用 Type="ui_response" 的 Command 回复，payload 是 UIResponsePayload（带 request_id 关联）。
type UIRequest struct {
	RequestID string `json:"request_id"`
	Kind      string `json:"kind"` // "confirm" / "select" / "input"
	Message   string `json:"message,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// UIResponsePayload 是 client 回复 ui_response 命令的 payload。
type UIResponsePayload struct {
	RequestID string `json:"request_id"`
	Approved  bool   `json:"approved"`        // confirm: 是否批准
	Value     string `json:"value,omitempty"` // select/input: 选择的值
}

// Handler 处理一条 Command，返回 Response。
type Handler func(cmd Command) Response

// Serve 在 in/out 上跑 JSON-line 协议循环：逐行读 Command → Handler → 写 Response。
// 用于 headless 嵌入；TUI 模式不启用。无效 JSON 返回 error 响应而非中断循环。
func Serve(in io.Reader, out io.Writer, handler Handler) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var cmd Command
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			_ = enc.Encode(Response{Type: "error", Error: "invalid command: " + err.Error()})
			continue
		}
		if err := enc.Encode(handler(cmd)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// StreamHandler 返回流式响应（多个 Response，如 agent 的逐事件推送）。
type StreamHandler func(cmd Command) <-chan Response

// ServeStream 像 Serve，但 handler 返回事件序列，逐个写出。用于流式 agent 事件，
// 让 IDE/外部程序能实时看到 agent 的 tool 调用与 token 流。
func ServeStream(in io.Reader, out io.Writer, handler StreamHandler) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var cmd Command
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			_ = enc.Encode(Response{Type: "error", Error: "invalid command: " + err.Error()})
			continue
		}
		for resp := range handler(cmd) {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
