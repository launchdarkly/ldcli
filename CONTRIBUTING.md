# Contributing to the LaunchDarkly Command Line Interface (ldcli)

## Submitting bug reports and feature requests

The LaunchDarkly team monitors the [issue tracker](https://github.com/launchdarkly/ldcli/issues) in the repository. Bug reports and feature requests should be filed in this issue tracker.

## Submitting pull requests

We encourage pull requests and other contributions from the community. Before submitting pull requests, ensure that all temporary or unintended code is removed. Don't worry about adding reviewers to the pull request; the LaunchDarkly team will add themselves.

## Git Hooks

To install the repo's git hooks, run `make install-hooks`.

**pre-commit**

The pre-commit hook checks that relevant project files are formatted with `go fmt`, and that
the `go.mod/go.sum` files are tidy.

In addition, pre-commit will run dev server UI tests and build the project to make sure an up-to-date build is being checked in. You will need to install npm.

## The Rust port

The CLI is being ported to Rust. Both builds live in this repo: the Go tree is the released CLI and the reference for behavior, and `rust/crates/ldcli` is the port in progress.

`make parity` runs the same arguments against both binaries and diffs the exit code, stdout, stderr, and declared files. A command that the Rust binary does not implement yet is simply not compared; each case opts in with `rust = true` once it should match. [`parity/README.md`](parity/README.md) covers running the harness, reading a failure, and adding a case.

Two rules follow from this while the port is underway. Changing Go behavior means recapturing the affected transcripts with `make parity-capture`, and that diff is the behavior change under review. Changing Go output so that a Rust diff passes is backwards: the Go tree is what users have today.

## Adding a new command

There are a few things you need to do in order to wire up a new top-level command.

1. Add your command to the root command by calling `cmd.AddComand` in the `NewRootCommand` method of the `cmd` package.
2. Update the root command's usage template by modifying the `getUsageTemplate` method in the `cmd` package.
3. Instrument your command by setting a `PreRun` or `PersistentPreRun` on your command which calls `tracker.SendCommandRunEvent`. Example below.

```go
cmd := &cobra.Command{
    Use:   "dev-server",
    Short: "Development server",
    Long:  "Start and use a local development server for overriding flag values.",
    PersistentPreRun: func(cmd *cobra.Command, args []string) {

        tracker := analyticsTrackerFn(
            viper.GetString(cliflags.AccessTokenFlag),
            viper.GetString(cliflags.BaseURIFlag),
            viper.GetBool(cliflags.AnalyticsOptOut),
        )
        tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
            cmd,
            "dev-server",
            map[string]interface{}{
                "action": cmd.Name(),
            }))
    },
}

```
