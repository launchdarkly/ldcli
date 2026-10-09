package awsdevops_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func TestCheckAWSCLIVersion(t *testing.T) {
	tests := map[string]struct {
		reported    string
		wantWarning bool
	}{
		"supported":   {reported: "aws-cli/2.36.0 Python/3.13.0 linux/6.8 exe/x86_64", wantWarning: false},
		"newer":       {reported: "aws-cli/2.41.2 Python/3.13.0 linux/6.8 exe/x86_64", wantWarning: false},
		"too old":     {reported: "aws-cli/2.35.9 Python/3.13.0 linux/6.8 exe/x86_64", wantWarning: true},
		"v1":          {reported: "aws-cli/1.42.0 Python/3.13.0 linux/6.8", wantWarning: true},
		"unparseable": {reported: "something else", wantWarning: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			withFakeAWSCLI(t, test.reported)

			warning := awsdevops.CheckAWSCLIVersion(context.Background())
			if test.wantWarning {
				assert.NotEmpty(t, warning)

				return
			}
			assert.Empty(t, warning)
		})
	}
}

func TestCheckAWSCLIVersionWithoutAWSCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	assert.Contains(t, awsdevops.CheckAWSCLIVersion(context.Background()), "not installed")
}

func withFakeAWSCLI(t *testing.T, reported string) {
	t.Helper()

	dir := t.TempDir()
	script := "#!/bin/sh\necho \"" + reported + "\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aws"), []byte(script), 0o700))
	t.Setenv("PATH", dir)
}
