package safety

import (
	"encoding/json"
	"regexp"
	"strings"
)

type denyRule struct {
	reason  string
	pattern *regexp.Regexp
}

var dangerousCommandRules = []denyRule{
	{reason: "blocked dangerous command: sudo", pattern: regexp.MustCompile(`(?i)\bsudo\b`)},
	{reason: "blocked dangerous command: rm -rf root or home", pattern: regexp.MustCompile(`(?i)\brm\s+-[a-z]*r[a-z]*f[a-z]*\s+(/|~|\$home)|\brm\s+-[a-z]*f[a-z]*r[a-z]*\s+(/|~|\$home)`)},
	{reason: "blocked dangerous command: mkfs", pattern: regexp.MustCompile(`(?i)\bmkfs(\.|\b)`)},
	{reason: "blocked dangerous command: dd to device", pattern: regexp.MustCompile(`(?i)\bdd\b[^\n]*\bof=/dev/`)},
	{reason: "blocked dangerous command: fork bomb", pattern: regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`)},
	{reason: "blocked dangerous command: remote script pipe", pattern: regexp.MustCompile(`(?i)\b(curl|wget)\b[^|\n]*\|\s*(sh|bash|zsh|fish|ksh)\b`)},
	{reason: "blocked dangerous command: whole filesystem scan", pattern: regexp.MustCompile(`(?i)\bfind\s+(/|~|\$home)`)},
	{reason: "blocked dangerous command: chmod 777 root or home", pattern: regexp.MustCompile(`(?i)\bchmod\s+-R\s+777\s+(/|~)`)},
	{reason: "blocked dangerous command: shutdown", pattern: regexp.MustCompile(`(?i)\b(shutdown|reboot|halt|poweroff)\b`)},
}

func DangerousCommandReason(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	normalized := strings.Join(strings.Fields(command), " ")
	for _, rule := range dangerousCommandRules {
		if rule.pattern.MatchString(normalized) {
			return rule.reason
		}
	}
	return ""
}

func dangerousCommandFromRawInput(toolName string, rawInput string) string {
	if toolName != "run_command" && toolName != "execute_command" {
		return ""
	}
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(rawInput), &input); err != nil {
		return ""
	}
	return DangerousCommandReason(input.Command)
}
