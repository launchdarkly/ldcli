package awsdevops

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
)

// The DevOps Agent list APIs page at 100 items, so every caller has to follow
// NextToken: a partial list hides resources that already exist.

func listServices(ctx context.Context, clients Clients) ([]agenttypes.RegisteredService, error) {
	var (
		services  []agenttypes.RegisteredService
		nextToken *string
	)
	for {
		page, err := clients.Agent.ListServices(ctx, &devopsagent.ListServicesInput{NextToken: nextToken})
		if err != nil {
			return nil, fmt.Errorf("unable to list registered services: %w", err)
		}
		services = append(services, page.Services...)
		if page.NextToken == nil {
			return services, nil
		}
		nextToken = page.NextToken
	}
}

func listAgentSpaces(ctx context.Context, clients Clients) ([]agenttypes.AgentSpace, error) {
	var (
		spaces    []agenttypes.AgentSpace
		nextToken *string
	)
	for {
		page, err := clients.Agent.ListAgentSpaces(ctx, &devopsagent.ListAgentSpacesInput{NextToken: nextToken})
		if err != nil {
			return nil, fmt.Errorf("unable to list agent spaces: %w", err)
		}
		spaces = append(spaces, page.AgentSpaces...)
		if page.NextToken == nil {
			return spaces, nil
		}
		nextToken = page.NextToken
	}
}

func listAssociations(ctx context.Context, clients Clients, agentSpaceID string) ([]agenttypes.Association, error) {
	var (
		associations []agenttypes.Association
		nextToken    *string
	)
	for {
		page, err := clients.Agent.ListAssociations(ctx, &devopsagent.ListAssociationsInput{
			AgentSpaceId: aws.String(agentSpaceID),
			NextToken:    nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("unable to list associations for agent space %s: %w", agentSpaceID, err)
		}
		associations = append(associations, page.Associations...)
		if page.NextToken == nil {
			return associations, nil
		}
		nextToken = page.NextToken
	}
}

// AgentSpaceSummary identifies an agent space the account already has, for a
// caller that has to pick one.
type AgentSpaceSummary struct {
	AgentSpaceID string `json:"agentSpaceId"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	// ManagedByLDCLI means that setup made the space.
	ManagedByLDCLI bool `json:"managedByLdcli"`
	// TagErr is set if setup could not read the tags of the space. The owner
	// of the space is then unknown, and ManagedByLDCLI is false.
	TagErr error `json:"-"`
}

// AgentSpaces lists the agent spaces in the account and region. A space that
// setup made has the ldcli:managed tag. A user can change the description, so
// only the tag counts. Each space costs one tag lookup.
//
// If a tag lookup fails, the summary of that space has TagErr, and the other
// spaces stay in the list. Setup uses the tag to decide if it can change the
// roles and the operator app of a space, so a caller must not use a space with
// TagErr as if its owner is known. If every lookup fails, for example without
// aidevops:ListTagsForResource, AgentSpaces returns the first error. The
// status command does not call AgentSpaces.
func AgentSpaces(ctx context.Context, clients Clients) ([]AgentSpaceSummary, error) {
	spaces, err := listAgentSpaces(ctx, clients)
	if err != nil || len(spaces) == 0 {
		return nil, err
	}
	accountID, err := clients.AccountID(ctx)
	if err != nil {
		return nil, err
	}

	summaries := make([]AgentSpaceSummary, 0, len(spaces))
	var firstErr error
	failed := 0
	for _, space := range spaces {
		summary := AgentSpaceSummary{
			AgentSpaceID: aws.ToString(space.AgentSpaceId),
			Name:         aws.ToString(space.Name),
			Description:  aws.ToString(space.Description),
		}
		summary.ManagedByLDCLI, summary.TagErr = hasManagedTag(ctx, clients, accountID, summary.AgentSpaceID)
		if summary.TagErr != nil {
			failed++
			if firstErr == nil {
				firstErr = summary.TagErr
			}
		}
		summaries = append(summaries, summary)
	}
	if failed == len(summaries) {
		return nil, firstErr
	}

	return summaries, nil
}

// AgentSpaceNames lists the agent spaces without their tags, for a caller
// that needs only the names, such as a check that a new name is free. It
// makes no tag lookups, so ManagedByLDCLI is always false. A caller must not
// use these summaries to decide who made a space.
func AgentSpaceNames(ctx context.Context, clients Clients) ([]AgentSpaceSummary, error) {
	spaces, err := listAgentSpaces(ctx, clients)
	if err != nil {
		return nil, err
	}
	summaries := make([]AgentSpaceSummary, 0, len(spaces))
	for _, space := range spaces {
		summaries = append(summaries, AgentSpaceSummary{
			AgentSpaceID: aws.ToString(space.AgentSpaceId),
			Name:         aws.ToString(space.Name),
			Description:  aws.ToString(space.Description),
		})
	}

	return summaries, nil
}

// AgentSpace reads one agent space, with the same ownership check as
// AgentSpaces, for a caller that already has the space ID. If setup cannot
// read the tags, the summary has TagErr.
func AgentSpace(ctx context.Context, clients Clients, spaceID string) (AgentSpaceSummary, error) {
	out, err := clients.Agent.GetAgentSpace(ctx, &devopsagent.GetAgentSpaceInput{AgentSpaceId: aws.String(spaceID)})
	if err != nil {
		return AgentSpaceSummary{}, fmt.Errorf("unable to read agent space %s: %w", spaceID, err)
	}
	accountID, err := clients.AccountID(ctx)
	if err != nil {
		return AgentSpaceSummary{}, err
	}
	summary := AgentSpaceSummary{
		AgentSpaceID: spaceID,
		Name:         aws.ToString(out.AgentSpace.Name),
		Description:  aws.ToString(out.AgentSpace.Description),
	}
	// A tag error goes on the summary, as in AgentSpaces. The caller decides
	// what to do with a space whose owner is unknown.
	summary.ManagedByLDCLI, summary.TagErr = hasManagedTag(ctx, clients, accountID, spaceID)

	return summary, nil
}

// hasManagedTag reports the ldcli:managed tag on an agent space.
func hasManagedTag(ctx context.Context, clients Clients, accountID, spaceID string) (bool, error) {
	out, err := clients.Agent.ListTagsForResource(ctx, &devopsagent.ListTagsForResourceInput{
		ResourceArn: aws.String(agentSpaceARN(clients.Region, accountID, spaceID)),
	})
	// The SDK retries a throttle or a server error before it returns. Only an
	// access error points to the permission.
	if err != nil && errorCode(err) == "AccessDeniedException" {
		return false, fmt.Errorf(
			"unable to read the tags of agent space %s. Setup needs aidevops:ListTagsForResource: %w", spaceID, err)
	}
	if err != nil {
		return false, fmt.Errorf("unable to read the tags of agent space %s: %w", spaceID, err)
	}

	return out.Tags[ManagedTagKey] == ManagedTagValue, nil
}

// agentSpaceARN builds the ARN that the tag API needs. An agent space does not
// report its ARN.
func agentSpaceARN(region, accountID, spaceID string) string {
	return fmt.Sprintf("arn:aws:aidevops:%s:%s:agentspace/%s", region, accountID, spaceID)
}

// accountMCPServer finds the MCP server of the AWS account: a server at
// endpoint that signs in with OAuth. A server with a token is never a match,
// even with the name MCPServerName, because its token belongs to one
// LaunchDarkly account and not to the AWS account.
func accountMCPServer(ctx context.Context, clients Clients, endpoint string) (mcpRef, error) {
	services, err := listServices(ctx, clients)
	if err != nil {
		return mcpRef{}, err
	}
	for _, service := range services {
		if mcpEndpointOf(service) == endpoint && usesOAuth(service) {
			return mcpRef{ID: aws.ToString(service.ServiceId), Name: serviceName(service)}, nil
		}
	}

	return mcpRef{}, nil
}

func usesOAuth(service agenttypes.RegisteredService) bool {
	details, ok := service.AdditionalServiceDetails.(*agenttypes.AdditionalServiceDetailsMemberMcpserver)

	return ok && details.Value.AuthorizationMethod == agenttypes.MCPServerAuthorizationMethodOauth3lo
}

// mcpRef identifies a registered MCP server.
type mcpRef struct {
	ID   string
	Name string
}

// linkedMCPServer finds the LaunchDarkly MCP server at endpoint that an agent
// space is connected to. Setup keeps that server, so a space that someone
// connected by hand keeps its server. A space connected to more than one
// server is an error, because setup cannot tell which one to use.
func linkedMCPServer(ctx context.Context, clients Clients, agentSpaceID, endpoint string) (mcpRef, error) {
	// A new space has no ID and no server yet.
	if agentSpaceID == "" {
		return mcpRef{}, nil
	}
	associations, err := listAssociations(ctx, clients, agentSpaceID)
	if err != nil {
		return mcpRef{}, err
	}
	links := make(map[string]bool, len(associations))
	for _, association := range associations {
		links[aws.ToString(association.ServiceId)] = true
	}
	services, err := listServices(ctx, clients)
	if err != nil {
		return mcpRef{}, err
	}
	var (
		linked mcpRef
		all    []string
	)
	for _, service := range services {
		ref := mcpRef{ID: aws.ToString(service.ServiceId), Name: serviceName(service)}
		if mcpEndpointOf(service) != endpoint || !links[ref.ID] {
			continue
		}
		linked = ref
		all = append(all, fmt.Sprintf("%s (%s)", ref.Name, ref.ID))
	}
	if len(all) > 1 {
		return mcpRef{}, fmt.Errorf(
			"agent space %s is connected to %d LaunchDarkly MCP servers: %s. "+
				"Disconnect all but one in the AWS console, then run setup again",
			agentSpaceID, len(all), strings.Join(all, ", "),
		)
	}

	return linked, nil
}

// spacesNamed lists IDs of spaces named name.
func spacesNamed(ctx context.Context, clients Clients, name string) ([]string, error) {
	// An empty name names no space, even a space without a name.
	if name == "" {
		return nil, nil
	}
	spaces, err := listAgentSpaces(ctx, clients)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, space := range spaces {
		if strings.EqualFold(aws.ToString(space.Name), name) {
			ids = append(ids, aws.ToString(space.AgentSpaceId))
		}
	}

	return ids, nil
}

// FindAssociation returns the ID of an existing association between an agent
// space and a service, so setup can be re-run without AWS rejecting duplicates.
func FindAssociation(ctx context.Context, clients Clients, agentSpaceID, serviceID string) (string, error) {
	associations, err := listAssociations(ctx, clients, agentSpaceID)
	if err != nil {
		return "", err
	}
	for _, association := range associations {
		if aws.ToString(association.ServiceId) == serviceID {
			return aws.ToString(association.AssociationId), nil
		}
	}

	return "", nil
}

// FindAWSAssociation returns the ID of the association between an agent space
// and its own AWS account. An agent space can also read other AWS accounts.
// Those associations have the service ID "aws" too, so the match uses the
// monitor configuration and the account ID.
func FindAWSAssociation(ctx context.Context, clients Clients, agentSpaceID, accountID string) (string, error) {
	associations, err := listAssociations(ctx, clients, agentSpaceID)
	if err != nil {
		return "", err
	}
	for _, association := range associations {
		if aws.ToString(association.ServiceId) != awsServiceID {
			continue
		}
		id, kind := associationAccount(association.Configuration)
		if kind == string(agenttypes.MonitorAccountTypeMonitor) && id == accountID {
			return aws.ToString(association.AssociationId), nil
		}
	}

	return "", nil
}

// mcpEndpointOf returns the endpoint a registered MCP server serves, which is
// what identifies a registration: the name is the customer's to choose, so a
// self-hosted LaunchDarkly MCP server can carry ours.
func mcpEndpointOf(service agenttypes.RegisteredService) string {
	details, ok := service.AdditionalServiceDetails.(*agenttypes.AdditionalServiceDetailsMemberMcpserver)
	if !ok {
		return ""
	}

	return aws.ToString(details.Value.Endpoint)
}

// serviceName prefers the name AWS keeps in the type-specific details, which is
// where MCP servers carry theirs.
func serviceName(service agenttypes.RegisteredService) string {
	if details, ok := service.AdditionalServiceDetails.(*agenttypes.AdditionalServiceDetailsMemberMcpserver); ok {
		if name := aws.ToString(details.Value.Name); name != "" {
			return name
		}
	}

	return aws.ToString(service.Name)
}
