package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func (c *AnthropicCompatibleClient) streamAnthropicWire(ctx context.Context, messages []Message, tools []ToolSpec, chunks chan<- Chunk) error {
	return c.streamAnthropicWireWithParser(ctx, messages, tools, chunks, newAnthropicSSEParser())
}

func (c *AnthropicCompatibleClient) streamAnthropicWireWithParser(ctx context.Context, messages []Message, tools []ToolSpec, chunks chan<- Chunk, parser *anthropicSSEParser) error {
	if c.cfg.BaseURL == "" {
		return fmt.Errorf("model base URL is not configured; set BONDCODE_BASE_URL or configure model.base_url")
	}
	if c.cfg.Model == "" {
		return fmt.Errorf("model name is not configured; set BONDCODE_MODEL or configure model.model")
	}
	if c.cfg.APIKeyEnv == "" {
		c.cfg.APIKeyEnv = "BONDCODE_API_KEY"
	}
	apiKey := os.Getenv(c.cfg.APIKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("model API key is not configured; set %s", c.cfg.APIKeyEnv)
	}
	maxTokens := c.cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	reqBody, err := buildAnthropicRequest(c.cfg.Model, maxTokens, c.cfg.Temperature, c.cfg.ThinkingEnabled, c.cfg.ThinkingBudgetTokens, messages, tools)
	if err != nil {
		return err
	}
	if c.cfg.PromptCache {
		applyCacheBreakpoints(reqBody)
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/messages"
	controller := newStreamIdleController(ctx, c.streamIdleTimeout)
	defer controller.finish()
	req, err := http.NewRequestWithContext(controller.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("x-api-key", apiKey)
	// Also send Authorization: Bearer for Anthropic-compatible gateways
	// (Zhipu bigmodel, Aliyun bailian, ...) that require Bearer and reject the
	// Anthropic-standard x-api-key. Standard Anthropic ignores this header, so
	// dual-sending is safe and preserves official API compatibility.
	req.Header.Set("Authorization", "Bearer "+apiKey)

	setupGeneration := controller.arm()
	resp, err := c.httpClient.Do(req)
	controller.disarm(setupGeneration)
	if err != nil {
		return controller.translate(err)
	}
	resp.Body = newStreamIdleBody(resp.Body, controller)
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return controller.translate(readErr)
		}
		return &APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}
	return controller.translate(parseAnthropicSSEWithParser(controller.Context(), resp.Body, chunks, parser))
}
