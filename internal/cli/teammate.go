package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/junnhwan/bond-code/internal/teammate"
	"github.com/spf13/cobra"
)

func newTeammateClientCommand() *cobra.Command {
	var cfg teammate.Config
	cmd := &cobra.Command{
		Use:    "teammate-client",
		Short:  "Run the restricted local collaboration terminal client",
		Hidden: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			required := []struct{ name, value string }{{"parent-endpoint", cfg.ParentEndpoint}, {"launch-token-file", cfg.LaunchTokenFile}, {"task-id", cfg.TaskID}, {"session-id", cfg.SessionID}, {"backend-ownership-id", cfg.OwnershipID}}
			for _, field := range required {
				if strings.TrimSpace(field.value) == "" {
					return fmt.Errorf("--%s is required", field.name)
				}
			}
			if cfg.Generation == 0 {
				return fmt.Errorf("--generation must be greater than zero")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return teammate.Run(cmd.Context(), cfg, os.Stdin, os.Stdout)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&cfg.ParentEndpoint, "parent-endpoint", "", "loopback parent endpoint")
	flags.StringVar(&cfg.LaunchTokenFile, "launch-token-file", "", "protected one-time token file")
	flags.StringVar(&cfg.TaskID, "task-id", "", "canonical task ID")
	flags.StringVar(&cfg.SessionID, "session-id", "", "parent session ID")
	flags.StringVar(&cfg.TeamID, "team-id", "", "team ID")
	flags.StringVar(&cfg.MemberID, "member-id", "", "member ID")
	flags.Uint64Var(&cfg.Generation, "generation", 0, "task generation")
	flags.StringVar(&cfg.OwnershipID, "backend-ownership-id", "", "backend ownership fence")
	return cmd
}
