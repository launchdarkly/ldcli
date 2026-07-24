package setup

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/launchdarkly/ldcli/internal/resources"
)

// Auth carries resolved credentials so the service never reads global config.
type Auth struct {
	AccessToken string
	BaseURI     string
}

// Service orchestrates the setup steps over the LaunchDarkly API and the local
// project. It holds no UI or CLI state; callers resolve credentials into Auth
// and pass them in.
type Service struct {
	Client      resources.Client
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

// ListProjects returns the account's projects.
func (s Service) ListProjects(a Auth) ([]ProjectSummary, error) {
	path, _ := url.JoinPath(a.BaseURI, "api/v2/projects")
	res, err := s.Client.MakeRequest(a.AccessToken, "GET", path, "application/json", nil, nil, false)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Items []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(res, &resp); err != nil {
		return nil, fmt.Errorf("parsing projects: %w", err)
	}

	projects := make([]ProjectSummary, len(resp.Items))
	for i, item := range resp.Items {
		projects[i] = ProjectSummary{Key: item.Key, Name: item.Name}
	}
	return projects, nil
}

// ListEnvironments returns the environments in a project.
func (s Service) ListEnvironments(a Auth, projectKey string) ([]EnvSummary, error) {
	path, _ := url.JoinPath(a.BaseURI, "api/v2/projects", projectKey, "environments")
	res, err := s.Client.MakeRequest(a.AccessToken, "GET", path, "application/json", nil, nil, false)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Items []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(res, &resp); err != nil {
		return nil, fmt.Errorf("parsing environments: %w", err)
	}

	envs := make([]EnvSummary, len(resp.Items))
	for i, item := range resp.Items {
		envs[i] = EnvSummary{Key: item.Key, Name: item.Name}
	}
	return envs, nil
}

// EnvKeys returns the SDK credentials for an environment.
func (s Service) EnvKeys(a Auth, projectKey, envKey string) (EnvKeys, error) {
	path, _ := url.JoinPath(a.BaseURI, "api/v2/projects", projectKey, "environments", envKey)
	res, err := s.Client.MakeRequest(a.AccessToken, "GET", path, "application/json", nil, nil, false)
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
	path, _ := url.JoinPath(a.BaseURI, "api/v2/flags", projectKey)
	body, err := json.Marshal(map[string]string{"key": key, "name": name})
	if err != nil {
		return "", err
	}

	_, err = s.Client.MakeRequest(a.AccessToken, "POST", path, "application/json", nil, body, false)
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
func (s Service) Verify(a Auth, projectKey, envKey string) (*VerifyResult, error) {
	return DefaultVerifier(s.Client).Verify(a.AccessToken, a.BaseURI, projectKey, envKey)
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
