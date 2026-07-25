package rpc

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestServeRoundTrip(t *testing.T) {
	in := strings.NewReader(`{"type":"ping"}` + "\n")
	var out bytes.Buffer
	if err := Serve(in, &out, func(cmd Command) Response {
		if cmd.Type != "ping" {
			t.Fatalf("expected ping, got %s", cmd.Type)
		}
		return Response{Type: "result", OK: true, Payload: "pong"}
	}); err != nil {
		t.Fatal(err)
	}
	var resp Response
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Type != "result" || resp.OK != true || resp.Payload != "pong" {
		t.Fatalf("expected pong result, got %+v", resp)
	}
}

// 无效 JSON 返回 error 响应，循环继续。
func TestServeInvalidCommandReturnsError(t *testing.T) {
	in := strings.NewReader("not json\n")
	var out bytes.Buffer
	if err := Serve(in, &out, func(Command) Response {
		t.Fatal("handler should not be called for invalid command")
		return Response{}
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("invalid command")) {
		t.Fatalf("expected error response, got %s", out.String())
	}
}

// 多条命令逐条处理。
func TestServeMultipleCommands(t *testing.T) {
	in := strings.NewReader(`{"type":"a"}` + "\n" + `{"type":"b"}` + "\n")
	var out bytes.Buffer
	count := 0
	if err := Serve(in, &out, func(cmd Command) Response {
		count++
		return Response{Type: "result", OK: true}
	}); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 commands handled, got %d", count)
	}
}

// ServeStream 把 handler 返回的事件序列逐个写出。
func TestServeStreamEmitsEvents(t *testing.T) {
	in := strings.NewReader(`{"type":"run"}` + "\n")
	var out bytes.Buffer
	if err := ServeStream(in, &out, func(cmd Command) <-chan Response {
		ch := make(chan Response, 3)
		ch <- Response{Type: "event", OK: true, Payload: "chunk1"}
		ch <- Response{Type: "event", OK: true, Payload: "chunk2"}
		ch <- Response{Type: "result", OK: true, Payload: "done"}
		close(ch)
		return ch
	}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(strings.TrimSpace(out.String()), "\n") + 1
	if lines != 3 {
		t.Fatalf("expected 3 streamed responses, got %d (out=%q)", lines, out.String())
	}
}

// UI 确认往返：send → ui_request → ui_response → result（多轮命令模式）。
func TestUIRequestResponseRoundTrip(t *testing.T) {
	in := strings.NewReader(`{"type":"send","payload":{"prompt":"rm"}}` + "\n" + `{"type":"ui_response","payload":{"request_id":"r1","approved":false}}` + "\n")
	var out bytes.Buffer
	step := 0
	if err := Serve(in, &out, func(cmd Command) Response {
		step++
		if step == 1 {
			return Response{Type: "ui_request", Payload: UIRequest{RequestID: "r1", Kind: "confirm", Message: "approve?"}}
		}
		var uir UIResponsePayload
		_ = json.Unmarshal(cmd.Payload, &uir)
		if uir.RequestID != "r1" {
			t.Fatalf("expected r1, got %s", uir.RequestID)
		}
		return Response{Type: "result", OK: true, Payload: "rejected"}
	}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(strings.TrimSpace(out.String()), "\n") + 1
	if lines != 2 {
		t.Fatalf("expected 2 responses (ui_request + result), got %d", lines)
	}
}
