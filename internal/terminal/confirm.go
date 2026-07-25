package terminal

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/junnhwan/bond-code/internal/safety"
)

// Confirmer is a plain stdin/stdout confirmer for non-TUI (headless / --once)
// runs. It is the non-interactive counterpart to tui.Confirmer; both implement
// safety.Confirmer. The package is named "terminal" (not "ui") to stay distinct
// from the Bubble Tea "tui" package.
type Confirmer struct {
	In  io.Reader
	Out io.Writer
}

func NewConfirmer(in io.Reader, out io.Writer) Confirmer {
	return Confirmer{In: in, Out: out}
}

func (c Confirmer) Confirm(ctx context.Context, req safety.ConfirmationRequest) (bool, error) {
	if c.In == nil {
		return false, nil
	}
	if c.Out != nil {
		if req.Risk == string(safety.ConfirmHigh) || req.Risk == "high" {
			fmt.Fprintf(c.Out, "Tool: %s\nRisk: %s\n%s\nType \"yes\" to execute: ", req.ToolName, req.Risk, req.Detail)
		} else {
			fmt.Fprintf(c.Out, "Execute %s? [y/N] ", req.Summary)
		}
	}

	answerCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(c.In)
		if scanner.Scan() {
			answerCh <- scanner.Text()
			return
		}
		answerCh <- ""
	}()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case answer := <-answerCh:
		answer = strings.TrimSpace(strings.ToLower(answer))
		if req.Risk == string(safety.ConfirmHigh) || req.Risk == "high" {
			return answer == "yes", nil
		}
		return answer == "y" || answer == "yes", nil
	}
}
