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

// FindMCPServer returns the ID of the LaunchDarkly MCP server if one is
// already registered on the account. AWS keeps MCP servers at the account
// level, so a second agent space reuses the existing registration.
func FindMCPServer(ctx context.Context, clients Clients) (string, error) {
	services, err := listServices(ctx, clients)
	if err != nil {
		return "", err
	}
	for _, service := range services {
		if isLaunchDarklyMCPServer(service) {
			return aws.ToString(service.ServiceId), nil
		}
	}

	return "", nil
}

// FindAgentSpace returns the ID of the agent space with the given name, or an
// empty string when the account has none.
func FindAgentSpace(ctx context.Context, clients Clients, name string) (string, error) {
	spaces, err := listAgentSpaces(ctx, clients)
	if err != nil {
		return "", err
	}
	for _, space := range spaces {
		if strings.EqualFold(aws.ToString(space.Name), name) {
			return aws.ToString(space.AgentSpaceId), nil
		}
	}

	return "", nil
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

// isLaunchDarklyMCPServer matches on the name AWS reports at either level, and
// on the endpoint, since MCP servers carry their name in the type-specific
// details rather than the top-level field.
func isLaunchDarklyMCPServer(service agenttypes.RegisteredService) bool {
	if strings.EqualFold(aws.ToString(service.Name), MCPServerName) {
		return true
	}

	details, ok := service.AdditionalServiceDetails.(*agenttypes.AdditionalServiceDetailsMemberMcpserver)
	if !ok {
		return false
	}

	return strings.EqualFold(aws.ToString(details.Value.Name), MCPServerName) ||
		aws.ToString(details.Value.Endpoint) == MCPServerEndpoint
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
