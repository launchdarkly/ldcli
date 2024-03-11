package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newHelloCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hello",
		Short: "A hello world command.",
		Long:  `A hello world command that prints out {"hello": "world"}`,
		RunE:  runHello,
	}

	// bind command-specific flags
	cmd.Flags().BoolP("informal", "i", false, "Make the greeting less formal")
	_ = viper.BindPFlag("informal", cmd.Flags().Lookup("informal"))

	return cmd
}

func runHello(cmd *cobra.Command, args []string) error {
	out := `{"hello": "world"}`
	if viper.GetBool("informal") {
		out = `{"hi": "world"}`
	}

	fmt.Fprintf(cmd.OutOrStdout(), out+"\n")

	return nil
}
