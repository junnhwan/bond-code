package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
)

// APIError 携带 HTTP 状态码与响应体，让上层按错误类型决策而非字符串匹配。
// 429 / 5xx / prompt_too_long 都通过 errors.As 拿到 *APIError 后用分类断言区分；
// Err 非 nil 时表示是底层网络错误（连接/超时），此时 StatusCode 为 0。
type APIError struct {
	StatusCode int
	Body       string
	Err        error
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("model API returned HTTP %d: %s", e.StatusCode, e.Body)
}

func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsRetryable 报告错误是否值得退避重试：429、5xx 或网络层临时错误。
// 4xx（非 429）多为请求本身错误，重试无意义；调用者 context 取消/超时不重试。
func IsRetryable(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	var idleErr *StreamIdleTimeoutError
	if errors.As(err, &idleErr) {
		return true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == 429 || apiErr.StatusCode >= 500 {
			return true
		}
		if apiErr.StatusCode != 0 {
			return false
		}
	}
	// net/http wraps response-header and other transport timeouts in
	// *url.Error. Those errors may match context.DeadlineExceeded even though
	// the caller context remains active, so classify them before rejecting
	// caller-owned context errors.
	var urlErr *url.Error
	if errors.As(err, &urlErr) && isNetworkError(urlErr.Err) {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return isNetworkError(err)
}

// IsRateLimited 报告 429 限流。
func IsRateLimited(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 429
	}
	return false
}

// IsOverloaded 报告 5xx（含 529 过载）。
func IsOverloaded(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500
	}
	return false
}

// IsPromptTooLong 报告 400 且响应体含上下文超长关键词。
// prompt_too_long 在 loop 层用 reactive compaction 处理（需改 messages），不在 llm
// 层重试；此函数供 loop 层判断。不同 Anthropic 兼容网关措辞不一，关键词集合保守匹配。
func IsPromptTooLong(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
		low := strings.ToLower(apiErr.Body)
		for _, kw := range promptTooLongKeywords {
			if strings.Contains(low, kw) {
				return true
			}
		}
	}
	return false
}

var promptTooLongKeywords = []string{
	"prompt is too long",
	"context length",
	"context window",
	"too many input tokens",
	"maximum context length",
}

// isNetworkError 报告底层网络临时错误（超时、连接重置/拒绝/断开）。
// http.Client.Do 返回的 *url.Error 会被 errors.As 穿透到底层 net.Error。
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	for _, e := range []error{syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
