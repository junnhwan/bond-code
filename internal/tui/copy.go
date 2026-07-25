package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var copyToClipboard = systemCopyToClipboard

func (m Model) runCopyCommand(args []string) (Model, bool) {
	content, ok := m.latestCopyableOutput()
	if !ok {
		body := "no output to copy"
		m.timeline = m.timeline.AppendBlock(BlockError, "/copy", body)
		m = m.pushToast(body, toastWarn)
		return m.markNewOutputBelow(), true
	}
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		target := m.copyTargetPath(args[0])
		if err := writeCopyFile(target, content); err != nil {
			body := err.Error()
			m.timeline = m.timeline.AppendBlock(BlockError, "/copy", body)
			m = m.pushToast(body, toastError)
			return m.markNewOutputBelow(), true
		}
		body := "copied latest output to " + target
		m.timeline = m.timeline.AppendBlock(BlockCommand, "/copy", body)
		m = m.pushToast("copied to "+target, toastSuccess)
		return m.markNewOutputBelow(), true
	}
	if err := copyToClipboard(content); err == nil {
		body := "copied latest output to clipboard"
		m.timeline = m.timeline.AppendBlock(BlockCommand, "/copy", body)
		m = m.pushToast("copied to clipboard", toastSuccess)
		return m.markNewOutputBelow(), true
	}
	target := m.copyTargetPath("bondcode-copy.txt")
	if err := writeCopyFile(target, content); err != nil {
		body := err.Error()
		m.timeline = m.timeline.AppendBlock(BlockError, "/copy", body)
		m = m.pushToast(body, toastError)
		return m.markNewOutputBelow(), true
	}
	body := "clipboard unavailable; copied latest output to " + target
	m.timeline = m.timeline.AppendBlock(BlockCommand, "/copy", body)
	m = m.pushToast("clipboard unavailable; saved to file", toastWarn)
	return m.markNewOutputBelow(), true
}

func (m Model) latestCopyableOutput() (string, bool) {
	for turnIdx := len(m.timeline.Turns) - 1; turnIdx >= 0; turnIdx-- {
		blocks := m.timeline.Turns[turnIdx].Blocks
		for blockIdx := len(blocks) - 1; blockIdx >= 0; blockIdx-- {
			block := blocks[blockIdx]
			var body string
			switch block.Kind {
			case BlockAssistant, BlockCommand:
				body = block.Body
			case BlockTool, BlockConfirmation:
				if block.Tool != nil {
					body = firstNonEmpty(block.Tool.Output, block.Tool.Error, block.Body)
				} else {
					body = block.Body
				}
			default:
				continue
			}
			if strings.TrimSpace(body) != "" {
				return body, true
			}
		}
	}
	return "", false
}

func (m Model) copyTargetPath(arg string) string {
	target := strings.TrimSpace(arg)
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	root := strings.TrimSpace(m.cfg.Status.ProjectRoot)
	if root == "" {
		root = "."
	}
	return filepath.Clean(filepath.Join(root, target))
}

func writeCopyFile(path string, content string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("copy target path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("copy target directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("copy output: %w", err)
	}
	return nil
}

func systemCopyToClipboard(content string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("clip")
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		}
	}
	if cmd == nil {
		return errors.New("clipboard command not found")
	}
	cmd.Stdin = strings.NewReader(content)
	return cmd.Run()
}
