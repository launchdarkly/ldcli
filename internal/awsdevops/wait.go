package awsdevops

import (
	"context"
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
		serviceID, err := findGitHubService(ctx, clients)
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

func findGitHubService(ctx context.Context, clients Clients) (string, error) {
	services, err := clients.Agent.ListServices(ctx, &devopsagent.ListServicesInput{
		FilterServiceType: agenttypes.ServiceGithub,
	})
	if err != nil {
		return "", fmt.Errorf("unable to list registered services: %w", err)
	}
	for _, service := range services.Services {
		if service.ServiceType == agenttypes.ServiceGithub {
			return aws.ToString(service.ServiceId), nil
		}
	}

	return "", nil
}
