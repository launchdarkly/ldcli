package awsdevopsagent

import (
	"bytes"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/analytics"
)

// Both flags must fail before any AWS call.
func TestSetupRejectsSpaceIDWithNewSpace(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/none")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/none")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	cmd := NewSetupCmd(analytics.NoopClientFn{}.Tracker())
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--agent-space-id", "space-2", "--new-agent-space", "--region", "us-east-1"})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), agentSpaceIDFlag)
	assert.Contains(t, err.Error(), newAgentSpaceFlag)
	assert.NotContains(t, err.Error(), "AWS")
}

func TestSetupAcceptsEachSpaceFlagAlone(t *testing.T) {
	for _, args := range [][]string{{"--agent-space-id", "space-2"}, {"--new-agent-space"}} {
		cmd := NewSetupCmd(nil)
		require.NoError(t, cmd.ParseFlags(args))
		assert.NoError(t, cmd.ValidateFlagGroups())
	}
}

func spaceIDRun(t *testing.T, flags map[string]string) []string {
	t.Helper()
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	require.NoError(t, cmd.Flags().Set(agentSpaceIDFlag, "space-2"))
	for name, value := range flags {
		require.NoError(t, cmd.Flags().Set(name, value))
	}
	clients, w := countingClients(agenttypes.AgentSpace{
		AgentSpaceId: aws.String("space-2"),
		Name:         aws.String("platform"),
	})
	opts, err := setupOptions(cmd)
	require.NoError(t, err)
	opts.LDAccessToken = "api-token"

	require.NoError(t, setupWith(cmd, clients, opts, true, false))

	return w.calls
}

// An auth flag asks for full setup.
func TestSpaceIDWithAuthFlagRunsFullSetup(t *testing.T) {
	cases := map[string]map[string]string{
		"auth-flow":        {authFlowFlag: "iam"},
		"idc-instance-arn": {authFlowFlag: "idc", idcInstanceARNFlag: "arn:aws:sso:::instance/ssoins-1"},
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			calls := spaceIDRun(t, flags)

			assert.Contains(t, calls, "EnableOperatorApp")
			assert.Contains(t, calls, "CreateRole")
			assert.NotContains(t, calls, "CreateAgentSpace")
		})
	}
}

// Without one, setup only connects.
func TestSpaceIDAloneOnlyConnects(t *testing.T) {
	calls := spaceIDRun(t, nil)

	assert.NotContains(t, calls, "EnableOperatorApp")
	assert.NotContains(t, calls, "CreateRole")
	assert.NotContains(t, calls, "CreateAgentSpace")
	assert.Contains(t, calls, "RegisterService")
}

// Sign-in flags need their matching --auth-flow.
func TestSignInFlagNeedsMatchingAuthFlow(t *testing.T) {
	cases := map[string]map[string]string{
		"idc arn alone":       {idcInstanceARNFlag: "arn:aws:sso:::instance/ssoins-1"},
		"idc arn with iam":    {authFlowFlag: "iam", idcInstanceARNFlag: "arn:aws:sso:::instance/ssoins-1"},
		"issuer url alone":    {issuerURLFlag: "https://idp.example.com"},
		"idp client with idc": {authFlowFlag: "idc", idcInstanceARNFlag: "arn:aws:sso:::instance/ssoins-1", idpClientIDFlag: "client"},
		"idp secret alone":    {idpClientSecretFlag: "secret"},
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			cmd := NewSetupCmd(nil)
			for flag, value := range flags {
				require.NoError(t, cmd.Flags().Set(flag, value))
			}

			_, err := setupOptions(cmd)

			assert.Error(t, err)
		})
	}
}

func TestSignInFlagsWithMatchingAuthFlowPass(t *testing.T) {
	cases := map[string]map[string]string{
		"default iam": {},
		"idc upper":   {authFlowFlag: "IDC", idcInstanceARNFlag: "arn:aws:sso:::instance/ssoins-1"},
		"idp":         {authFlowFlag: "idp", issuerURLFlag: "https://idp.example.com", idpClientIDFlag: "client", idpClientSecretFlag: "secret"},
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			cmd := NewSetupCmd(nil)
			for flag, value := range flags {
				require.NoError(t, cmd.Flags().Set(flag, value))
			}

			_, err := setupOptions(cmd)

			assert.NoError(t, err)
		})
	}
}
