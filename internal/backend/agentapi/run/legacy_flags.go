package run

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NormalizeLegacyFlagArgs keeps the old Go flag style (-long-name) working
// after the agentapi entrypoint moved to cobra/pflag (--long-name).
func NormalizeLegacyFlagArgs(args []string, command *cobra.Command) []string {
	if len(args) == 0 || command == nil {
		return args
	}
	out := make([]string, len(args))
	copy(out, args)
	for i, arg := range out {
		if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") || len(arg) <= 2 {
			continue
		}
		nameValue := strings.TrimPrefix(arg, "-")
		name, _, _ := strings.Cut(nameValue, "=")
		if commandFlag(command, name) == nil {
			continue
		}
		out[i] = "--" + nameValue
	}
	return out
}

func commandFlag(command *cobra.Command, name string) *pflag.Flag {
	if flag := command.Flags().Lookup(name); flag != nil {
		return flag
	}
	if flag := command.PersistentFlags().Lookup(name); flag != nil {
		return flag
	}
	if flag := command.InheritedFlags().Lookup(name); flag != nil {
		return flag
	}
	return nil
}
