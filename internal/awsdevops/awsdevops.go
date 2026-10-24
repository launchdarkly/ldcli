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
	ErrNoRegion      = errors.New("no AWS region found." +
		"\n\nTo fix this, pass --region <region>, OR run `export AWS_REGION=<region>`, " +
		"OR add a region to the AWS profile with `aws configure set region <region> --profile <name>`.")
)

// AgentAPI is the subset of the AWS DevOps Agent API this package uses.
type AgentAPI interface {
	AssociateService(context.Context, *devopsagent.AssociateServiceInput, ...func(*devopsagent.Options)) (*devopsagent.AssociateServiceOutput, error)
	CreateAgentSpace(context.Context, *devopsagent.CreateAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.CreateAgentSpaceOutput, error)
	DeregisterService(context.Context, *devopsagent.DeregisterServiceInput, ...func(*devopsagent.Options)) (*devopsagent.DeregisterServiceOutput, error)
	DisassociateService(context.Context, *devopsagent.DisassociateServiceInput, ...func(*devopsagent.Options)) (*devopsagent.DisassociateServiceOutput, error)
	EnableOperatorApp(context.Context, *devopsagent.EnableOperatorAppInput, ...func(*devopsagent.Options)) (*devopsagent.EnableOperatorAppOutput, error)
	GetOperatorApp(context.Context, *devopsagent.GetOperatorAppInput, ...func(*devopsagent.Options)) (*devopsagent.GetOperatorAppOutput, error)
	ListTagsForResource(context.Context, *devopsagent.ListTagsForResourceInput, ...func(*devopsagent.Options)) (*devopsagent.ListTagsForResourceOutput, error)
	GetAgentSpace(context.Context, *devopsagent.GetAgentSpaceInput, ...func(*devopsagent.Options)) (*devopsagent.GetAgentSpaceOutput, error)
	ListAgentSpaces(context.Context, *devopsagent.ListAgentSpacesInput, ...func(*devopsagent.Options)) (*devopsagent.ListAgentSpacesOutput, error)
	ListAssociations(context.Context, *devopsagent.ListAssociationsInput, ...func(*devopsagent.Options)) (*devopsagent.ListAssociationsOutput, error)
	ListServices(context.Context, *devopsagent.ListServicesInput, ...func(*devopsagent.Options)) (*devopsagent.ListServicesOutput, error)
	RegisterService(context.Context, *devopsagent.RegisterServiceInput, ...func(*devopsagent.Options)) (*devopsagent.RegisterServiceOutput, error)
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
	var missing awsconfig.SharedConfigProfileNotExistError
	if errors.As(err, &missing) {
		// The selected profile is not in the files, for example after a typo.
		return Clients{}, noCredentialsError(profile, nil)
	}
	if err != nil {
		return Clients{}, fmt.Errorf("unable to load AWS configuration: %w", err)
	}
	if cfg.Region == "" {
		// A selected profile can give the region, so report the missing
		// profile first.
		if NoProfileSelected(profile) && len(knownProfiles()) > 0 {
			return Clients{}, noCredentialsError(profile, nil)
		}

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

// noCredsErr hides its cause from Error.
type noCredsErr struct {
	msg   string
	cause error
}

func (e *noCredsErr) Error() string        { return e.msg }
func (e *noCredsErr) Unwrap() error        { return e.cause }
func (e *noCredsErr) Is(target error) bool { return target == ErrNoCredentials }

// maxListedProfiles caps the profile names in an error. Up to this many,
// the error lists every profile.
const maxListedProfiles = 10

// noCredentialsError builds the surfaced message for a credential-resolution
// failure. profile is the value passed via --profile; cause is the underlying
// SDK error, which may be nil when credentials simply had no keys.
func noCredentialsError(profile string, cause error) error {
	selected, chosen := SelectedProfile(profile)
	profiles := knownProfiles()
	exists := slices.ContainsFunc(profiles, func(p awsProfile) bool { return p.name == selected })
	// The default profile counts only if it exists.
	named := chosen || exists
	// Only a cause that is not from IMDS tells what failed, for example a
	// credential_process. The message does not show the cause, because a
	// cause can contain the output of a credential_process with the secret key.
	useful := cause != nil && !fromIMDS(cause)
	sso := SSOProfile(ConfigFilePath(), profile)

	var msg string
	switch {
	case sso != "" && (cause == nil || SSOTokenInvalid(cause)):
		msg = fmt.Sprintf(
			"no AWS credentials for profile %q. Run `aws sso login --profile %s`, then run this command again.",
			sso, sso,
		) + otherProfiles(selected, chosen, profiles)
	case useful && named:
		msg = fmt.Sprintf(
			"unable to get AWS credentials for profile %q. Run `aws sts get-caller-identity --profile %s` to see the cause.",
			selected, selected,
		) + otherProfiles(selected, chosen, profiles)
	case useful:
		msg = "unable to get AWS credentials. Run `aws sts get-caller-identity` to see the cause."
	case chosen && !exists && len(profiles) > 0:
		msg = fmt.Sprintf("profile %q is not in your AWS config.", selected) + fixes(
			"pass --profile <name> with a profile from the list below",
			"run `export AWS_PROFILE=<name>`",
			addProfile(selected),
		) + listProfiles(profiles)
	case chosen && !exists:
		msg = fmt.Sprintf("profile %q is not in your AWS config.", selected) + fixes(addProfile(selected))
	case chosen:
		others := otherThan(selected, profiles)
		options := []string{addProfile(selected)}
		if len(others) > 0 {
			options = append([]string{"pass --profile <name> with another profile from the list below"}, options...)
		}
		msg = fmt.Sprintf("profile %q has no AWS credentials.", selected) + fixes(options...) + listProfiles(others)
	case len(profiles) > 0:
		msg = "no AWS profile selected." + fixes(
			"pass --profile <name> with a profile from the list below",
			"run `export AWS_PROFILE=<name>`",
			// The SDK uses the default profile with no flag, so the next
			// run works.
			addProfile("default"),
		) + listProfiles(profiles)
	default:
		msg = "no AWS credentials found." + fixes(addProfile("default"))
	}

	return &noCredsErr{msg: msg, cause: cause}
}

// fixes joins the ways to fix an error, easiest first, in one sentence. The
// uppercase OR shows that the user does only one of them.
func fixes(options ...string) string {
	return "\n\nTo fix this, " + strings.Join(options, ", OR ") + "."
}

// addProfile tells how to add the named profile with SSO or an AWS console
// session.
func addProfile(name string) string {
	return fmt.Sprintf(
		"add the %s profile with `aws configure sso --profile %s` for SSO, or `aws login --profile %s` for an AWS console session",
		name, name, name,
	)
}

// otherProfiles tells how to use another profile when the SDK fell back to
// the default profile. The user can expect a profile from aws sso login, but
// the SDK does not use it without --profile or AWS_PROFILE.
func otherProfiles(selected string, chosen bool, profiles []awsProfile) string {
	if chosen {
		return ""
	}
	others := otherThan(selected, profiles)
	if len(others) == 0 {
		return ""
	}

	return "\n\nTo use another profile, pass --profile <name> with a profile from the list below, " +
		"OR run `export AWS_PROFILE=<name>`." + listProfiles(others)
}

// otherThan returns the profiles without the named one.
func otherThan(name string, profiles []awsProfile) []awsProfile {
	return slices.DeleteFunc(slices.Clone(profiles), func(p awsProfile) bool { return p.name == name })
}

// fromIMDS reports a cause from the EC2 metadata service. The SDK tries it
// last, so its error only says that no other source had credentials.
func fromIMDS(err error) bool {
	var op interface{ Service() string }

	return errors.As(err, &op) && op.Service() == "ec2imds"
}

// awsProfile is a profile in the shared AWS files. account and role are
// empty if the profile does not name them.
type awsProfile struct {
	name    string
	account string
	role    string
}

// listProfiles lists the first few profiles, one per line, with the account
// and role of each. If it does not show them all, it names the command that
// does.
func listProfiles(profiles []awsProfile) string {
	if len(profiles) == 0 {
		return ""
	}
	shown := profiles[:min(len(profiles), maxListedProfiles)]
	nameWidth, accountWidth := 0, 0
	for _, p := range shown {
		nameWidth = max(nameWidth, len(p.name))
		accountWidth = max(accountWidth, len(p.account))
	}

	var b strings.Builder
	b.WriteString("\n\nProfiles:")
	for _, p := range shown {
		line := fmt.Sprintf("  %-*s  %-*s  %s", nameWidth, p.name, accountWidth, p.account, p.role)
		b.WriteString("\n" + strings.TrimRight(line, " "))
	}
	if more := len(profiles) - len(shown); more > 0 {
		fmt.Fprintf(&b, "\n  and %d more. Run `aws configure list-profiles` to see all of them", more)
	}

	return b.String()
}

// knownProfiles returns the profiles configured in the shared AWS config and
// credentials files. It is best-effort: unreadable files are silently skipped
// so a diagnostic path never masks the original error.
func knownProfiles() []awsProfile {
	var profiles []awsProfile
	index := map[string]int{}
	current := -1
	add := func(name string) {
		if name == "" {
			current = -1
			return
		}
		i, ok := index[name]
		if !ok {
			i = len(profiles)
			index[name] = i
			profiles = append(profiles, awsProfile{name: name})
		}
		current = i
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
				if current >= 0 {
					setProfileField(&profiles[current], line)
				}
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
				add("")
			default:
				// credentials file lists bare names
				add(name)
			}
		}
		_ = file.Close()
	}

	return profiles
}

// setProfileField reads the account and role from an SSO or role_arn line.
func setProfileField(p *awsProfile, line string) {
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}
	value = strings.TrimSpace(value)
	switch strings.TrimSpace(key) {
	case "sso_account_id":
		p.account = value
	case "sso_role_name":
		p.role = value
	case "role_arn":
		// arn:aws:iam::<account>:role/<path/><name>
		parts := strings.SplitN(value, ":", 6)
		if len(parts) == 6 && strings.HasPrefix(parts[5], "role/") {
			if p.account == "" {
				p.account = parts[4]
			}
			if p.role == "" {
				p.role = parts[5][strings.LastIndex(parts[5], "/")+1:]
			}
		}
	}
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
