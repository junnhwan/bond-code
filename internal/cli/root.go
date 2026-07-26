package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/command"
	commandbuiltin "github.com/junnhwan/bond-code/internal/command/builtin"
	"github.com/junnhwan/bond-code/internal/rpc"

	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	return newRootCommandWithBootstrap(app.Bootstrap)
}

// newHeadlessCommand 提供无 TUI 的 JSON-line 模式：stdin 读 send 命令，stdout 写最终
// 答案响应。用于嵌入 IDE/外部程序驱动 agent（rpc 协议骨架的 cmd 层接入）。
func newHeadlessCommand(bootstrap bootstrapFunc) *cobra.Command {
	var configPath string
	var fake, yes bool
	cmd := &cobra.Command{
		Use:   "headless",
		Short: "Run in headless JSON-line mode (stdin commands → stdout responses)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			application, err := bootstrap(app.Options{ConfigPath: configPath, UseFakeLLM: fake, AutoYes: yes})
			if err != nil {
				return err
			}
			return rpc.Serve(os.Stdin, os.Stdout, func(c rpc.Command) rpc.Response {
				if c.Type != "send" {
					return rpc.Response{Type: "error", Error: "unknown command type: " + c.Type}
				}
				var payload struct {
					Prompt string `json:"prompt"`
				}
				_ = json.Unmarshal(c.Payload, &payload)
				result, err := application.Agent.Run(cmd.Context(), payload.Prompt)
				if err != nil {
					return rpc.Response{Type: "error", Error: err.Error()}
				}
				return rpc.Response{Type: "result", OK: true, Payload: result.FinalAnswer}
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to config YAML")
	cmd.Flags().BoolVar(&fake, "fake", false, "use fake local LLM")
	cmd.Flags().BoolVar(&yes, "yes", false, "auto-approve low/medium risk tool calls")
	return cmd
}

func newRootCommandWithBootstrap(bootstrap bootstrapFunc) *cobra.Command {
	return newRootCommandWithBootstrapAndTUI(bootstrap, runTUI)
}

type tuiRunnerFunc func(context.Context, *app.App) error

func newRootCommandWithBootstrapAndTUI(bootstrap bootstrapFunc, tuiRunner tuiRunnerFunc) *cobra.Command {
	var configPath string
	var fake bool
	var yes bool
	var resume bool
	var debugLevel string
	cmd := &cobra.Command{
		Use:   "bondcode [session-id]",
		Short: "Open the BondCode interactive workspace",
		// Optional positional id is only meaningful with --resume
		// (`bondcode --resume` / `bondcode --resume <id>`).
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tuiConfirmer := newTUIConfirmer()
			tuiQuestioner := newTUIQuestioner()
			resumeID := ""
			openPicker := false
			if resume {
				if len(args) == 1 {
					resumeID = strings.TrimSpace(args[0])
				}
				if resumeID == "" {
					openPicker = true
				}
			} else if len(args) == 1 {
				return fmt.Errorf("unexpected argument %q (did you mean: bondcode --resume %s)", args[0], args[0])
			}
			application, err := bootstrap(app.Options{
				ConfigPath:        configPath,
				UseFakeLLM:        fake,
				AutoYes:           yes,
				Confirmer:         tuiConfirmer,
				Questioner:        tuiQuestioner,
				ResumeSessionID:   resumeID,
				OpenSessionPicker: openPicker,
				Debug:             parseDebugVerbose(debugLevel),
			})
			if err != nil {
				return err
			}
			defer application.Close()
			return tuiRunner(cmd.Context(), application)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to config YAML")
	cmd.Flags().BoolVar(&fake, "fake", false, "use fake local LLM for tests and demos")
	_ = cmd.Flags().MarkHidden("fake")
	cmd.Flags().BoolVar(&yes, "yes", false, "auto-approve low and medium risk tool calls")
	cmd.Flags().BoolVar(&resume, "resume", false, "resume a previous conversation (optional session id as argument; omit id to open the picker)")
	cmd.Flags().StringVar(&debugLevel, "debug", "", "enable debug trace at <data-dir>/<id>.debug.jsonl ('', 'default', or 'full'); BONDCODE_DEBUG env is equivalent")

	cmd.AddCommand(newHeadlessCommand(bootstrap))
	cmd.AddCommand(newTeammateClientCommand())
	cmd.AddCommand(newConfigCommand())
	sessionCmd := newSessionCommand()
	sessionCmd.Hidden = true
	cmd.AddCommand(sessionCmd)
	mcpCmd := newMCPCommand()
	mcpCmd.Hidden = true
	cmd.AddCommand(mcpCmd)
	addSlashEquivalentCommands(cmd, bootstrap)

	return cmd
}

func addSlashEquivalentCommands(root *cobra.Command, bootstrap bootstrapFunc) {
	registry := command.NewRegistry()
	_ = commandbuiltin.RegisterAll(registry)
	for _, name := range registry.Names() {
		cmdDef, _ := registry.Get(name)
		if cmdDef.Name == "session" || cmdDef.Name == "help" {
			continue
		}
		def := cmdDef
		root.AddCommand(&cobra.Command{
			Use:    def.Name,
			Short:  def.Description,
			Hidden: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				application, err := bootstrap(app.Options{})
				if err != nil {
					return err
				}
				result, err := def.Run(cmd.Context(), commandEnvForApp(application), args)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), result.Output)
				return nil
			},
		})
	}
}
