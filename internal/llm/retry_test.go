package llm_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
)

func quickRetryCfg() llm.RetryConfig {
	return llm.RetryConfig{
		Enabled:                   true,
		MaxAttempts:               4,
		BaseBackoffMs:             1,
		MaxBackoffMs:              4,
		OverloadFallbackThreshold: 2,
	}
}

// drain 跑完一次 Stream：消费全部 chunk 后读最终错误。
func drain(ctx context.Context, c llm.Client, msgs []llm.Message, tools []llm.ToolSpec) ([]llm.Chunk, error) {
	ch, errs := c.Stream(ctx, msgs, tools)
	var chunks []llm.Chunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	return chunks, <-errs
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		name                      string
		err                       error
		retry, rate, over, prompt bool
	}{
		{"429", &llm.APIError{StatusCode: 429}, true, true, false, false},
		{"500", &llm.APIError{StatusCode: 500}, true, false, true, false},
		{"529", &llm.APIError{StatusCode: 529}, true, false, true, false},
		{"400 generic", &llm.APIError{StatusCode: 400, Body: "bad request"}, false, false, false, false},
		{"400 prompt too long", &llm.APIError{StatusCode: 400, Body: "prompt is too long: 200000 > 200000"}, false, false, false, true},
		{"400 context length", &llm.APIError{StatusCode: 400, Body: "This model's maximum context length is 200000"}, false, false, false, true},
		{"404", &llm.APIError{StatusCode: 404}, false, false, false, false},
		{"nil", nil, false, false, false, false},
	}
	for _, tc := range cases {
		if got := llm.IsRetryable(context.Background(), tc.err); got != tc.retry {
			t.Errorf("%s: llm.IsRetryable=%v want %v", tc.name, got, tc.retry)
		}
		if got := llm.IsRateLimited(tc.err); got != tc.rate {
			t.Errorf("%s: llm.IsRateLimited=%v want %v", tc.name, got, tc.rate)
		}
		if got := llm.IsOverloaded(tc.err); got != tc.over {
			t.Errorf("%s: llm.IsOverloaded=%v want %v", tc.name, got, tc.over)
		}
		if got := llm.IsPromptTooLong(tc.err); got != tc.prompt {
			t.Errorf("%s: llm.IsPromptTooLong=%v want %v", tc.name, got, tc.prompt)
		}
	}
	// context 取消不可重试
	if llm.IsRetryable(context.Background(), context.Canceled) {
		t.Error("context.Canceled should not be retryable")
	}
}

func TestRetryClient_SuccessAfterRateLimit(t *testing.T) {
	fake := llmfake.NewWithErrors(
		[][]llm.Chunk{nil, nil, {{Content: "ok"}}},
		[]error{&llm.APIError{StatusCode: 429}, &llm.APIError{StatusCode: 429}, nil},
	)
	rc := llm.NewRetryClient(fake, quickRetryCfg(), nil)
	chunks, err := drain(context.Background(), rc, nil, nil)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if len(chunks) != 1 || chunks[0].Content != "ok" {
		t.Fatalf("expected single ok chunk, got %+v", chunks)
	}
	if fake.Calls() != 3 {
		t.Fatalf("expected 3 attempts (2 retries), got %d", fake.Calls())
	}
}

func TestRetryClient_ExhaustedRetries(t *testing.T) {
	fake := llmfake.NewWithErrors(
		[][]llm.Chunk{nil, nil, nil, nil},
		[]error{&llm.APIError{StatusCode: 429}, &llm.APIError{StatusCode: 429}, &llm.APIError{StatusCode: 429}, &llm.APIError{StatusCode: 429}},
	)
	rc := llm.NewRetryClient(fake, quickRetryCfg(), nil) // MaxAttempts=4
	_, err := drain(context.Background(), rc, nil, nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 {
		t.Fatalf("expected wrapped 429 llm.APIError, got %v", err)
	}
	if fake.Calls() != 4 {
		t.Fatalf("expected 4 attempts, got %d", fake.Calls())
	}
}

func TestRetryClient_FallbackOnOverload(t *testing.T) {
	primary := llmfake.NewWithErrors(
		[][]llm.Chunk{nil},
		[]error{&llm.APIError{StatusCode: 529}},
	)
	fb := llmfake.NewWithErrors(
		[][]llm.Chunk{{{Content: "fb"}}},
		nil,
	)
	cfg := llm.RetryConfig{
		Enabled: true, MaxAttempts: 3, BaseBackoffMs: 1, MaxBackoffMs: 4,
		OverloadFallbackThreshold: 1, FallbackModels: []string{"fb-model"},
	}
	rc := llm.NewRetryClient(primary, cfg, func(string) llm.Client { return fb })
	chunks, err := drain(context.Background(), rc, nil, nil)
	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if len(chunks) != 1 || chunks[0].Content != "fb" {
		t.Fatalf("expected fb chunk, got %+v", chunks)
	}
	if primary.Calls() != 1 || fb.Calls() != 1 {
		t.Fatalf("expected primary=1 fb=1, got primary=%d fb=%d", primary.Calls(), fb.Calls())
	}
}

func TestRetryClient_AllFallbackFail(t *testing.T) {
	primary := llmfake.NewWithErrors(
		[][]llm.Chunk{nil},
		[]error{&llm.APIError{StatusCode: 529}},
	)
	fb := llmfake.NewWithErrors(
		[][]llm.Chunk{nil},
		[]error{&llm.APIError{StatusCode: 529}},
	)
	cfg := llm.RetryConfig{
		Enabled: true, MaxAttempts: 3, BaseBackoffMs: 1, MaxBackoffMs: 4,
		OverloadFallbackThreshold: 1, FallbackModels: []string{"fb"},
	}
	rc := llm.NewRetryClient(primary, cfg, func(string) llm.Client { return fb })
	_, err := drain(context.Background(), rc, nil, nil)
	if err == nil {
		t.Fatal("expected error when all fallbacks fail")
	}
}

func TestRetryClient_NoRetryAfterForwarded(t *testing.T) {
	// 先发一个 chunk，再出错（流中途）——已转发，绝不重试。
	fake := llmfake.NewWithErrors(
		[][]llm.Chunk{{{Content: "partial"}}},
		[]error{&llm.APIError{StatusCode: 529}},
	)
	rc := llm.NewRetryClient(fake, quickRetryCfg(), nil)
	chunks, err := drain(context.Background(), rc, nil, nil)
	if err == nil {
		t.Fatal("expected mid-stream error to propagate")
	}
	if len(chunks) != 1 || chunks[0].Content != "partial" {
		t.Fatalf("expected partial chunk forwarded exactly once, got %+v", chunks)
	}
	if fake.Calls() != 1 {
		t.Fatalf("must NOT retry after forwarding a chunk, got %d calls", fake.Calls())
	}
}

func TestRetryClient_NonRetryable4xx(t *testing.T) {
	fake := llmfake.NewWithErrors(
		[][]llm.Chunk{nil},
		[]error{&llm.APIError{StatusCode: 400, Body: "invalid model"}},
	)
	rc := llm.NewRetryClient(fake, quickRetryCfg(), nil)
	_, err := drain(context.Background(), rc, nil, nil)
	if err == nil {
		t.Fatal("expected 400 error")
	}
	if fake.Calls() != 1 {
		t.Fatalf("non-retryable 4xx must not retry, got %d calls", fake.Calls())
	}
}

func TestRetryClient_DisabledPassthrough(t *testing.T) {
	fake := llmfake.NewWithErrors(
		[][]llm.Chunk{nil},
		[]error{&llm.APIError{StatusCode: 429}},
	)
	rc := llm.NewRetryClient(fake, llm.RetryConfig{Enabled: false}, nil)
	_, err := drain(context.Background(), rc, nil, nil)
	if err == nil {
		t.Fatal("expected 429 passthrough when disabled")
	}
	if fake.Calls() != 1 {
		t.Fatalf("disabled retry must not retry, got %d calls", fake.Calls())
	}
}

func TestRetryClient_ContextCancelStopsBackoff(t *testing.T) {
	responses := make([][]llm.Chunk, 10)
	errs := make([]error, 10)
	for i := range errs {
		errs[i] = &llm.APIError{StatusCode: 429}
	}
	fake := llmfake.NewWithErrors(responses, errs)
	cfg := llm.RetryConfig{
		Enabled: true, MaxAttempts: 10,
		BaseBackoffMs: 2000, MaxBackoffMs: 8000,
	}
	rc := llm.NewRetryClient(fake, cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := drain(ctx, rc, nil, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected cancel error")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("cancel should stop backoff promptly, took %v", elapsed)
	}
}

func TestIsRetryableUsesCallerContextForTransportTimeouts(t *testing.T) {
	idleErr := &llm.StreamIdleTimeoutError{Duration: 10 * time.Millisecond}
	headerErr := &url.Error{Op: "Post", URL: "https://example.test", Err: context.DeadlineExceeded}
	if !llm.IsRetryable(context.Background(), idleErr) {
		t.Fatal("typed stream idle timeout should be retryable while caller is active")
	}
	if !llm.IsRetryable(context.Background(), headerErr) {
		t.Fatal("response-header transport timeout should be retryable while caller is active")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if llm.IsRetryable(canceled, idleErr) {
		t.Fatal("idle timeout must not retry after caller cancellation")
	}
	if llm.IsRetryable(canceled, headerErr) {
		t.Fatal("transport timeout must not retry after caller cancellation")
	}

	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if llm.IsRetryable(deadline, idleErr) {
		t.Fatal("idle timeout must not retry after caller deadline")
	}
}

func TestRetryClientRetriesIdleTimeoutBeforeOutput(t *testing.T) {
	fake := llmfake.NewWithErrors(
		[][]llm.Chunk{nil, {{Content: "ok"}}},
		[]error{&llm.StreamIdleTimeoutError{Duration: time.Second}, nil},
	)
	rc := llm.NewRetryClient(fake, quickRetryCfg(), nil)
	chunks, err := drain(context.Background(), rc, nil, nil)
	if err != nil {
		t.Fatalf("retry after idle timeout failed: %v", err)
	}
	if fake.Calls() != 2 {
		t.Fatalf("idle timeout attempts = %d, want 2", fake.Calls())
	}
	if len(chunks) != 1 || chunks[0].Content != "ok" {
		t.Fatalf("chunks = %+v, want one ok chunk", chunks)
	}
}

func TestRetryClientDoesNotRetryIdleTimeoutAfterOutput(t *testing.T) {
	idleErr := &llm.StreamIdleTimeoutError{Duration: time.Second}
	fake := llmfake.NewWithErrors(
		[][]llm.Chunk{{{Content: "partial"}}},
		[]error{idleErr},
	)
	rc := llm.NewRetryClient(fake, quickRetryCfg(), nil)
	chunks, err := drain(context.Background(), rc, nil, nil)
	if !errors.Is(err, idleErr) {
		t.Fatalf("error = %v, want original idle error", err)
	}
	if fake.Calls() != 1 {
		t.Fatalf("partial idle timeout attempts = %d, want 1", fake.Calls())
	}
	if len(chunks) != 1 || chunks[0].Content != "partial" {
		t.Fatalf("chunks = %+v, want partial exactly once", chunks)
	}
}

func TestRetryClientRetriesWrappedResponseHeaderTimeout(t *testing.T) {
	headerErr := &url.Error{Op: "Post", URL: "https://example.test", Err: context.DeadlineExceeded}
	fake := llmfake.NewWithErrors(
		[][]llm.Chunk{nil, {{Content: "ok"}}},
		[]error{headerErr, nil},
	)
	rc := llm.NewRetryClient(fake, quickRetryCfg(), nil)
	chunks, err := drain(context.Background(), rc, nil, nil)
	if err != nil {
		t.Fatalf("retry after response-header timeout failed: %v", err)
	}
	if fake.Calls() != 2 {
		t.Fatalf("response-header timeout attempts = %d, want 2", fake.Calls())
	}
	if len(chunks) != 1 || chunks[0].Content != "ok" {
		t.Fatalf("chunks = %+v, want one ok chunk", chunks)
	}
}
