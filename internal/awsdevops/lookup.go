package awsdevops

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
)

// FindMCPServer returns the ID of the LaunchDarkly MCP server if one is
// already registered on the account. AWS keeps MCP servers at the account
// level, so a second agent space reuses the existing registration.
func FindMCPServer(ctx context.Context, clients Clients) (string, error) {
	var nextToken *string
	for {
		services, err := clients.Agent.ListServices(ctx, &devopsagent.ListServicesInput{NextToken: nextToken})
		if err != nil {
			return "", fmt.Errorf("unable to list registered services: %w", err)
		}
		for _, service := range services.Services {
			if isLaunchDarklyMCPServer(service) {
				return aws.ToString(service.ServiceId), nil
			}
		}
		if services.NextToken == nil {
			return "", nil
		}
		nextToken = services.NextToken
	}
}

// FindAgentSpace returns the ID of the agent space with the given name, or an
// empty string when the account has none.
func FindAgentSpace(ctx context.Context, clients Clients, name string) (string, error) {
	var nextToken *string
	for {
		spaces, err := clients.Agent.ListAgentSpaces(ctx, &devopsagent.ListAgentSpacesInput{NextToken: nextToken})
		if err != nil {
			return "", fmt.Errorf("unable to list agent spaces: %w", err)
		}
		for _, space := range spaces.AgentSpaces {
			if strings.EqualFold(aws.ToString(space.Name), name) {
				return aws.ToString(space.AgentSpaceId), nil
			}
		}
		if spaces.NextToken == nil {
			return "", nil
		}
		nextToken = spaces.NextToken
	}
}

// FindAssociation returns the ID of an existing association between an agent
// space and a service, so setup can be re-run without AWS rejecting duplicates.
func FindAssociation(ctx context.Context, clients Clients, agentSpaceID, serviceID string) (string, error) {
	var nextToken *string
	for {
		associations, err := clients.Agent.ListAssociations(ctx, &devopsagent.ListAssociationsInput{
			AgentSpaceId: aws.String(agentSpaceID),
			NextToken:    nextToken,
		})
		if err != nil {
			return "", fmt.Errorf("unable to list associations for agent space %s: %w", agentSpaceID, err)
		}
		for _, association := range associations.Associations {
			if aws.ToString(association.ServiceId) == serviceID {
				return aws.ToString(association.AssociationId), nil
			}
		}
		if associations.NextToken == nil {
			return "", nil
		}
		nextToken = associations.NextToken
	}
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
