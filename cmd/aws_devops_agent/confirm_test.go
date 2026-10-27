package awsdevopsagent

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

const testAccountID = "123456789012"

// writes counts every AWS call that changes state.
type writes struct{ calls []string }

func (w *writes) add(name string) { w.calls = append(w.calls, name) }

type countingAgent struct {
	stubAgent
	writes *writes
}

func (a countingAgent) AssociateService(context.Context, *devopsagent.AssociateServiceInput, ...func(*devopsagent.Options)) (*devopsagent.AssociateServiceOutput, error) {
	a.writes.add("AssociateService")

	return &devopsagent.AssociateServiceOutput{Association: &agenttypes.Association{AssociationId: aws.String("assoc-1")}}, nil
}

func (a countingAgent) CreateAgentSpace(context.Context, *devopsagent.CreateAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.CreateAgentSpaceOutput, error) {
	a.writes.add("CreateAgentSpace")

	return &devopsagent.CreateAgentSpaceOutput{AgentSpace: &agenttypes.AgentSpace{AgentSpaceId: aws.String("space-new")}}, nil
}

func (a countingAgent) DeregisterService(context.Context, *devopsagent.DeregisterServiceInput, ...func(*devopsagent.Options)) (*devopsagent.DeregisterServiceOutput, error) {
	a.writes.add("DeregisterService")

	return &devopsagent.DeregisterServiceOutput{}, nil
}

func (a countingAgent) DisassociateService(context.Context, *devopsagent.DisassociateServiceInput, ...func(*devopsagent.Options)) (*devopsagent.DisassociateServiceOutput, error) {
	a.writes.add("DisassociateService")

	return &devopsagent.DisassociateServiceOutput{}, nil
}

func (a countingAgent) EnableOperatorApp(context.Context, *devopsagent.EnableOperatorAppInput, ...func(*devopsagent.Options)) (*devopsagent.EnableOperatorAppOutput, error) {
	a.writes.add("EnableOperatorApp")

	return &devopsagent.EnableOperatorAppOutput{OperatorAppUrl: aws.String("https://space-new.aidevops.global.app.aws")}, nil
}

func (a countingAgent) GetAgentSpace(_ context.Context, in *devopsagent.GetAgentSpaceInput, _ ...func(*devopsagent.Options)) (*devopsagent.GetAgentSpaceOutput, error) {
	for _, space := range a.spaces {
		if aws.ToString(space.AgentSpaceId) == aws.ToString(in.AgentSpaceId) {
			return &devopsagent.GetAgentSpaceOutput{AgentSpace: &space}, nil
		}
	}

	return &devopsagent.GetAgentSpaceOutput{AgentSpace: &agenttypes.AgentSpace{AgentSpaceId: in.AgentSpaceId}}, nil
}

func (a countingAgent) UpdateAgentSpace(context.Context, *devopsagent.UpdateAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.UpdateAgentSpaceOutput, error) {
	a.writes.add("UpdateAgentSpace")

	return &devopsagent.UpdateAgentSpaceOutput{}, nil
}

func (a countingAgent) RegisterService(context.Context, *devopsagent.RegisterServiceInput, ...func(*devopsagent.Options)) (*devopsagent.RegisterServiceOutput, error) {
	a.writes.add("RegisterService")

	return &devopsagent.RegisterServiceOutput{ServiceId: aws.String("mcp-1")}, nil
}

type countingIAM struct{ writes *writes }

func (c countingIAM) AttachRolePolicy(context.Context, *iam.AttachRolePolicyInput, ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
	c.writes.add("AttachRolePolicy")

	return &iam.AttachRolePolicyOutput{}, nil
}

func (c countingIAM) CreateRole(_ context.Context, in *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	c.writes.add("CreateRole")

	return &iam.CreateRoleOutput{Role: &iamtypes.Role{
		Arn: aws.String("arn:aws:iam::" + testAccountID + ":role/" + aws.ToString(in.RoleName)),
	}}, nil
}

func (c countingIAM) GetRole(context.Context, *iam.GetRoleInput, ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	return nil, &iamtypes.NoSuchEntityException{}
}

func (c countingIAM) ListRoleTags(context.Context, *iam.ListRoleTagsInput, ...func(*iam.Options)) (*iam.ListRoleTagsOutput, error) {
	return &iam.ListRoleTagsOutput{}, nil
}

func (c countingIAM) PutRolePolicy(context.Context, *iam.PutRolePolicyInput, ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	c.writes.add("PutRolePolicy")

	return &iam.PutRolePolicyOutput{}, nil
}

func (c countingIAM) UpdateAssumeRolePolicy(context.Context, *iam.UpdateAssumeRolePolicyInput, ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
	c.writes.add("UpdateAssumeRolePolicy")

	return &iam.UpdateAssumeRolePolicyOutput{}, nil
}

type accountSTS struct{}

func (accountSTS) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return &sts.GetCallerIdentityOutput{Account: aws.String(testAccountID)}, nil
}

func countingClients(spaces ...agenttypes.AgentSpace) (awsdevops.Clients, *writes) {
	w := &writes{}

	return awsdevops.Clients{
		Agent:  countingAgent{stubAgent: stubAgent{spaces: spaces}, writes: w},
		IAM:    countingIAM{writes: w},
		STS:    accountSTS{},
		Region: "eu-west-2",
	}, w
}

// noReadStdin fails the test on any read.
type noReadStdin struct{ t *testing.T }

func (r noReadStdin) Read([]byte) (int, error) {
	r.t.Error("setup read stdin")

	return 0, nil
}

// setupTestCmd returns stderr, where the plan and prompts go. Stdout is separate.
func setupTestCmd(t *testing.T, stdin string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	cmd := NewSetupCmd(nil)
	errOut := &bytes.Buffer{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(errOut)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetContext(context.Background())

	return cmd, errOut
}

func newSpaceOptions() awsdevops.SetupOptions {
	return awsdevops.SetupOptions{AgentSpaceName: "ld-devops-agent", AuthFlow: "iam", LDAccessToken: "api-token"}
}

// A json run in a terminal asks too, because the question goes to stderr.
func TestSetupJSONInATerminalAsks(t *testing.T) {
	cmd, out := setupTestCmd(t, "n\n")
	clients, w := countingClients()

	err := setupWith(cmd, clients, newSpaceOptions(), false, true)

	require.Error(t, err)
	assert.Contains(t, out.String(), "Continue? [Y/n]")
	assert.Empty(t, w.calls)
	assert.Empty(t, stdoutOf(cmd))
}

func TestSetupStopsUnlessTheAnswerIsYes(t *testing.T) {
	for name, answer := range map[string]string{
		"near miss ye":   "ye\n",
		"near miss yess": "yess\n",
		"no":             "n\n",
		"end of input":   "",
		"yes with words": "yes please\n",
	} {
		t.Run(name, func(t *testing.T) {
			cmd, _ := setupTestCmd(t, answer)
			clients, w := countingClients()

			err := setupWith(cmd, clients, newSpaceOptions(), true, true)

			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "cancel")
			assert.Empty(t, w.calls, "setup changed AWS before the user agreed")
		})
	}
}

func TestSetupContinuesOnYes(t *testing.T) {
	for name, answer := range map[string]string{
		"lowercase y":   "y\n",
		"uppercase Y":   "Y\n",
		"mixed yes":     "YeS\n",
		"padded yes":    "  yes  \n",
		"no final line": "y",
		"empty answer":  "\n",
	} {
		t.Run(name, func(t *testing.T) {
			cmd, _ := setupTestCmd(t, answer)
			clients, w := countingClients()

			err := setupWith(cmd, clients, newSpaceOptions(), true, true)

			require.NoError(t, err)
			assert.Contains(t, w.calls, "CreateAgentSpace")
		})
	}
}

func TestSetupWithYesFlagDoesNotReadStdin(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))
	clients, w := countingClients()

	err := setupWith(cmd, clients, newSpaceOptions(), true, true)

	require.NoError(t, err)
	assert.Contains(t, w.calls, "CreateAgentSpace")
}

func TestSetupWithoutATerminalDoesNotReadStdin(t *testing.T) {
	for name, plaintext := range map[string]bool{"plaintext": true, "json": false} {
		t.Run(name, func(t *testing.T) {
			cmd, _ := setupTestCmd(t, "")
			cmd.SetIn(noReadStdin{t})
			clients, w := countingClients()

			err := setupWith(cmd, clients, newSpaceOptions(), plaintext, false)

			require.NoError(t, err)
			assert.Contains(t, w.calls, "CreateAgentSpace")
		})
	}
}

func TestSetupPlanNamesWhereANewSpaceGoes(t *testing.T) {
	cmd, out := setupTestCmd(t, "n\n")
	require.NoError(t, cmd.Flags().Set(profileFlag, "staging-admin"))
	clients, _ := countingClients()

	_ = setupWith(cmd, clients, newSpaceOptions(), true, true)

	assert.Contains(t, out.String(), testAccountID)
	assert.Contains(t, out.String(), "eu-west-2")
	assert.Contains(t, out.String(), "staging-admin")
	assert.Contains(t, out.String(), "ld-devops-agent")
	assert.Contains(t, out.String(), "Continue? [Y/n]")
}

func TestSetupPlanSaysDefaultCredentialsWithoutAProfile(t *testing.T) {
	cmd, out := setupTestCmd(t, "n\n")
	clients, _ := countingClients()

	_ = setupWith(cmd, clients, newSpaceOptions(), true, true)

	assert.Contains(t, out.String(), "default credentials")
}

func TestSetupPlanNamesTheSpaceItConnectsTo(t *testing.T) {
	cmd, out := setupTestCmd(t, "ye\n")
	clients, w := countingClients(agenttypes.AgentSpace{
		AgentSpaceId: aws.String("space-2"),
		Name:         aws.String("platform"),
	})
	opts := newSpaceOptions()
	opts.AgentSpaceID = "space-2"

	err := setupWith(cmd, clients, opts, true, true)

	require.Error(t, err)
	assert.Empty(t, w.calls)
	assert.Contains(t, out.String(), testAccountID)
	assert.Contains(t, out.String(), "eu-west-2")
	assert.Contains(t, out.String(), "platform (space-2)")
}

// The choice and the answer share one stdin.
func TestSetupConfirmsAfterTheSpaceChoice(t *testing.T) {
	cmd, out := setupTestCmd(t, "1\nye\n")
	clients, w := countingClients(agenttypes.AgentSpace{
		AgentSpaceId: aws.String("space-2"),
		Name:         aws.String("platform"),
	})

	err := setupWith(cmd, clients, newSpaceOptions(), true, true)

	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "cancel")
	assert.Empty(t, w.calls)
	assert.Contains(t, out.String(), "platform (space-2)")
}

func stderrOf(cmd *cobra.Command) string {
	return cmd.ErrOrStderr().(*bytes.Buffer).String()
}

func stdoutOf(cmd *cobra.Command) string {
	return cmd.OutOrStdout().(*bytes.Buffer).String()
}

// The plan and progress go to stderr. Stdout keeps only the result.
func TestSetupProgressGoesToStderr(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, _ := countingClients()

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, false))

	assert.Contains(t, stderrOf(cmd), "Setup plan")
	assert.Contains(t, stderrOf(cmd), "Created agent space")
	assert.NotContains(t, stdoutOf(cmd), "Setup plan")
	assert.NotContains(t, stdoutOf(cmd), "Created agent space")
	assert.Contains(t, stdoutOf(cmd), "Open the DevOps Agent at")
}

// Prompts go to stderr, so a redirected stdout does not hide them.
func TestSetupPromptsGoToStderr(t *testing.T) {
	cmd, _ := setupTestCmd(t, "1\nn\n")
	clients, _ := countingClients(agenttypes.AgentSpace{
		AgentSpaceId: aws.String("space-2"),
		Name:         aws.String("platform"),
	})

	_ = setupWith(cmd, clients, newSpaceOptions(), true, true)

	assert.Contains(t, stderrOf(cmd), "Which agent space should LaunchDarkly connect to?")
	assert.Contains(t, stderrOf(cmd), "Continue? [Y/n]")
	assert.Empty(t, stdoutOf(cmd))
}
