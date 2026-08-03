package run

import (
	"os"

	"github.com/spf13/cobra"

	startupconfig "claude-codex/internal/backend/agentapi/config"
)

func NewCommand() *cobra.Command {
	cfg := startupconfig.Default()
	command := &cobra.Command{
		Use:           "agentapi",
		Short:         "Run the agent API server",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.Validate(); err != nil {
				return err
			}
			return Run(cmd.Context(), cfg)
		},
	}
	startupconfig.BindFlags(command, &cfg)
	return command
}

func Main() {
	command := NewCommand()
	command.SetArgs(NormalizeLegacyFlagArgs(os.Args[1:], command))
	if err := command.Execute(); err != nil {
		logFatal(err)
	}
}
