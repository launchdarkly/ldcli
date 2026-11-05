// Package awsdevops provisions the AWS DevOps Agent resources that let the agent
// operate against a LaunchDarkly account. Every call is made with the caller's own
// AWS credentials, so the customer must authenticate before invoking the CLI.
package awsdevops

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
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
	"us-east-2",
	"us-west-2",
	"ca-central-1",
	"sa-east-1",
	"ap-south-1",
	"ap-southeast-1",
	"ap-southeast-2",
	"ap-northeast-1",
	"ap-northeast-2",
	"eu-central-1",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"eu-north-1",
}

var (
	// ErrNoCredentials is the sentinel for any missing-credentials failure.
	// Use errors.Is to detect it; the surfaced error message is built by
	// noCredentialsError with the detail that fits the situation (named
	// profile not usable, no profile selected, nothing configured at all).
	ErrNoCredentials = errors.New("no AWS credentials found")
	ErrNoRegion      = errors.New("no AWS region found. Set AWS_REGION or pass --region")
)

// AgentAPI is the subset of the AWS DevOps Agent API this package uses.
type AgentAPI interface {
	AssociateService(context.Context, *devopsagent.AssociateServiceInput, ...func(*devopsagent.Options)) (*devopsagent.AssociateServiceOutput, error)
	CreateAgentSpace(context.Context, *devopsagent.CreateAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.CreateAgentSpaceOutput, error)
	CreateAsset(context.Context, *devopsagent.CreateAssetInput, ...func(*devopsagent.Options)) (*devopsagent.CreateAssetOutput, error)
	EnableOperatorApp(context.Context, *devopsagent.EnableOperatorAppInput, ...func(*devopsagent.Options)) (*devopsagent.EnableOperatorAppOutput, error)
	GetOperatorApp(context.Context, *devopsagent.GetOperatorAppInput, ...func(*devopsagent.Options)) (*devopsagent.GetOperatorAppOutput, error)
	ListTagsForResource(context.Context, *devopsagent.ListTagsForResourceInput, ...func(*devopsagent.Options)) (*devopsagent.ListTagsForResourceOutput, error)
	GetAgentSpace(context.Context, *devopsagent.GetAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.GetAgentSpaceOutput, error)
	GetAssetFile(context.Context, *devopsagent.GetAssetFileInput, ...func(*devopsagent.Options)) (*devopsagent.GetAssetFileOutput, error)
	ListAgentSpaces(context.Context, *devopsagent.ListAgentSpacesInput, ...func(*devopsagent.Options)) (*devopsagent.ListAgentSpacesOutput, error)
	ListAssets(context.Context, *devopsagent.ListAssetsInput, ...func(*devopsagent.Options)) (*devopsagent.ListAssetsOutput, error)
	ListAssociations(context.Context, *devopsagent.ListAssociationsInput, ...func(*devopsagent.Options)) (*devopsagent.ListAssociationsOutput, error)
	ListServices(context.Context, *devopsagent.ListServicesInput, ...func(*devopsagent.Options)) (*devopsagent.ListServicesOutput, error)
	RegisterService(context.Context, *devopsagent.RegisterServiceInput, ...func(*devopsagent.Options)) (*devopsagent.RegisterServiceOutput, error)
	UpdateAgentSpace(context.Context, *devopsagent.UpdateAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.UpdateAgentSpaceOutput, error)
	UpdateAsset(context.Context, *devopsagent.UpdateAssetInput, ...func(*devopsagent.Options)) (*devopsagent.UpdateAssetOutput, error)
	UpdateAssociation(context.Context, *devopsagent.UpdateAssociationInput, ...func(*devopsagent.Options)) (*devopsagent.UpdateAssociationOutput, error)
}

// IAMAPI is the subset of the IAM API this package uses.
type IAMAPI interface {
	AttachRolePolicy(context.Context, *iam.AttachRolePolicyInput, ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
	CreateRole(context.Context, *iam.CreateRoleInput, ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	GetRole(context.Context, *iam.GetRoleInput, ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	ListRoleTags(context.Context, *iam.ListRoleTagsInput, ...func(*iam.Options)) (*iam.ListRoleTagsOutput, error)
	PutRolePolicy(context.Context, *iam.PutRolePolicyInput, ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error)
	UpdateAssumeRolePolicy(context.Context, *iam.UpdateAssumeRolePolicyInput, ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error)
}

// STSAPI is the subset of the STS API this package uses.
type STSAPI interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// Clients bundles the AWS clients and the resolved session details the
// provisioning steps run against.
type Clients struct {
	Agent   AgentAPI
	IAM     IAMAPI
	STS     STSAPI
	Sources Sources
	Region  string
}

// NewClients builds AWS clients from the ambient credential chain: environment
// variables, shared config/credentials files, SSO caches, or instance metadata.
// A non-empty profile overrides AWS_PROFILE for this session, so callers that
// just ran `aws sso login --profile foo` can select that profile without
// exporting an env var.
// It fails before any API call when the session has no credentials or no region.
func NewClients(ctx context.Context, region, profile string) (Clients, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
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
		return Clients{}, noCredentialsError(ctx, profile, err)
	}
	if !creds.HasKeys() {
		return Clients{}, noCredentialsError(ctx, profile, nil)
	}

	return Clients{
		Agent:   devopsagent.NewFromConfig(cfg),
		IAM:     iam.NewFromConfig(cfg),
		STS:     sts.NewFromConfig(cfg),
		Sources: NewGitHubSources(),
		Region:  cfg.Region,
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

// noCredsErr hides its cause from Error.
type noCredsErr struct {
	msg   string
	cause error
}

func (e *noCredsErr) Error() string        { return e.msg }
func (e *noCredsErr) Unwrap() error        { return e.cause }
func (e *noCredsErr) Is(target error) bool { return target == ErrNoCredentials }

// maxListedProfiles caps the profile names in an error.
const maxListedProfiles = 5

// noCredentialsError builds the surfaced message for a credential-resolution
// failure. profile is the value passed via --profile; cause is the underlying
// SDK error, which may be nil when credentials simply had no keys.
func noCredentialsError(ctx context.Context, profile string, cause error) error {
	selected, chosen := SelectedProfile(profile)
	profiles := knownProfiles()
	// The default profile counts only if it exists.
	named := chosen || slices.Contains(profiles, selected)
	// Only a cause that is not from IMDS tells what failed, for example a
	// credential_process. The message does not show the cause, because a
	// cause can contain the output of a credential_process with the secret key.
	useful := cause != nil && !fromIMDS(cause)
	sso := SSOProfile(ctx, profile)

	var msg string
	switch {
	case sso != "" && (cause == nil || SSOTokenInvalid(cause)):
		msg = fmt.Sprintf(
			"no AWS credentials for profile %q. Run `aws sso login --profile %s`, then run this command again",
			sso, sso,
		)
	case useful && named:
		msg = fmt.Sprintf(
			"unable to get AWS credentials for profile %q. Run `aws sts get-caller-identity --profile %s` to see the cause",
			selected, selected,
		)
	case useful:
		msg = "unable to get AWS credentials. Run `aws sts get-caller-identity` to see the cause"
	case chosen:
		msg = fmt.Sprintf(
			"profile %q has no AWS credentials. Add credentials to it, or pass --profile with another profile",
			selected,
		)
	case len(profiles) > 0:
		msg = fmt.Sprintf(
			"no AWS profile selected. Pass --profile <name> or set AWS_PROFILE. Profiles: %s. Run `aws configure list-profiles` to see all",
			summarizeProfiles(profiles),
		)
	default:
		msg = "no AWS credentials found. Run `aws sso login --profile <name>` or set AWS_ACCESS_KEY_ID"
	}

	return &noCredsErr{msg: msg, cause: cause}
}

// fromIMDS reports a cause from the EC2 metadata service. The SDK tries it
// last, so its error only says that no other source had credentials.
func fromIMDS(err error) bool {
	var op interface{ Service() string }

	return errors.As(err, &op) && op.Service() == "ec2imds"
}

// summarizeProfiles lists the first few names.
func summarizeProfiles(profiles []string) string {
	if len(profiles) <= maxListedProfiles {
		return strings.Join(profiles, ", ")
	}

	return fmt.Sprintf(
		"%s and %d more",
		strings.Join(profiles[:maxListedProfiles], ", "),
		len(profiles)-maxListedProfiles,
	)
}

// knownProfiles returns the profile names configured in the shared AWS
// config and credentials files. It is best-effort: unreadable files are
// silently skipped so a diagnostic path never masks the original error.
func knownProfiles() []string {
	var profiles []string
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		profiles = append(profiles, name)
	}

	for _, path := range []string{credentialsFilePath(), configFilePath()} {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
				continue
			}
			name := strings.TrimSpace(line[1 : len(line)-1])
			switch {
			case name == "default":
				add("default")
			case strings.HasPrefix(name, "profile "):
				add(strings.TrimSpace(strings.TrimPrefix(name, "profile ")))
			case strings.ContainsRune(name, ' '):
				// sso-session, services, etc. — skip
			default:
				// credentials file lists bare names
				add(name)
			}
		}
		_ = file.Close()
	}

	return profiles
}

// credentialsFilePath and configFilePath return the files the SDK reads
// profiles from. AWS_SHARED_CREDENTIALS_FILE and AWS_CONFIG_FILE override them.
func credentialsFilePath() string {
	if v := os.Getenv("AWS_SHARED_CREDENTIALS_FILE"); v != "" {
		return v
	}

	return awsconfig.DefaultSharedCredentialsFilename()
}

func configFilePath() string {
	if v := os.Getenv("AWS_CONFIG_FILE"); v != "" {
		return v
	}

	return awsconfig.DefaultSharedConfigFilename()
}
