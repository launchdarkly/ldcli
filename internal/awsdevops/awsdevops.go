// Package awsdevops provisions the AWS DevOps Agent resources that let the agent
// operate against a LaunchDarkly account. Every call is made with the caller's own
// AWS credentials, so the customer must authenticate before invoking the CLI.
package awsdevops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// SupportedRegions are the regions where the AWS DevOps Agent control plane is available.
var SupportedRegions = []string{
	"us-east-1",
	"us-west-2",
	"ap-southeast-2",
	"ap-northeast-1",
	"eu-central-1",
	"eu-west-1",
}

var (
	ErrNoCredentials = errors.New("no AWS credentials found. Authenticate first (for example `aws sso login --profile my-profile` or exporting AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY and AWS_SESSION_TOKEN), then re-run this command")
	ErrNoRegion      = errors.New("no AWS region found. Set AWS_REGION or pass --region")
)

// AgentAPI is the subset of the AWS DevOps Agent API this package uses.
type AgentAPI interface {
	AssociateService(context.Context, *devopsagent.AssociateServiceInput, ...func(*devopsagent.Options)) (*devopsagent.AssociateServiceOutput, error)
	CreateAgentSpace(context.Context, *devopsagent.CreateAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.CreateAgentSpaceOutput, error)
	CreateAsset(context.Context, *devopsagent.CreateAssetInput, ...func(*devopsagent.Options)) (*devopsagent.CreateAssetOutput, error)
	CreateTrigger(context.Context, *devopsagent.CreateTriggerInput, ...func(*devopsagent.Options)) (*devopsagent.CreateTriggerOutput, error)
	DeleteAgentSpace(context.Context, *devopsagent.DeleteAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.DeleteAgentSpaceOutput, error)
	DeleteAsset(context.Context, *devopsagent.DeleteAssetInput, ...func(*devopsagent.Options)) (*devopsagent.DeleteAssetOutput, error)
	DeregisterService(context.Context, *devopsagent.DeregisterServiceInput, ...func(*devopsagent.Options)) (*devopsagent.DeregisterServiceOutput, error)
	DisassociateService(context.Context, *devopsagent.DisassociateServiceInput, ...func(*devopsagent.Options)) (*devopsagent.DisassociateServiceOutput, error)
	EnableOperatorApp(context.Context, *devopsagent.EnableOperatorAppInput, ...func(*devopsagent.Options)) (*devopsagent.EnableOperatorAppOutput, error)
	GetAgentSpace(context.Context, *devopsagent.GetAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.GetAgentSpaceOutput, error)
	ListAgentSpaces(context.Context, *devopsagent.ListAgentSpacesInput, ...func(*devopsagent.Options)) (*devopsagent.ListAgentSpacesOutput, error)
	ListAssets(context.Context, *devopsagent.ListAssetsInput, ...func(*devopsagent.Options)) (*devopsagent.ListAssetsOutput, error)
	ListAssociations(context.Context, *devopsagent.ListAssociationsInput, ...func(*devopsagent.Options)) (*devopsagent.ListAssociationsOutput, error)
	ListServices(context.Context, *devopsagent.ListServicesInput, ...func(*devopsagent.Options)) (*devopsagent.ListServicesOutput, error)
	RegisterService(context.Context, *devopsagent.RegisterServiceInput, ...func(*devopsagent.Options)) (*devopsagent.RegisterServiceOutput, error)
}

// IAMAPI is the subset of the IAM API this package uses.
type IAMAPI interface {
	AttachRolePolicy(context.Context, *iam.AttachRolePolicyInput, ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
	CreateRole(context.Context, *iam.CreateRoleInput, ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	DeleteRole(context.Context, *iam.DeleteRoleInput, ...func(*iam.Options)) (*iam.DeleteRoleOutput, error)
	DeleteRolePolicy(context.Context, *iam.DeleteRolePolicyInput, ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error)
	DetachRolePolicy(context.Context, *iam.DetachRolePolicyInput, ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error)
	GetRole(context.Context, *iam.GetRoleInput, ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	PutRolePolicy(context.Context, *iam.PutRolePolicyInput, ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error)
}

// STSAPI is the subset of the STS API this package uses.
type STSAPI interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// Clients bundles the AWS clients and the resolved session details the
// provisioning steps run against.
type Clients struct {
	Agent  AgentAPI
	IAM    IAMAPI
	STS    STSAPI
	Region string
}

// NewClients builds AWS clients from the ambient credential chain: environment
// variables, shared config/credentials files, SSO caches, or instance metadata.
// It fails before any API call when the session has no credentials or no region.
func NewClients(ctx context.Context, region string) (Clients, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return Clients{}, fmt.Errorf("unable to load AWS configuration: %w", err)
	}
	if cfg.Region == "" {
		return Clients{}, ErrNoRegion
	}
	if !slices.Contains(SupportedRegions, cfg.Region) {
		return Clients{}, fmt.Errorf(
			"AWS DevOps Agent is not available in %s. Supported regions: %s",
			cfg.Region,
			strings.Join(SupportedRegions, ", "),
		)
	}

	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return Clients{}, fmt.Errorf("%w: %s", ErrNoCredentials, err)
	}
	if !creds.HasKeys() {
		return Clients{}, ErrNoCredentials
	}

	return Clients{
		Agent:  devopsagent.NewFromConfig(cfg),
		IAM:    iam.NewFromConfig(cfg),
		STS:    sts.NewFromConfig(cfg),
		Region: cfg.Region,
	}, nil
}

// AccountID returns the account the current session authenticates to.
func (c Clients) AccountID(ctx context.Context) (string, error) {
	out, err := c.STS.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("unable to identify the AWS account for the current session: %w", err)
	}
	if out.Account == nil || *out.Account == "" {
		return "", ErrNoCredentials
	}

	return *out.Account, nil
}
