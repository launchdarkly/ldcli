package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/setup"
	"github.com/launchdarkly/ldcli/internal/setup/agent"
)

// agentMode is the value of the --agent flag.
type agentMode string

const (
	// agentOff always uses the init templates. It is the zero value, so a wizard
	// built without a mode never starts OpenCode.
	agentOff agentMode = ""
	// agentAuto uses the agent if it can run, and the init templates if not.
	agentAuto agentMode = "auto"
	// agentOn uses the agent, and stops with an error if it cannot run.
	agentOn agentMode = "on"
)

const agentFlag = "agent"

func parseAgentMode(s string) (agentMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto":
		return agentAuto, nil
	case "on":
		return agentOn, nil
	case "off":
		return agentOff, nil
	}
	return agentOff, errors.NewError(fmt.Sprintf("--agent must be auto, on, or off, not %q", s))
}

// agentTask builds the agent task from the wizard's detection result.
func agentTask(dir string, detection *setup.DetectResult, flagKey string, sdkInstalled bool) agent.Task {
	t := agent.Task{Dir: dir, FlagKey: flagKey, SDKInstalled: sdkInstalled}
	if detection != nil {
		t.SDKID = detection.SDKID
		t.Language = detection.Language
		t.Framework = detection.Framework
		t.PackageManager = detection.PackageManager
		if detection.EntryPointExists {
			t.EntryPoint = detection.EntryPoint
		}
	}
	return t
}

// credentialExport returns the shell command that sets the credential that
// the agent's code reads for the SDK, or an empty string if that credential is
// not known. The agent never sees the value, so the user must set it before
// the application starts. Only the SDK's own credential is shown, so the
// screen does not print keys that the application does not use.
func credentialExport(sdkID, sdkKey, clientSideID, mobileKey string) string {
	name := agent.CredentialEnvVar(sdkID)
	value := map[string]string{
		agent.SDKKeyEnvVar:       sdkKey,
		agent.ClientSideIDEnvVar: clientSideID,
		agent.MobileKeyEnvVar:    mobileKey,
	}[name]
	if value == "" {
		return ""
	}
	return fmt.Sprintf("export %s=%s", name, value)
}

// describeTool returns one line for a tool call.
func describeTool(tc agent.ToolCall) string {
	what := tc.Name
	if tc.Title != "" {
		what = tc.Name + ": " + tc.Title
	}
	switch tc.Status {
	case "completed":
		return "✓ " + what
	case "error":
		return "✗ " + what
	}
	return "… " + what
}

// describePermission returns the question for a permission request.
func describePermission(req agent.PermissionRequest) string {
	if len(req.Patterns) == 0 {
		return fmt.Sprintf("The agent asks to use %s.", req.Permission)
	}
	return fmt.Sprintf("The agent asks to use %s: %s", req.Permission, strings.Join(req.Patterns, ", "))
}

func newAgentCmd(svc setup.Service, runner agent.Runner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Use the setup agent to add SDK initialization code to a project",
		Long: `Runs only the agent step of setup.

The setup agent uses OpenCode to edit the project. OpenCode must be installed.
The agent asks before it runs a shell command. It never receives your
LaunchDarkly credentials: the code that it writes reads them from environment
variables.`,
		RunE: runAgent(svc, runner),
	}

	cmd.Flags().String(pathFlag, ".", "Path to the project directory")
	cmd.Flags().String(sdkIDFlag, "", "SDK identifier (e.g. node-server, react-client-sdk)")
	_ = cmd.MarkFlagRequired(sdkIDFlag)
	cmd.Flags().String(flagKeyFlag, "", "Feature flag key that the new code evaluates")
	_ = cmd.MarkFlagRequired(flagKeyFlag)
	cmd.Flags().String(sdkKeyFlag, "", "Server-side SDK key, shown in the closing instructions only")
	cmd.Flags().String(clientIDFlag, "", "Client-side environment ID, shown in the closing instructions only")
	cmd.Flags().String(mobileFlag, "", "Mobile SDK key, shown in the closing instructions only")

	return cmd
}

func runAgent(svc setup.Service, runner agent.Runner) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		dir, err := filepath.Abs(getFlag(cmd, pathFlag))
		if err != nil {
			return err
		}
		sdkID := getFlag(cmd, sdkIDFlag)

		// Reuse detection for the facts in the prompt. If the project is a
		// different language from the SDK, the detected entry point is wrong, so
		// look it up again for the SDK that the user named.
		detection, err := svc.Detect(dir)
		if err != nil || detection == nil {
			detection = &setup.DetectResult{}
		}
		if detection.SDKID != sdkID {
			detection.SDKID = sdkID
			detection.Framework = ""
			detection.EntryPoint, detection.EntryPointExists = setup.EntryPointFor(dir, sdkID)
			detection.PackageManager = setup.PackageManagerFor(dir, sdkID)
		}
		task := agentTask(dir, detection, getFlag(cmd, flagKeyFlag), setup.IsInstalled(dir, sdkID))

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		if err := runner.Check(ctx); err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Starting the setup agent in %s\n", dir)
		run, err := runner.Start(ctx, task)
		if err != nil {
			return err
		}
		defer run.Close()

		files, err := streamAgent(ctx, run, out, bufio.NewReader(cmd.InOrStdin()))
		if err != nil {
			return err
		}

		fmt.Fprintln(out)
		fmt.Fprintln(out)
		if len(files) == 0 {
			fmt.Fprintln(out, "The setup agent finished but did not report changed files.")
		} else {
			fmt.Fprintln(out, "The setup agent changed these files:")
			for _, f := range files {
				fmt.Fprintf(out, "  %s\n", f)
			}
		}
		if export := credentialExport(sdkID, getFlag(cmd, sdkKeyFlag), getFlag(cmd, clientIDFlag), getFlag(cmd, mobileFlag)); export != "" {
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Set the credential before you start your application:")
			fmt.Fprintf(out, "  %s\n", export)
		}
		return nil
	}
}

// streamAgent prints the run's events and asks the user to answer permission
// requests. It returns the changed files when the run finishes.
func streamAgent(ctx context.Context, run agent.Run, out io.Writer, in *bufio.Reader) ([]string, error) {
	changed := map[string]bool{}
	for {
		var ev agent.Event
		var ok bool
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ev, ok = <-run.Events():
		}
		if !ok {
			return nil, errors.NewError("The setup agent stopped without a result.")
		}
		switch ev.Kind {
		case agent.EventText:
			fmt.Fprint(out, ev.Text)
		case agent.EventTool:
			if ev.Tool.Status != "running" {
				fmt.Fprintf(out, "\n%s\n", describeTool(ev.Tool))
			}
		case agent.EventFileEdited:
			changed[ev.File] = true
		case agent.EventUsage:
			if ev.Usage.ModelID != "" {
				fmt.Fprintf(out, "\n(model %s/%s, %d input and %d output tokens)\n",
					ev.Usage.ProviderID, ev.Usage.ModelID, ev.Usage.InputTokens, ev.Usage.OutputTokens)
			}
		case agent.EventPermission:
			fmt.Fprintf(out, "\n%s\nAllow? [y/N] ", describePermission(ev.Permission))
			answer, _ := in.ReadString('\n')
			allow := strings.EqualFold(strings.TrimSpace(answer), "y")
			if err := run.Reply(ctx, ev.Permission, allow); err != nil {
				return nil, fmt.Errorf("could not answer the agent: %w", err)
			}
		case agent.EventDone:
			return sortedKeys(changed), nil
		case agent.EventError:
			return nil, errors.NewError("The setup agent failed: " + ev.Text)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
