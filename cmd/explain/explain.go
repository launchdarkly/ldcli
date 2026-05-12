// Package explain implements the `ldcli explain` subcommand: a structured,
// machine-readable view of any ldcli command's schema, designed for LLM
// agents that would otherwise spend many turns chasing `--help` output.
//
// The command resolves a command path to an explainer via
// internal/explain.Registry, then renders JSON (default for agents) or
// markdown (default for humans on a TTY).
package explain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/explain"
)

const (
	formatJSON     = "json"
	formatMarkdown = "markdown"
)

// NewExplainCmd builds the `explain` subcommand. The Registry is passed in so
// tests can substitute a custom one; production callers should use
// explain.DefaultRegistry().
func NewExplainCmd(registry *explain.Registry) *cobra.Command {
	var formatFlag string
	var markdownFlag bool

	cmd := &cobra.Command{
		Use:   "explain <command> [subcommand...]",
		Short: "Print a machine-readable schema for an ldcli command",
		Long: `Print a structured description of an ldcli command — including its inputs,
output shape, error catalog, and curated examples — so that LLM agents can
construct payloads without recursing through "--help".

The output is JSON by default (the canonical agent format). Use --markdown
for a human-readable view.

Examples:

  # JSON schema for the highest-value write command
  ldcli explain flags update

  # Markdown view (good for humans)
  ldcli explain flags list --markdown

  # List which commands have a curated schema today
  ldcli explain --list
`,
		// We don't talk to the API and we don't read config; bypass the root
		// --access-token requirement.
		DisableFlagParsing: false,
		SilenceUsage:       true,
		RunE: func(c *cobra.Command, args []string) error {
			if markdownFlag {
				formatFlag = formatMarkdown
			}

			listOnly, _ := c.Flags().GetBool("list")
			if listOnly {
				return runList(c, registry)
			}

			if len(args) == 0 {
				return errors.New("explain requires a command path, e.g. `ldcli explain flags update`")
			}

			path := normalizePath(args)
			expl, err := registry.Resolve(path)
			if err != nil {
				if errors.Is(err, explain.ErrCommandNotFound) {
					return fmt.Errorf(
						"no explanation available for `%s`. Run `ldcli explain --list` to see covered commands; until full coverage lands, fall back to `ldcli %s --help`",
						strings.Join(path, " "), strings.Join(path, " "),
					)
				}
				return err
			}

			switch formatFlag {
			case formatMarkdown:
				fmt.Fprint(c.OutOrStdout(), explain.RenderMarkdown(expl))
			case formatJSON, "":
				out, err := explain.RenderJSON(expl)
				if err != nil {
					return err
				}
				fmt.Fprintln(c.OutOrStdout(), out)
			default:
				return fmt.Errorf("unsupported --format: %q (expected `json` or `markdown`)", formatFlag)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", formatJSON, "Output format: `json` (default) or `markdown`.")
	cmd.Flags().BoolVar(&markdownFlag, "markdown", false, "Shortcut for --format markdown.")
	cmd.Flags().Bool("list", false, "List commands that have curated explanations.")
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runList(c *cobra.Command, registry *explain.Registry) error {
	paths := registry.CuratedPaths()
	if len(paths) == 0 {
		fmt.Fprintln(c.OutOrStdout(), "No curated explanations registered.")
		return nil
	}
	fmt.Fprintln(c.OutOrStdout(), "Commands with curated explanations:")
	for _, p := range paths {
		fmt.Fprintf(c.OutOrStdout(), "  ldcli %s\n", p)
	}
	return nil
}

// normalizePath accepts argv-style input ("flags update") or a single
// space-joined string ("flags update") and returns the canonical slice.
func normalizePath(args []string) []string {
	if len(args) == 1 && strings.Contains(args[0], " ") {
		return strings.Fields(args[0])
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if a == "ldcli" {
			continue
		}
		out = append(out, a)
	}
	return out
}
