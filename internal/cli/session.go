package cli

import (
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/session"

	"github.com/spf13/cobra"
)

func newSessionCommand() *cobra.Command {
	var dir, configPath string
	// Resolve through the same path logic Bootstrap uses (config -> home-dir
	// default) so `session list/show/...` reads where the agent actually writes.
	effectiveDir := func() string { return app.ResolveSessionDir(configPath, dir) }
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage persisted sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "session directory (defaults to ~/.bondcode/projects/<encoded-cwd>)")
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config YAML")
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := session.NewJSONLStore(effectiveDir()).List()
			if err != nil {
				return err
			}
			for _, id := range ids {
				fmt.Fprintln(cmd.OutOrStdout(), id)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <session-id>",
		Short: "Show a session trace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			events, err := session.NewJSONLStore(effectiveDir()).Load(args[0])
			if err != nil {
				return err
			}
			for _, event := range events {
				if event.Message != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", event.Message.Role, event.Message.Content)
				}
				if event.AgentEvent != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "event: %s %s %s\n", event.AgentEvent.Type, event.AgentEvent.ToolName, event.AgentEvent.Message)
				}
				if event.ToolCall != nil {
					status := "rejected"
					if event.ToolCall.Approved {
						status = "approved"
					}
					parts := []string{event.ToolCall.Name, status, event.ToolCall.Output}
					if event.ToolCall.Error != "" {
						parts = append(parts, "error="+event.ToolCall.Error)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "tool: %s\n", strings.Join(parts, " "))
				}
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "export <session-id> <path>",
		Short: "Export a session JSONL trace",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := session.NewJSONLStore(effectiveDir()).Export(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "exported %s -> %s\n", args[0], args[1])
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "import <session-id> <path>",
		Short: "Import a session JSONL trace with a new session id",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := session.NewJSONLStore(effectiveDir()).Import(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "imported %s <- %s\n", args[0], args[1])
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "fork <source-session-id> <target-session-id>",
		Short: "Fork a session JSONL trace to a new id",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := session.NewJSONLStore(effectiveDir()).Fork(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "forked %s -> %s\n", args[0], args[1])
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := session.NewJSONLStore(effectiveDir()).Delete(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", args[0])
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "trace [session-id]",
		Short: "Print a diagnostic summary (turns, anomalies, tool stats)",
		Long: "Print a scannable summary of a session: one line per agent turn with its\n" +
			"step/tool count and finish state, an anomalies section flagging loop guards,\n" +
			"rejections, errors and unfinished turns, plus global tool stats. Streaming\n" +
			"noise (chunks/context) is collapsed.\n" +
			"Use this to hand off 'what happened' without pasting the raw JSONL.\n\n" +
			"With --debug, also folds in the model-decision layer from <id>.debug.jsonl\n" +
			"(only present if the session ran with --debug): per-turn prompt-cache hit\n" +
			"rate, token usage, and each tool's risk/decision/timing — the 'what the model\n" +
			"actually saw and decided' view that complements the audit layer.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := effectiveDir()
			store := session.NewJSONLStore(d)
			id := ""
			if len(args) == 1 {
				id = args[0]
			} else {
				latest, err := latestSessionID(d)
				if err != nil {
					return err
				}
				id = latest
			}
			events, err := store.Load(id)
			if err != nil {
				return err
			}
			var debugRecords []observe.Record
			if debugTrace, _ := cmd.Flags().GetBool("debug"); debugTrace {
				debugRecords, err = loadDebugRecords(d, id)
				if err != nil {
					return err
				}
			}
			runSessionTrace(cmd.OutOrStdout(), id, events, debugRecords)
			return nil
		},
	})
	// Last-added command is the trace subcommand; attach --debug to it.
	traceCmd := cmd.Commands()[len(cmd.Commands())-1]
	traceCmd.Flags().Bool("debug", false, "fold in the model-decision layer from <id>.debug.jsonl (cache/usage/tool decisions)")
	return cmd
}
