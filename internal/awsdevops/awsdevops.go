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
	"path/filepath"
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
		return Clients{}, noCredentialsError(profile, err)
	}
	if !creds.HasKeys() {
		return Clients{}, noCredentialsError(profile, nil)
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

// noCredsErr wraps a formatted message while still satisfying
// errors.Is(err, ErrNoCredentials). Callers get an actionable message
// without losing the ability to branch on the sentinel.
type noCredsErr struct {
	msg   string
	cause error
}

func (e *noCredsErr) Error() string {
	if e.cause == nil {
		return e.msg
	}

	return fmt.Sprintf("%s: %s", e.msg, e.cause)
}

func (e *noCredsErr) Unwrap() error    { return e.cause }
func (e *noCredsErr) Is(target error) bool { return target == ErrNoCredentials }

// noCredentialsError builds the surfaced message for a credential-resolution
// failure. profile is the value passed via --profile; cause is the underlying
// SDK error, which may be nil when credentials simply had no keys.
func noCredentialsError(profile string, cause error) error {
	active := profile
	if active == "" {
		active = os.Getenv("AWS_PROFILE")
	}

	switch {
	case active != "":
		return &noCredsErr{
			msg: fmt.Sprintf(
				"no AWS credentials found for profile %q. Run `aws sso login --profile %s` (or refresh the profile's credentials), then re-run this command",
				active, active,
			),
			cause: cause,
		}
	case len(knownProfiles()) > 0:
		profiles := knownProfiles()
		return &noCredsErr{
			msg: fmt.Sprintf(
				"no AWS credentials found and no AWS profile selected. Pass --profile <name> or set AWS_PROFILE, then re-run. Profiles configured on this machine: %s",
				strings.Join(profiles, ", "),
			),
			cause: cause,
		}
	default:
		return &noCredsErr{
			msg: "no AWS credentials found. Authenticate first (for example `aws sso login --profile my-profile` or exporting AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY and AWS_SESSION_TOKEN), then re-run this command",
			cause: cause,
		}
	}
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

	for _, path := range sharedConfigPaths() {
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

// sharedConfigPaths returns the paths the AWS SDK reads profiles from,
// honoring AWS_CONFIG_FILE and AWS_SHARED_CREDENTIALS_FILE overrides.
func sharedConfigPaths() []string {
	var paths []string
	if v := os.Getenv("AWS_SHARED_CREDENTIALS_FILE"); v != "" {
		paths = append(paths, v)
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".aws", "credentials"))
	}
	if v := os.Getenv("AWS_CONFIG_FILE"); v != "" {
		paths = append(paths, v)
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".aws", "config"))
	}

	return paths
}
