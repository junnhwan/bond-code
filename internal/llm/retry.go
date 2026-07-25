package llm

import (
	"context"
	"fmt"
	"math/rand"
	"runtime/debug"
	"time"

	"github.com/junnhwan/bond-code/internal/observe"
)

// RetryConfig 控制传输层重试行为。零值 Enabled=false 表示不重试（向后兼容 fail-fast）。
type RetryConfig struct {
	Enabled                   bool
	MaxAttempts               int // 含首次；<=1 表示只试一次
	BaseBackoffMs             int
	MaxBackoffMs              int
	OverloadFallbackThreshold int // 连续 5xx 次数触发 fallback
	FallbackModels            []string
}

// FallbackFactory 把 model 名构造成 Client，由装配层提供（避免 llm 包依赖具体实现）。
type FallbackFactory func(model string) Client

// RetryClient 包裹一个 Client，对可重试错误（429/5xx/网络抖动）做指数退避重试，
// 在连续过载时切换 fallback model。实现 Client 接口，对调用方完全透明。
//
// 流式不变量：一旦向下游转发过任何 chunk，后续错误一律透传不重试，避免重发导致
// 重复 chunk（见 tryStream 的 forwarded 标记）。
type RetryClient struct {
	inner    Client
	fallback []Client
	cfg      RetryConfig
}

// NewRetryClient 用 inner 作主 client；当 cfg.FallbackModels 非空且 factory 非 nil
// 时构造 fallback client 序列。
func NewRetryClient(inner Client, cfg RetryConfig, factory FallbackFactory) *RetryClient {
	rc := &RetryClient{inner: inner, cfg: normalizeRetryConfig(cfg)}
	for _, m := range cfg.FallbackModels {
		if m == "" || factory == nil {
			continue
		}
		if c := factory(m); c != nil {
			rc.fallback = append(rc.fallback, c)
		}
	}
	return rc
}

func normalizeRetryConfig(cfg RetryConfig) RetryConfig {
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}
	if cfg.BaseBackoffMs <= 0 {
		cfg.BaseBackoffMs = 1000
	}
	if cfg.MaxBackoffMs <= 0 {
		cfg.MaxBackoffMs = 8000
	}
	if cfg.OverloadFallbackThreshold <= 0 {
		cfg.OverloadFallbackThreshold = 2
	}
	return cfg
}

func (c *RetryClient) Stream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan Chunk, <-chan error) {
	// 未启用或只试一次：直接透传 inner，零开销。
	if !c.cfg.Enabled || c.cfg.MaxAttempts <= 1 {
		return c.inner.Stream(ctx, messages, tools)
	}
	out := make(chan Chunk)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		defer func() {
			if r := recover(); r != nil {
				observe.LogPanic("llm-retry", r, debug.Stack())
				select {
				case errs <- fmt.Errorf("stream panic: %v", r):
				default:
				}
			}
		}()
		errs <- c.streamWithRetry(ctx, messages, tools, out)
	}()
	return out, errs
}

// streamWithRetry 跑重试循环，把成功 chunk 透传到 out，返回最终错误。
func (c *RetryClient) streamWithRetry(ctx context.Context, messages []Message, tools []ToolSpec, out chan<- Chunk) error {
	overloadStreak := 0
	var lastErr error

	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		forwarded, err := c.tryStream(ctx, c.inner, messages, tools, out, &overloadStreak)
		lastErr = err
		if err == nil {
			return nil
		}
		if forwarded {
			// 已转发过 chunk：绝不重试（防重复）。
			return err
		}
		if !IsRetryable(ctx, err) {
			// 不可重试（4xx 非 429 等）：立即透传。
			return err
		}
		// 连续过载达阈值且备了 fallback：切 fallback（不再消耗主重试预算）。
		if IsOverloaded(err) && overloadStreak >= c.cfg.OverloadFallbackThreshold && len(c.fallback) > 0 {
			return c.tryFallbacks(ctx, messages, tools, out)
		}
		// 还能重试：退避后下一轮。
		if attempt < c.cfg.MaxAttempts {
			if werr := c.wait(ctx, attempt); werr != nil {
				return werr
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("model API failed after %d attempts", c.cfg.MaxAttempts)
	}
	return fmt.Errorf("model API failed after %d attempts: %w", c.cfg.MaxAttempts, lastErr)
}

// tryStream 调用 client.Stream 把 chunk 转发到 out，返回 (forwarded, err)。
// overloadStreak 在遇到 5xx 且未转发时递增（供主循环判断 fallback 触发）。
func (c *RetryClient) tryStream(ctx context.Context, client Client, messages []Message, tools []ToolSpec, out chan<- Chunk, overloadStreak *int) (bool, error) {
	inChunks, inErrs := client.Stream(ctx, messages, tools)
	forwarded := false
	for {
		select {
		case <-ctx.Done():
			return forwarded, ctx.Err()
		case chunk, ok := <-inChunks:
			if !ok {
				continue // inner 关闭，等 errs 收尾。
			}
			forwarded = true
			select {
			case <-ctx.Done():
				return forwarded, ctx.Err()
			case out <- chunk:
			}
		case err := <-inErrs:
			if err == nil {
				return forwarded, nil
			}
			if !forwarded && IsOverloaded(err) {
				*overloadStreak++
			}
			return forwarded, err
		}
	}
}

// tryFallbacks 依次尝试 fallback client，成功即返回，全部失败返回最后错误。
func (c *RetryClient) tryFallbacks(ctx context.Context, messages []Message, tools []ToolSpec, out chan<- Chunk) error {
	streak := 0
	var lastErr error
	for _, fb := range c.fallback {
		if err := ctx.Err(); err != nil {
			return err
		}
		forwarded, err := c.tryStream(ctx, fb, messages, tools, out, &streak)
		if err == nil {
			return nil
		}
		lastErr = err
		if forwarded || !IsRetryable(ctx, err) {
			return err
		}
		if werr := c.wait(ctx, 1); werr != nil {
			return werr
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all fallback models failed")
	}
	return lastErr
}

// wait 做全额抖动的指数退避：区间 [0, base×2^(attempt-1))，上限 MaxBackoffMs。
// 全额抖动（full jitter）避免重试同步聚集。
func (c *RetryClient) wait(ctx context.Context, attempt int) error {
	base := time.Duration(c.cfg.BaseBackoffMs) * time.Millisecond
	max := time.Duration(c.cfg.MaxBackoffMs) * time.Millisecond
	d := base * (1 << (attempt - 1))
	if d <= 0 || d > max {
		d = max
	}
	jitter := time.Duration(0)
	if d > 0 {
		jitter = time.Duration(rand.Int63n(int64(d) + 1))
	}
	if jitter <= 0 {
		jitter = base / 2
	}
	t := time.NewTimer(jitter)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
