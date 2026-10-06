package awsdevopsagent

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

type stubAgent struct {
	spaces []agenttypes.AgentSpace
}

func (s stubAgent) AssociateService(context.Context, *devopsagent.AssociateServiceInput, ...func(*devopsagent.Options)) (*devopsagent.AssociateServiceOutput, error) {
	return nil, nil
}

func (s stubAgent) CreateAgentSpace(context.Context, *devopsagent.CreateAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.CreateAgentSpaceOutput, error) {
	return nil, nil
}

func (s stubAgent) DeregisterService(context.Context, *devopsagent.DeregisterServiceInput, ...func(*devopsagent.Options)) (*devopsagent.DeregisterServiceOutput, error) {
	return nil, nil
}

func (s stubAgent) DisassociateService(context.Context, *devopsagent.DisassociateServiceInput, ...func(*devopsagent.Options)) (*devopsagent.DisassociateServiceOutput, error) {
	return nil, nil
}

func (s stubAgent) EnableOperatorApp(context.Context, *devopsagent.EnableOperatorAppInput, ...func(*devopsagent.Options)) (*devopsagent.EnableOperatorAppOutput, error) {
	return nil, nil
}

func (s stubAgent) GetAgentSpace(context.Context, *devopsagent.GetAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.GetAgentSpaceOutput, error) {
	return nil, nil
}

func (s stubAgent) ListAgentSpaces(context.Context, *devopsagent.ListAgentSpacesInput, ...func(*devopsagent.Options)) (*devopsagent.ListAgentSpacesOutput, error) {
	return &devopsagent.ListAgentSpacesOutput{AgentSpaces: s.spaces}, nil
}

func (s stubAgent) ListAssociations(context.Context, *devopsagent.ListAssociationsInput, ...func(*devopsagent.Options)) (*devopsagent.ListAssociationsOutput, error) {
	return &devopsagent.ListAssociationsOutput{}, nil
}

func (s stubAgent) ListServices(context.Context, *devopsagent.ListServicesInput, ...func(*devopsagent.Options)) (*devopsagent.ListServicesOutput, error) {
	return &devopsagent.ListServicesOutput{}, nil
}

func (s stubAgent) RegisterService(context.Context, *devopsagent.RegisterServiceInput, ...func(*devopsagent.Options)) (*devopsagent.RegisterServiceOutput, error) {
	return nil, nil
}

func testCmd(stdin string) (*cobra.Command, *bytes.Buffer) {
	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetIn(strings.NewReader(stdin))

	return cmd, out
}

func testSpaces() []awsdevops.AgentSpaceSummary {
	return []awsdevops.AgentSpaceSummary{
		{AgentSpaceID: "space-1", Name: "launchdarkly"},
		{AgentSpaceID: "space-2", Name: "platform"},
	}
}

func TestSelectAgentSpaceTakesTheChosenSpace(t *testing.T) {
	cmd, out := testCmd("2\n")

	chosen, err := selectAgentSpace(cmd, testSpaces(), "us-east-1")
	require.NoError(t, err)

	assert.Equal(t, "space-2", chosen)
	assert.Contains(t, out.String(), "2) platform (space-2)")
}

func TestSelectAgentSpaceDefaultsToTheFirstSpace(t *testing.T) {
	cmd, _ := testCmd("\n")

	chosen, err := selectAgentSpace(cmd, testSpaces(), "us-east-1")
	require.NoError(t, err)

	assert.Equal(t, "space-1", chosen)
}

func TestSelectAgentSpaceAsksAgainAfterAnInvalidAnswer(t *testing.T) {
	cmd, out := testCmd("9\nnope\n1\n")

	chosen, err := selectAgentSpace(cmd, testSpaces(), "us-east-1")
	require.NoError(t, err)

	assert.Equal(t, "space-1", chosen)
	assert.Equal(t, 2, strings.Count(out.String(), "Enter a number between 1 and 2"))
}

func TestSelectAgentSpaceProvisionsANewSpaceOnN(t *testing.T) {
	cmd, _ := testCmd("n\n")

	chosen, err := selectAgentSpace(cmd, testSpaces(), "us-east-1")
	require.NoError(t, err)

	assert.Empty(t, chosen)
}

func TestSelectAgentSpaceFailsWithoutAnAnswer(t *testing.T) {
	cmd, _ := testCmd("")

	_, err := selectAgentSpace(cmd, testSpaces(), "us-east-1")

	assert.ErrorContains(t, err, "no agent space was chosen")
}

func newStubClients(spaces ...agenttypes.AgentSpace) awsdevops.Clients {
	return awsdevops.Clients{Agent: stubAgent{spaces: spaces}, Region: "us-east-1"}
}

func TestResolveAgentSpaceProvisionsWhenTheAccountHasNone(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{AgentSpaceName: "launchdarkly"}

	provision, err := resolveAgentSpace(cmd, newStubClients(), &opts, true)
	require.NoError(t, err)

	assert.True(t, provision)
	assert.Empty(t, opts.AgentSpaceID)
}

func TestResolveAgentSpaceTakesTheFlaggedSpace(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{AgentSpaceID: "space-2", AgentSpaceName: "launchdarkly"}

	provision, err := resolveAgentSpace(cmd, newStubClients(), &opts, true)
	require.NoError(t, err)

	assert.False(t, provision)
}

// Without a terminal there is nobody to ask, so a space this command owns is
// reused and anything else is an error rather than a guess.
func TestResolveAgentSpaceReusesTheOwnSpaceWithoutATerminal(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{AgentSpaceName: "launchdarkly"}
	clients := newStubClients(agenttypes.AgentSpace{
		AgentSpaceId: aws.String("space-1"),
		Name:         aws.String("launchdarkly"),
	})

	provision, err := resolveAgentSpace(cmd, clients, &opts, true)
	require.NoError(t, err)

	assert.True(t, provision)
}

func TestResolveAgentSpaceFailsOnSomeoneElsesSpaceWithoutATerminal(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{AgentSpaceName: "launchdarkly"}
	clients := newStubClients(agenttypes.AgentSpace{
		AgentSpaceId: aws.String("space-2"),
		Name:         aws.String("platform"),
	})

	_, err := resolveAgentSpace(cmd, clients, &opts, true)

	assert.ErrorContains(t, err, "platform space-2")
	assert.ErrorContains(t, err, "--agent-space-id")
}
