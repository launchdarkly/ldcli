package wizard_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/ldcli/internal/wizard"
)

func TestInstallArgs(t *testing.T) {
	tests := map[string]struct {
		sdkID          string
		packageManager string
		wantArgs       []string
	}{
		"react with npm": {
			sdkID:          "react-client-sdk",
			packageManager: "npm",
			wantArgs:       []string{"npm", "install", "launchdarkly-react-client-sdk"},
		},
		"react with yarn": {
			sdkID:          "react-client-sdk",
			packageManager: "yarn",
			wantArgs:       []string{"yarn", "install", "launchdarkly-react-client-sdk"},
		},
		"react with pnpm": {
			sdkID:          "react-client-sdk",
			packageManager: "pnpm",
			wantArgs:       []string{"pnpm", "install", "launchdarkly-react-client-sdk"},
		},
		"node-server with npm": {
			sdkID:          "node-server",
			packageManager: "npm",
			wantArgs:       []string{"npm", "install", "@launchdarkly/node-server-sdk"},
		},
		"node-server with yarn": {
			sdkID:          "node-server",
			packageManager: "yarn",
			wantArgs:       []string{"yarn", "install", "@launchdarkly/node-server-sdk"},
		},
		"python with pip": {
			sdkID:          "python-server-sdk",
			packageManager: "pip",
			wantArgs:       []string{"pip", "install", "launchdarkly-server-sdk"},
		},
		"python with pip3": {
			sdkID:          "python-server-sdk",
			packageManager: "pip3",
			wantArgs:       []string{"pip3", "install", "launchdarkly-server-sdk"},
		},
		"python defaults to pip when package manager is empty": {
			sdkID:          "python-server-sdk",
			packageManager: "",
			wantArgs:       []string{"pip", "install", "launchdarkly-server-sdk"},
		},
		"go always uses go get regardless of package manager": {
			sdkID:          "go-server-sdk",
			packageManager: "npm",
			wantArgs:       []string{"go", "get", "github.com/launchdarkly/go-server-sdk/v7"},
		},
		"java returns nil (manual install)": {
			sdkID:          "java-server-sdk",
			packageManager: "maven",
			wantArgs:       nil,
		},
		"unknown SDK returns nil": {
			sdkID:          "unknown-sdk",
			packageManager: "npm",
			wantArgs:       nil,
		},
	}

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			got := wizard.InstallArgs(tt.sdkID, tt.packageManager)

			assert.Equal(t, tt.wantArgs, got)
		})
	}
}
