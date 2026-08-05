package setup

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/launchdarkly/ldcli/internal/environments"
	"github.com/launchdarkly/ldcli/internal/flags"
	"github.com/launchdarkly/ldcli/internal/projects"
	"github.com/launchdarkly/ldcli/internal/resources"
)

// Auth carries resolved credentials so the service never reads global config.
type Auth struct {
	AccessToken string
	BaseURI     string
}

// Clients groups the LaunchDarkly API clients the service depends on. Projects,
// Environments, and Flags use the shared typed clients; Resources backs Verify,
// whose sdk-active endpoint has no typed-client wrapper.
type Clients struct {
	Projects     projects.Client
	Environments environments.Client
	Flags        flags.Client
	Resources    resources.Client
}

// Service orchestrates the setup steps over the LaunchDarkly API and the local
// project. It holds no UI or CLI state; callers resolve credentials into Auth
// and pass them in.
type Service struct {
	Clients     Clients
	Detector    Detector
	Installer   Installer
	Initializer Initializer
}

// ProjectSummary is a project as the setup flow needs it.
type ProjectSummary struct {
	Key  string
	Name string
}

// EnvSummary is an environment as the setup flow needs it.
type EnvSummary struct {
	Key  string
	Name string
}

// EnvKeys are the SDK credentials for an environment.
type EnvKeys struct {
	SDKKey       string
	ClientSideID string
	MobileKey    string
}

// listPageSize is how many items each list request asks for. The wizard needs
// every project and environment, so the requests page through until a short page
// says there are no more.
const listPageSize = 100

// keyedItems is the shape both list endpoints return.
type keyedItems struct {
	Items []struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"items"`
}

// ListProjects returns the account's projects, following pagination so accounts
// with more projects than a single page are listed in full.
func (s Service) ListProjects(a Auth) ([]ProjectSummary, error) {
	var projects []ProjectSummary
	for offset := int64(0); ; offset += listPageSize {
		res, err := s.Clients.Projects.List(context.Background(), a.AccessToken, a.BaseURI, listPageSize, offset)
		if err != nil {
			return nil, err
		}

		var resp keyedItems
		if err := json.Unmarshal(res, &resp); err != nil {
			return nil, fmt.Errorf("parsing projects: %w", err)
		}

		for _, item := range resp.Items {
			projects = append(projects, ProjectSummary{Key: item.Key, Name: item.Name})
		}
		if len(resp.Items) < listPageSize {
			return projects, nil
		}
	}
}

// ListEnvironments returns the environments in a project, following pagination so
// projects with more environments than a single page are listed in full.
func (s Service) ListEnvironments(a Auth, projectKey string) ([]EnvSummary, error) {
	var envs []EnvSummary
	for offset := int64(0); ; offset += listPageSize {
		res, err := s.Clients.Environments.List(context.Background(), a.AccessToken, a.BaseURI, projectKey, listPageSize, offset)
		if err != nil {
			return nil, err
		}

		var resp keyedItems
		if err := json.Unmarshal(res, &resp); err != nil {
			return nil, fmt.Errorf("parsing environments: %w", err)
		}

		for _, item := range resp.Items {
			envs = append(envs, EnvSummary{Key: item.Key, Name: item.Name})
		}
		if len(resp.Items) < listPageSize {
			return envs, nil
		}
	}
}

// EnvKeys returns the SDK credentials for an environment.
func (s Service) EnvKeys(a Auth, projectKey, envKey string) (EnvKeys, error) {
	res, err := s.Clients.Environments.Get(context.Background(), a.AccessToken, a.BaseURI, envKey, projectKey)
	if err != nil {
		return EnvKeys{}, err
	}

	var resp struct {
		SDKKey       string `json:"apiKey"`
		ClientSideID string `json:"_id"`
		MobileKey    string `json:"mobileKey"`
	}
	if err := json.Unmarshal(res, &resp); err != nil {
		return EnvKeys{}, fmt.Errorf("parsing environment details: %w", err)
	}

	return EnvKeys{
		SDKKey:       resp.SDKKey,
		ClientSideID: resp.ClientSideID,
		MobileKey:    resp.MobileKey,
	}, nil
}

// Detect inspects the project directory for language, framework, and SDK.
func (s Service) Detect(dir string) (*DetectResult, error) {
	return s.Detector.Detect(dir)
}

// Install installs the SDK package for the project. It returns the installer's
// error unchanged; callers that must not dead-end (the interactive wizard) apply
// their own fallback, while non-interactive callers surface the error.
func (s Service) Install(dir string, detection *DetectResult) (*InstallResult, error) {
	return s.Installer.Install(dir, detection)
}

// CreateFlag creates a feature flag, treating an existing flag (conflict) as
// success and returning its key.
func (s Service) CreateFlag(a Auth, projectKey, key, name string) (string, error) {
	_, err := s.Clients.Flags.Create(context.Background(), a.AccessToken, a.BaseURI, name, key, projectKey)
	if err != nil {
		if je, parseErr := parseJSONError(err); parseErr == nil && je.Code == "conflict" {
			return key, nil
		}
		return "", err
	}
	return key, nil
}

// Inject writes SDK initialization code into filePath.
func (s Service) Inject(sdkID, filePath string, cfg InitConfig) (*InitResult, error) {
	return s.Initializer.InjectIntoFile(sdkID, filePath, cfg)
}

// Verify polls until the SDK reports as active or a timeout is reached.
func (s Service) Verify(a Auth, projectKey, envKey, sdkID string) (*VerifyResult, error) {
	return DefaultVerifier(s.Clients.Resources).Verify(a.AccessToken, a.BaseURI, projectKey, envKey, sdkID)
}

type jsonError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// parseJSONError decodes a LaunchDarkly API error whose message is a JSON body.
func parseJSONError(err error) (*jsonError, error) {
	var je jsonError
	if parseErr := json.Unmarshal([]byte(err.Error()), &je); parseErr != nil {
		return nil, parseErr
	}
	return &je, nil
}
