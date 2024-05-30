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
