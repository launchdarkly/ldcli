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
	spaces   []agenttypes.AgentSpace
	services []agenttypes.RegisteredService
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

func (s stubAgent) GetOperatorApp(context.Context, *devopsagent.GetOperatorAppInput, ...func(*devopsagent.Options)) (*devopsagent.GetOperatorAppOutput, error) {
	return nil, &agenttypes.ResourceNotFoundException{}
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
	return &devopsagent.ListServicesOutput{Services: s.services}, nil
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

	chosen, err := selectAgentSpace(cmd, testSpaces(), "us-east-1", "")
	require.NoError(t, err)

	assert.Equal(t, spaceChoice{id: "space-2"}, chosen)
	assert.Contains(t, out.String(), "2) platform (space-2)")
}

// Without a name, Enter picks nothing.
func TestSelectAgentSpaceWithoutANameHasNoDefault(t *testing.T) {
	cmd, out := testCmd("\n3\n")

	chosen, err := selectAgentSpace(cmd, testSpaces(), "us-east-1", "")
	require.NoError(t, err)

	assert.Equal(t, spaceChoice{newSpace: true}, chosen)
	assert.Contains(t, out.String(), "3) provision a new agent space\n")
	assert.NotContains(t, out.String(), "named")
	assert.NotContains(t, out.String(), "[")
	assert.Equal(t, 1, strings.Count(out.String(), "Enter a number between 1 and 3"))
}

func TestSelectAgentSpaceDefaultsToTheSpaceWithTheName(t *testing.T) {
	cmd, out := testCmd("\n")

	chosen, err := selectAgentSpace(cmd, testSpaces(), "us-east-1", "Platform")
	require.NoError(t, err)

	assert.Equal(t, spaceChoice{id: "space-2"}, chosen)
	assert.Contains(t, out.String(), "connect to? [2] ")
	assert.Contains(t, out.String(), "3) provision a new agent space\n")
	assert.NotContains(t, out.String(), "named")
}

func TestSelectAgentSpaceDefaultsToANewSpaceWithAnUnusedName(t *testing.T) {
	cmd, out := testCmd("\n")

	chosen, err := selectAgentSpace(cmd, testSpaces(), "us-east-1", "payments")
	require.NoError(t, err)

	assert.Equal(t, spaceChoice{newSpace: true, name: "payments"}, chosen)
	assert.Contains(t, out.String(), "3) provision a new agent space named payments")
	assert.Contains(t, out.String(), "4) provision a new agent space\n")
	assert.Contains(t, out.String(), "connect to? [3] ")
}

func TestSelectAgentSpaceLetsYouNameANewSpaceAfterAName(t *testing.T) {
	cmd, _ := testCmd("4\n")

	chosen, err := selectAgentSpace(cmd, testSpaces(), "us-east-1", "payments")
	require.NoError(t, err)

	assert.Equal(t, spaceChoice{newSpace: true}, chosen)
}

func TestSelectAgentSpaceMarksTheSpacesSetupMade(t *testing.T) {
	cmd, out := testCmd("1\n")
	spaces := testSpaces()
	spaces[0].ManagedByLDCLI = true

	_, err := selectAgentSpace(cmd, spaces, "us-east-1", "")
	require.NoError(t, err)

	assert.Contains(t, out.String(), "1) launchdarkly (space-1), made by ldcli")
	assert.Contains(t, out.String(), "2) platform (space-2)\n")
}

func TestSelectAgentSpaceAsksAgainAfterAnInvalidAnswer(t *testing.T) {
	cmd, out := testCmd("9\nnope\n1\n")

	chosen, err := selectAgentSpace(cmd, testSpaces(), "us-east-1", "")
	require.NoError(t, err)

	assert.Equal(t, spaceChoice{id: "space-1"}, chosen)
	assert.Equal(t, 2, strings.Count(out.String(), "Enter a number between 1 and 3"))
}

func TestSelectAgentSpaceFailsWithoutAnAnswer(t *testing.T) {
	cmd, _ := testCmd("")

	_, err := selectAgentSpace(cmd, testSpaces(), "us-east-1", "")

	assert.ErrorContains(t, err, "no agent space was chosen")
}

func TestAskSpaceNameAsksAgainForAnEmptyOrUsedName(t *testing.T) {
	cmd, out := testCmd("\nPLATFORM\n  payments  \n")

	name, err := askSpaceName(cmd, testSpaces())
	require.NoError(t, err)

	assert.Equal(t, "payments", name)
	assert.Contains(t, out.String(), "Enter a name\n")
	assert.Contains(t, out.String(), "An agent space named PLATFORM already exists")
}

func TestAskSpaceNameFailsWithoutAnAnswer(t *testing.T) {
	cmd, _ := testCmd("")

	_, err := askSpaceName(cmd, testSpaces())

	assert.ErrorContains(t, err, "--agent-space-name")
}

func TestOwnSpace(t *testing.T) {
	managed := func(id, name string) awsdevops.AgentSpaceSummary {
		return awsdevops.AgentSpaceSummary{AgentSpaceID: id, Name: name, ManagedByLDCLI: true}
	}
	other := func(id, name string) awsdevops.AgentSpaceSummary {
		return awsdevops.AgentSpaceSummary{AgentSpaceID: id, Name: name}
	}
	cases := map[string]struct {
		spaces []awsdevops.AgentSpaceSummary
		want   int
	}{
		"one made by setup":  {[]awsdevops.AgentSpaceSummary{other("a", "platform"), managed("b", "ops")}, 1},
		"none made by setup": {[]awsdevops.AgentSpaceSummary{other("a", "launchdarkly")}, -1},
		"two made by setup":  {[]awsdevops.AgentSpaceSummary{managed("a", "ops"), managed("b", "oncall")}, -1},
		"no spaces":          {nil, -1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.want, ownSpace(c.spaces))
		})
	}
}

func newStubClients(spaces ...agenttypes.AgentSpace) awsdevops.Clients {
	return awsdevops.Clients{Agent: stubAgent{spaces: spaces}, Region: "us-east-1"}
}

func TestResolveAgentSpaceProvisionsWhenTheAccountHasNone(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{AgentSpaceName: "launchdarkly"}

	provision, err := resolveAgentSpace(cmd, newStubClients(), &opts, false)
	require.NoError(t, err)

	assert.True(t, provision)
	assert.Empty(t, opts.AgentSpaceID)
}

func TestResolveAgentSpaceTakesTheFlaggedSpace(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{AgentSpaceID: "space-2", AgentSpaceName: "launchdarkly"}

	provision, err := resolveAgentSpace(cmd, newStubClients(), &opts, false)
	require.NoError(t, err)

	assert.False(t, provision)
}

// Without a terminal there is nobody to ask, so a space this command owns is
// reused and anything else is an error rather than a guess.
func TestResolveAgentSpaceReusesTheOwnSpaceWithoutATerminal(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{AgentSpaceName: "launchdarkly"}
	clients := newStubClients(ldcliSpace("space-1", "launchdarkly"))

	provision, err := resolveAgentSpace(cmd, clients, &opts, false)
	require.NoError(t, err)

	assert.True(t, provision)
	assert.Equal(t, "space-1", opts.AgentSpaceID)
}

func TestResolveAgentSpaceFailsOnSomeoneElsesSpaceWithoutATerminal(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{}
	clients := newStubClients(agenttypes.AgentSpace{
		AgentSpaceId: aws.String("space-2"),
		Name:         aws.String("platform"),
	})

	_, err := resolveAgentSpace(cmd, clients, &opts, false)

	assert.ErrorContains(t, err, "platform space-2")
	assert.ErrorContains(t, err, "--agent-space-id")
}

func TestResolveAgentSpaceAsksAndConnectsToTheChosenSpace(t *testing.T) {
	cmd, _ := testCmd("1\n")
	opts := awsdevops.SetupOptions{AgentSpaceName: "launchdarkly"}
	clients := newStubClients(agenttypes.AgentSpace{
		AgentSpaceId: aws.String("space-2"),
		Name:         aws.String("platform"),
	})

	provision, err := resolveAgentSpace(cmd, clients, &opts, true)
	require.NoError(t, err)

	assert.False(t, provision)
	assert.Equal(t, "space-2", opts.AgentSpaceID)
}

func TestResolveAgentSpaceProvisionsANewSpaceWhenAsked(t *testing.T) {
	cmd, _ := testCmd("2\nfresh\n")
	opts := awsdevops.SetupOptions{AgentSpaceName: "launchdarkly"}
	clients := newStubClients(agenttypes.AgentSpace{
		AgentSpaceId: aws.String("space-1"),
		Name:         aws.String("launchdarkly"),
	})

	provision, err := resolveAgentSpace(cmd, clients, &opts, true)
	require.NoError(t, err)

	assert.True(t, provision)
	assert.Empty(t, opts.AgentSpaceID)
	// Without this the matching name makes Setup reuse the space the answer
	// declined.
	assert.True(t, opts.NewAgentSpace)
	assert.Equal(t, "fresh", opts.AgentSpaceName)
}

func TestResolveAgentSpaceAsksForANameInAnEmptyAccount(t *testing.T) {
	cmd, out := testCmd("payments\n")
	opts := awsdevops.SetupOptions{}

	provision, err := resolveAgentSpace(cmd, newStubClients(), &opts, true)
	require.NoError(t, err)

	assert.True(t, provision)
	assert.Equal(t, "payments", opts.AgentSpaceName)
	assert.NotContains(t, out.String(), "Which one")
}

func TestResolveAgentSpaceAsksForANameWithNewAgentSpace(t *testing.T) {
	cmd, out := testCmd("platform\npayments\n")
	opts := awsdevops.SetupOptions{NewAgentSpace: true}
	clients := newStubClients(agenttypes.AgentSpace{AgentSpaceId: aws.String("space-2"), Name: aws.String("platform")})

	provision, err := resolveAgentSpace(cmd, clients, &opts, true)
	require.NoError(t, err)

	assert.True(t, provision)
	assert.Equal(t, "payments", opts.AgentSpaceName)
	assert.Contains(t, out.String(), "An agent space named platform already exists")
	assert.NotContains(t, out.String(), "Which one")
}

// Nobody can type a name, so the run needs the flag.
func TestResolveAgentSpaceNeedsANameWithoutATerminal(t *testing.T) {
	for name, opts := range map[string]awsdevops.SetupOptions{
		"empty account":   {},
		"new-agent-space": {NewAgentSpace: true},
	} {
		t.Run(name, func(t *testing.T) {
			cmd, _ := testCmd("")

			_, err := resolveAgentSpace(cmd, newStubClients(), &opts, false)

			assert.ErrorContains(t, err, "--agent-space-name")
		})
	}
}

func TestResolveAgentSpaceReusesTheOnlySpaceSetupMadeWithoutAName(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{}
	clients := newStubClients(
		agenttypes.AgentSpace{AgentSpaceId: aws.String("space-1"), Name: aws.String("platform")},
		ldcliSpace("space-2", "oncall"),
	)

	provision, err := resolveAgentSpace(cmd, clients, &opts, false)
	require.NoError(t, err)

	assert.True(t, provision)
	assert.Equal(t, "space-2", opts.AgentSpaceID)
}
