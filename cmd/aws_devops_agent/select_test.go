package awsdevopsagent

import (
	"bytes"
	"context"
	"errors"
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
	// unreadTags fails the tag lookup of these space IDs.
	unreadTags map[string]bool
	// missing makes GetAgentSpace report these space IDs as not found.
	missing map[string]bool
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

// ListTagsForResource tags each space with the setup description, as setup
// does when it creates one.
func (s stubAgent) ListTagsForResource(_ context.Context, in *devopsagent.ListTagsForResourceInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListTagsForResourceOutput, error) {
	for id := range s.unreadTags {
		if strings.HasSuffix(aws.ToString(in.ResourceArn), ":agentspace/"+id) {
			return nil, errors.New("InternalServerException")
		}
	}
	for _, space := range s.spaces {
		managed := aws.ToString(space.Description) == "Managed by the LaunchDarkly CLI"
		if managed && strings.HasSuffix(aws.ToString(in.ResourceArn), ":agentspace/"+aws.ToString(space.AgentSpaceId)) {
			return &devopsagent.ListTagsForResourceOutput{
				Tags: map[string]string{awsdevops.ManagedTagKey: awsdevops.ManagedTagValue},
			}, nil
		}
	}

	return &devopsagent.ListTagsForResourceOutput{}, nil
}

func (s stubAgent) EnableOperatorApp(context.Context, *devopsagent.EnableOperatorAppInput, ...func(*devopsagent.Options)) (*devopsagent.EnableOperatorAppOutput, error) {
	return nil, nil
}

func (s stubAgent) GetAgentSpace(_ context.Context, in *devopsagent.GetAgentSpaceInput, _ ...func(*devopsagent.Options)) (*devopsagent.GetAgentSpaceOutput, error) {
	if s.missing[aws.ToString(in.AgentSpaceId)] {
		return nil, &agenttypes.ResourceNotFoundException{}
	}
	for _, space := range s.spaces {
		if aws.ToString(space.AgentSpaceId) == aws.ToString(in.AgentSpaceId) {
			return &devopsagent.GetAgentSpaceOutput{AgentSpace: &space}, nil
		}
	}

	return &devopsagent.GetAgentSpaceOutput{AgentSpace: &agenttypes.AgentSpace{AgentSpaceId: in.AgentSpaceId}}, nil
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
	return awsdevops.Clients{Agent: stubAgent{spaces: spaces}, STS: accountSTS{}, Region: "us-east-1"}
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

// failingList fails the space list, as a missing permission on another
// space would.
type failingList struct{ stubAgent }

func (failingList) ListAgentSpaces(context.Context, *devopsagent.ListAgentSpacesInput, ...func(*devopsagent.Options)) (*devopsagent.ListAgentSpacesOutput, error) {
	return nil, errors.New("AccessDeniedException")
}

// --agent-space-id reads only that space.
func TestResolveAgentSpaceIDSkipsTheList(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{AgentSpaceID: "space-1"}
	clients := newStubClients(ldcliSpace("space-1", "launchdarkly"))
	clients.Agent = failingList{clients.Agent.(stubAgent)}

	provision, err := resolveAgentSpace(cmd, clients, &opts, false)

	require.NoError(t, err)
	assert.True(t, provision)
}

// unreadSpace is a space whose tags setup could not read.
func unreadSpace(id, name string) awsdevops.AgentSpaceSummary {
	return awsdevops.AgentSpaceSummary{AgentSpaceID: id, Name: name, TagErr: errors.New("InternalServerException")}
}

// A space with an unknown owner is not a choice, and a note names it.
func TestSelectAgentSpaceLeavesOutASpaceWithUnreadTags(t *testing.T) {
	cmd, out := testCmd("2\n")
	spaces := append(testSpaces(), unreadSpace("space-3", "sandbox"))

	chosen, err := selectAgentSpace(cmd, spaces, "us-east-1", "")
	require.NoError(t, err)

	assert.Equal(t, spaceChoice{id: "space-2"}, chosen)
	assert.Contains(t, out.String(), "3) provision a new agent space\n")
	assert.NotContains(t, out.String(), "sandbox (space-3)")
	assert.Contains(t, out.String(),
		"1 agent space is not listed, because setup could not read the tags: sandbox space-3. "+
			"To use one, pass --agent-space-id <id>.")
}

func TestSelectAgentSpaceCountsEachSpaceWithUnreadTags(t *testing.T) {
	cmd, out := testCmd("1\n")
	spaces := append(testSpaces(), unreadSpace("space-3", "sandbox"), unreadSpace("space-4", "old"))

	_, err := selectAgentSpace(cmd, spaces, "us-east-1", "")
	require.NoError(t, err)

	assert.Contains(t, out.String(), "2 agent spaces are not listed, because setup could not read the tags: sandbox space-3, old space-4.")
}

// The name of a hidden space is not free, and it is not a default.
func TestSelectAgentSpaceDoesNotOfferTheNameOfAHiddenSpace(t *testing.T) {
	cmd, out := testCmd("1\n")
	spaces := append(testSpaces(), unreadSpace("space-3", "sandbox"))

	_, err := selectAgentSpace(cmd, spaces, "us-east-1", "Sandbox")
	require.NoError(t, err)

	assert.NotContains(t, out.String(), "named Sandbox")
	assert.NotContains(t, out.String(), "[")
}

// A hidden space with the same name makes the name ambiguous.
func TestSelectAgentSpaceHasNoDefaultIfAHiddenSpaceSharesTheName(t *testing.T) {
	cmd, out := testCmd("1\n")
	spaces := append(testSpaces(), unreadSpace("space-3", "platform"))

	_, err := selectAgentSpace(cmd, spaces, "us-east-1", "platform")
	require.NoError(t, err)

	assert.NotContains(t, out.String(), "[")
}

func TestPickWithoutAskingStopsAroundAHiddenSpace(t *testing.T) {
	cases := map[string]string{
		"without a name":             "",
		"with the hidden space name": "sandbox",
	}
	for name, spaceName := range cases {
		t.Run(name, func(t *testing.T) {
			spaces := []awsdevops.AgentSpaceSummary{
				{AgentSpaceID: "space-1", Name: "launchdarkly", ManagedByLDCLI: true},
				unreadSpace("space-3", "sandbox"),
			}
			opts := awsdevops.SetupOptions{AgentSpaceName: spaceName}

			_, err := pickWithoutAsking(spaces, &opts, "us-east-1")

			require.Error(t, err)
			assert.ErrorContains(t, err, "could not read the tags")
			assert.ErrorContains(t, err, "sandbox space-3")
			assert.ErrorContains(t, err, "--agent-space-id")
			assert.Empty(t, opts.AgentSpaceID)
			assert.False(t, opts.NewAgentSpace)
		})
	}
}

// A name that no hidden space has is still safe to use.
func TestPickWithoutAskingUsesANameBesideAHiddenSpace(t *testing.T) {
	spaces := []awsdevops.AgentSpaceSummary{
		{AgentSpaceID: "space-1", Name: "launchdarkly", ManagedByLDCLI: true},
		unreadSpace("space-3", "sandbox"),
	}

	opts := awsdevops.SetupOptions{AgentSpaceName: "launchdarkly"}
	provision, err := pickWithoutAsking(spaces, &opts, "us-east-1")
	require.NoError(t, err)
	assert.True(t, provision)
	assert.Equal(t, "space-1", opts.AgentSpaceID)

	opts = awsdevops.SetupOptions{AgentSpaceName: "new-space"}
	provision, err = pickWithoutAsking(spaces, &opts, "us-east-1")
	require.NoError(t, err)
	assert.True(t, provision)
	assert.True(t, opts.NewAgentSpace)
}

// One failed tag lookup leaves the other spaces in the list.
func TestResolveAgentSpaceListsTheSpacesWithReadTags(t *testing.T) {
	cmd, out := testCmd("1\n")
	opts := awsdevops.SetupOptions{}
	clients := awsdevops.Clients{
		Agent: stubAgent{
			spaces: []agenttypes.AgentSpace{
				ldcliSpace("space-1", "launchdarkly"),
				{AgentSpaceId: aws.String("space-2"), Name: aws.String("platform")},
			},
			unreadTags: map[string]bool{"space-2": true},
		},
		STS:    accountSTS{},
		Region: "us-east-1",
	}

	provision, err := resolveAgentSpace(cmd, clients, &opts, true)
	require.NoError(t, err)

	assert.True(t, provision)
	assert.Equal(t, "space-1", opts.AgentSpaceID)
	assert.Contains(t, out.String(), "1) launchdarkly (space-1), made by ldcli")
	assert.Contains(t, out.String(), "1 agent space is not listed")
	assert.Contains(t, out.String(), "platform space-2")
}

// If every lookup fails, setup stops.
func TestResolveAgentSpaceFailsIfNoTagCanBeRead(t *testing.T) {
	cmd, _ := testCmd("1\n")
	opts := awsdevops.SetupOptions{}
	clients := awsdevops.Clients{
		Agent: stubAgent{
			spaces:     []agenttypes.AgentSpace{{AgentSpaceId: aws.String("space-2"), Name: aws.String("platform")}},
			unreadTags: map[string]bool{"space-2": true},
		},
		STS:    accountSTS{},
		Region: "us-east-1",
	}

	_, err := resolveAgentSpace(cmd, clients, &opts, true)

	assert.ErrorContains(t, err, "unable to read the tags of agent space space-2")
}

// Without a name, Enter picks the one space that setup made.
func TestSelectAgentSpaceWithoutANameDefaultsToTheSpaceSetupMade(t *testing.T) {
	cmd, out := testCmd("\n")
	spaces := []awsdevops.AgentSpaceSummary{
		{AgentSpaceID: "space-1", Name: "platform"},
		{AgentSpaceID: "space-2", Name: "launchdarkly", ManagedByLDCLI: true},
	}

	chosen, err := selectAgentSpace(cmd, spaces, "us-east-1", "")
	require.NoError(t, err)

	assert.Equal(t, spaceChoice{id: "space-2"}, chosen)
	assert.Contains(t, out.String(), "connect to? [2] ")
}

// Two spaces that setup made, or a hidden space, leave no default.
func TestSelectAgentSpaceWithoutANameHasNoDefaultWhenItIsNotClear(t *testing.T) {
	cases := map[string][]awsdevops.AgentSpaceSummary{
		"two spaces setup made": {
			{AgentSpaceID: "space-1", Name: "a", ManagedByLDCLI: true},
			{AgentSpaceID: "space-2", Name: "b", ManagedByLDCLI: true},
		},
		"a hidden space": {
			{AgentSpaceID: "space-1", Name: "a", ManagedByLDCLI: true},
			unreadSpace("space-3", "sandbox"),
		},
	}
	for name, spaces := range cases {
		t.Run(name, func(t *testing.T) {
			cmd, out := testCmd("1\n")

			_, err := selectAgentSpace(cmd, spaces, "us-east-1", "")
			require.NoError(t, err)

			assert.NotContains(t, out.String(), "[")
		})
	}
}

func TestResolveAgentSpaceIDNamesAMissingSpace(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{AgentSpaceID: "space-typo"}
	clients := awsdevops.Clients{Agent: stubAgent{missing: map[string]bool{"space-typo": true}}, STS: accountSTS{}, Region: "us-east-1"}

	_, err := resolveAgentSpace(cmd, clients, &opts, false)

	assert.EqualError(t, err, "no agent space space-typo in us-east-1. Check --region and --profile, "+
		"or run setup without --agent-space-id to list the agent spaces")
}

// The list tells the user to pass --agent-space-id for a hidden space, so that
// works: the owner is unknown, so setup only connects the MCP server.
func TestResolveAgentSpaceIDWithUnreadTagsConnectsOnly(t *testing.T) {
	cmd, out := testCmd("")
	opts := awsdevops.SetupOptions{AgentSpaceID: "space-2"}
	clients := awsdevops.Clients{
		Agent: stubAgent{
			spaces:     []agenttypes.AgentSpace{ldcliSpace("space-2", "launchdarkly")},
			unreadTags: map[string]bool{"space-2": true},
		},
		STS:    accountSTS{},
		Region: "us-east-1",
	}

	provision, err := resolveAgentSpace(cmd, clients, &opts, false)

	require.NoError(t, err)
	assert.False(t, provision)
	assert.Contains(t, out.String(), "could not read the tags of agent space launchdarkly (space-2), so it only connects the MCP server")
	assert.Contains(t, out.String(), "--auth-flow")
}

// --auth-flow forces the full setup on a space that setup did not make, and
// setup warns about it before the plan.
func TestResolveAgentSpaceWarnsAboutAuthFlowOnAnotherSpace(t *testing.T) {
	for name, space := range map[string]agenttypes.AgentSpace{
		"made by someone else": {AgentSpaceId: aws.String("space-2"), Name: aws.String("platform")},
		"made by setup":        ldcliSpace("space-2", "platform"),
	} {
		t.Run(name, func(t *testing.T) {
			cmd, out := testCmd("")
			opts := awsdevops.SetupOptions{AgentSpaceID: "space-2", AuthFlowSet: true}

			provision, err := resolveAgentSpace(cmd, newStubClients(space), &opts, false)

			require.NoError(t, err)
			assert.True(t, provision)
			warned := strings.Contains(out.String(), "Warning: setup did not make agent space platform (space-2)")
			assert.Equal(t, name == "made by someone else", warned)
		})
	}
}

func TestNameNewSpaceGivesAnExampleName(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{}

	err := nameNewSpace(cmd, nil, &opts, false)

	assert.EqualError(t, err, "a new agent space needs a name; pass --agent-space-name, for example --agent-space-name launchdarkly")
}

// A new space needs only a free name, so a failed tag lookup does not stop it.
func TestResolveAgentSpaceNewSpaceDoesNotReadTags(t *testing.T) {
	cmd, _ := testCmd("")
	opts := awsdevops.SetupOptions{NewAgentSpace: true, AgentSpaceName: "fresh"}
	clients := awsdevops.Clients{
		Agent: stubAgent{
			spaces:     []agenttypes.AgentSpace{{AgentSpaceId: aws.String("space-2"), Name: aws.String("platform")}},
			unreadTags: map[string]bool{"space-2": true},
		},
		STS:    accountSTS{},
		Region: "us-east-1",
	}

	provision, err := resolveAgentSpace(cmd, clients, &opts, false)

	require.NoError(t, err)
	assert.True(t, provision)
	assert.Equal(t, "fresh", opts.AgentSpaceName)
}

// The name check still sees each space.
func TestResolveAgentSpaceNewSpaceAsksAgainForAUsedName(t *testing.T) {
	cmd, out := testCmd("platform\nfresh\n")
	opts := awsdevops.SetupOptions{NewAgentSpace: true}
	clients := newStubClients(agenttypes.AgentSpace{AgentSpaceId: aws.String("space-2"), Name: aws.String("platform")})

	_, err := resolveAgentSpace(cmd, clients, &opts, true)

	require.NoError(t, err)
	assert.Equal(t, "fresh", opts.AgentSpaceName)
	assert.Contains(t, out.String(), "An agent space named platform already exists")
}
