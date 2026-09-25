package awsdevops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
)

const defaultPollInterval = 5 * time.Second

// WaitForGitHubService polls until a GitHub service is registered on the
// account and returns its ID. Registration is a browser consent flow, so this
// is how the CLI picks up what the operator did in the console.
func WaitForGitHubService(ctx context.Context, clients Clients, interval time.Duration) (string, error) {
	if interval <= 0 {
		interval = defaultPollInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		serviceID, err := FindGitHubService(ctx, clients)
		if err != nil {
			return "", err
		}
		if serviceID != "" {
			return serviceID, nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

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

// FindAsset returns the ID of an asset with the given type and metadata name
// in an agent space, so setup can skip recreating one it already made.
func FindAsset(ctx context.Context, clients Clients, agentSpaceID, assetType, name string) (string, error) {
	var nextToken *string
	for {
		assets, err := clients.Agent.ListAssets(ctx, &devopsagent.ListAssetsInput{
			AgentSpaceId: aws.String(agentSpaceID),
			NextToken:    nextToken,
		})
		if err != nil {
			return "", fmt.Errorf("unable to list assets for agent space %s: %w", agentSpaceID, err)
		}
		for _, asset := range assets.Items {
			if aws.ToString(asset.AssetType) != assetType {
				continue
			}
			if strings.EqualFold(assetName(asset), name) {
				return aws.ToString(asset.AssetId), nil
			}
		}
		if assets.NextToken == nil {
			return "", nil
		}
		nextToken = assets.NextToken
	}
}

// assetName pulls the name AWS keeps in the asset's metadata document. The
// document type's own unmarshal reports a spurious "unsupported json type"
// error while still populating the destination, so the JSON bytes are read
// directly instead.
func assetName(asset agenttypes.Asset) string {
	if asset.Metadata == nil {
		return ""
	}
	raw, err := asset.Metadata.MarshalSmithyDocument()
	if err != nil {
		return ""
	}
	var meta struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ""
	}

	return meta.Name
}

// FindGitHubService returns the ID of the GitHub registration on the account,
// which only a browser consent flow can create.
func FindGitHubService(ctx context.Context, clients Clients) (string, error) {
	var nextToken *string
	for {
		services, err := clients.Agent.ListServices(ctx, &devopsagent.ListServicesInput{NextToken: nextToken})
		if err != nil {
			return "", fmt.Errorf("unable to list registered services: %w", err)
		}
		for _, service := range services.Services {
			if service.ServiceType == agenttypes.ServiceGithub {
				return aws.ToString(service.ServiceId), nil
			}
		}
		if services.NextToken == nil {
			return "", nil
		}
		nextToken = services.NextToken
	}
}
