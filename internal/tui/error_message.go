package tui

import "strings"

func humanizeAgentError(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	message := ""
	switch {
	case strings.Contains(lower, "401") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "api key") || strings.Contains(lower, "authentication"):
		message = "Authentication failed. Check BONDCODE_API_KEY and provider credentials."
	case strings.Contains(lower, "insufficient") && (strings.Contains(lower, "quota") || strings.Contains(lower, "balance") || strings.Contains(lower, "credit")):
		message = "Provider quota or balance is insufficient. Check billing and quota."
	case strings.Contains(lower, "404") || strings.Contains(lower, "model not found") || strings.Contains(lower, "not found"):
		message = "Model was not found. Check BONDCODE_MODEL and BONDCODE_BASE_URL."
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests"):
		message = "Rate limited by the model provider. Wait a moment or reduce request rate."
	case strings.Contains(lower, "500") || strings.Contains(lower, "502") || strings.Contains(lower, "503") || strings.Contains(lower, "504") || strings.Contains(lower, "529") || strings.Contains(lower, "overloaded"):
		message = "Model provider is temporarily unavailable. Retry later or switch model/provider."
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out"):
		message = "Request timed out. Check network/provider status and retry."
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "connection reset") || strings.Contains(lower, "no such host") || strings.Contains(lower, "dial tcp") || strings.Contains(lower, "tls handshake"):
		message = "Network connection failed. Check BONDCODE_BASE_URL and connectivity."
	}
	if message == "" {
		return raw
	}
	return message + "\n\nOriginal: " + raw
}
