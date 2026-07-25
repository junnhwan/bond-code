package cli

import (
	"encoding/json"
	"fmt"

	"github.com/junnhwan/bond-code/internal/config"

	"github.com/spf13/cobra"
)

func newConfigCommand() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage BondCode configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "example",
		Short: "Show example config path",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "configs/config.example.yaml")
			return nil
		},
	})
	show := &cobra.Command{
		Use:   "show",
		Short: "Load and print config as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			b, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	}
	show.Flags().StringVar(&path, "config", "configs/config.example.yaml", "path to config YAML")
	cmd.AddCommand(show)
	return cmd
}
