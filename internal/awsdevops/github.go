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

// GitHubRepo names the repository the agent dispatches workflows against.
type GitHubRepo struct {
	Owner string
	Name  string
	// ID is the numeric GitHub repository ID, as a string.
	ID string
	// Organization is true when an organization owns the repository.
	Organization bool
}

// FullName is owner/name.
func (r GitHubRepo) FullName() string {
	return r.Owner + "/" + r.Name
}

// GitHubRegistration finds the account-level GitHub registration for owner.
// It returns an empty ID when the account has none. AWS registers GitHub only
// through the console, where the AWS DevOps Agent GitHub App is installed, so
// setup can find a registration but cannot make one.
func GitHubRegistration(ctx context.Context, clients Clients, owner string) (string, error) {
	services, err := listServices(ctx, clients)
	if err != nil {
		return "", err
	}
	for _, service := range services {
		if service.ServiceType != agenttypes.ServiceGithub {
			continue
		}
		details, ok := service.AdditionalServiceDetails.(*agenttypes.AdditionalServiceDetailsMemberGithub)
		if !ok {
			continue
		}
		if strings.EqualFold(aws.ToString(details.Value.Owner), owner) {
			return aws.ToString(service.ServiceId), nil
		}
	}

	return "", nil
}

// WaitForGitHubRegistration polls until the GitHub registration for owner
// appears, while the person installs the GitHub App in the console.
func WaitForGitHubRegistration(ctx context.Context, clients Clients, owner string, logf func(string, ...any)) (string, error) {
	logf("Waiting for the GitHub registration for %s to appear in AWS", owner)
	deadline := time.Now().Add(oauthTimeout)
	for {
		id, err := GitHubRegistration(ctx, clients, owner)
		if err != nil {
			return "", err
		}
		if id != "" {
			return id, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf(
				"AWS has no GitHub registration for %s after %s. Finish installing the GitHub App in the console, then run setup again",
				owner, oauthTimeout,
			)
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(oauthPollInterval):
		}
	}
}

// ConnectGitHubRepo associates the repository with the agent space. It
// returns the association ID and whether the space already had the repo.
func ConnectGitHubRepo(ctx context.Context, clients Clients, agentSpaceID, serviceID string, repo GitHubRepo) (string, bool, error) {
	associations, err := listAssociations(ctx, clients, agentSpaceID)
	if err != nil {
		return "", false, err
	}
	for _, association := range associations {
		if aws.ToString(association.ServiceId) != serviceID {
			continue
		}
		config, ok := association.Configuration.(*agenttypes.ServiceConfigurationMemberGithub)
		if ok && strings.EqualFold(aws.ToString(config.Value.RepoName), repo.Name) &&
			strings.EqualFold(aws.ToString(config.Value.Owner), repo.Owner) {
			return aws.ToString(association.AssociationId), true, nil
		}
	}

	ownerType := agenttypes.GithubRepoOwnerTypeUser
	if repo.Organization {
		ownerType = agenttypes.GithubRepoOwnerTypeOrganization
	}
	out, err := clients.Agent.AssociateService(ctx, &devopsagent.AssociateServiceInput{
		AgentSpaceId: aws.String(agentSpaceID),
		ServiceId:    aws.String(serviceID),
		Configuration: &agenttypes.ServiceConfigurationMemberGithub{
			Value: agenttypes.GitHubConfiguration{
				Owner:     aws.String(repo.Owner),
				OwnerType: ownerType,
				RepoId:    aws.String(repo.ID),
				RepoName:  aws.String(repo.Name),
			},
		},
	})
	if err != nil {
		return "", false, fmt.Errorf("unable to connect %s to agent space %s: %w", repo.FullName(), agentSpaceID, err)
	}

	return aws.ToString(out.Association.AssociationId), false, nil
}
