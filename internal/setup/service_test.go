package setup

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/environments"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/flags"
	"github.com/launchdarkly/ldcli/internal/projects"
	"github.com/launchdarkly/ldcli/internal/resources"
)

var testAuth = Auth{AccessToken: "token", BaseURI: "https://example.com"}

// fakeDetector / fakeInstaller let us drive the service's passthrough steps
// without the filesystem or shelling out.
type fakeDetector struct {
	result *DetectResult
	err    error
}

func (f fakeDetector) Detect(string) (*DetectResult, error) { return f.result, f.err }

type fakeInstaller struct {
	result *InstallResult
	err    error
}

func (f fakeInstaller) Install(string, *DetectResult) (*InstallResult, error) {
	return f.result, f.err
}

func TestService_ListProjects(t *testing.T) {
	mockProjects := &projects.MockClient{}
	mockProjects.On("List", testAuth.AccessToken, testAuth.BaseURI, int64(listPageSize), int64(0)).
		Return([]byte(`{"items":[{"key":"p1","name":"Project One"},{"key":"p2","name":"Project Two"}]}`), nil)
	svc := Service{Clients: Clients{Projects: mockProjects}}

	got, err := svc.ListProjects(testAuth)

	require.NoError(t, err)
	assert.Equal(t, []ProjectSummary{{Key: "p1", Name: "Project One"}, {Key: "p2", Name: "Project Two"}}, got)
}

func TestService_ListEnvironments(t *testing.T) {
	mockEnvs := &environments.MockClient{}
	mockEnvs.On("List", testAuth.AccessToken, testAuth.BaseURI, "p1", int64(listPageSize), int64(0)).
		Return([]byte(`{"items":[{"key":"production","name":"Production"}]}`), nil)
	svc := Service{Clients: Clients{Environments: mockEnvs}}

	got, err := svc.ListEnvironments(testAuth, "p1")

	require.NoError(t, err)
	assert.Equal(t, []EnvSummary{{Key: "production", Name: "Production"}}, got)
}

func TestService_EnvKeys(t *testing.T) {
	mockEnvs := &environments.MockClient{}
	mockEnvs.On("Get", testAuth.AccessToken, testAuth.BaseURI, "production", "p1").
		Return([]byte(`{"apiKey":"sdk-123","_id":"client-456","mobileKey":"mob-789"}`), nil)
	svc := Service{Clients: Clients{Environments: mockEnvs}}

	got, err := svc.EnvKeys(testAuth, "p1", "production")

	require.NoError(t, err)
	assert.Equal(t, EnvKeys{SDKKey: "sdk-123", ClientSideID: "client-456", MobileKey: "mob-789"}, got)
}

func TestService_CreateFlag_Success(t *testing.T) {
	mockFlags := &flags.MockClient{}
	mockFlags.On("Create", testAuth.AccessToken, testAuth.BaseURI, "My New Flag", "my-new-flag", "p1").
		Return([]byte(`{"key":"my-new-flag"}`), nil)
	svc := Service{Clients: Clients{Flags: mockFlags}}

	key, err := svc.CreateFlag(testAuth, "p1", "my-new-flag", "My New Flag", "node-server")

	require.NoError(t, err)
	assert.Equal(t, "my-new-flag", key)
}

func TestService_CreateFlag_ConflictIsSuccess(t *testing.T) {
	mockFlags := &flags.MockClient{}
	mockFlags.On("Create", testAuth.AccessToken, testAuth.BaseURI, "My New Flag", "my-new-flag", "p1").
		Return([]byte(nil), errors.NewError(`{"code":"conflict","message":"already exists"}`))
	svc := Service{Clients: Clients{Flags: mockFlags}}

	key, err := svc.CreateFlag(testAuth, "p1", "my-new-flag", "My New Flag", "node-server")

	require.NoError(t, err)
	assert.Equal(t, "my-new-flag", key)
}

func TestService_CreateFlag_OtherErrorPropagates(t *testing.T) {
	mockFlags := &flags.MockClient{}
	mockFlags.On("Create", testAuth.AccessToken, testAuth.BaseURI, "My New Flag", "my-new-flag", "p1").
		Return([]byte(nil), errors.NewError(`{"code":"internal_error"}`))
	svc := Service{Clients: Clients{Flags: mockFlags}}

	_, err := svc.CreateFlag(testAuth, "p1", "my-new-flag", "My New Flag", "node-server")

	assert.Error(t, err)
}

func TestService_Detect(t *testing.T) {
	want := &DetectResult{Language: "go", SDKID: "go-server-sdk"}
	svc := Service{Detector: fakeDetector{result: want}}

	got, err := svc.Detect("/some/dir")

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestService_Install_Success(t *testing.T) {
	want := &InstallResult{SDKID: "node-server", Success: true}
	svc := Service{Installer: fakeInstaller{result: want}}

	got, err := svc.Install("/some/dir", &DetectResult{SDKID: "node-server"})

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestService_Install_ErrorPropagates(t *testing.T) {
	// The service returns the installer's error unchanged; the wizard, not the
	// service, decides whether to continue past a failed install.
	svc := Service{Installer: fakeInstaller{err: errors.NewError("boom")}}

	_, err := svc.Install("/some/dir", &DetectResult{SDKID: "node-server"})

	assert.Error(t, err)
}

func TestService_Inject(t *testing.T) {
	svc := Service{Initializer: Initializer{}}
	filePath := filepath.Join(t.TempDir(), "index.js")

	result, err := svc.Inject("node-server", filePath, InitConfig{SDKKey: "sdk-123"})

	require.NoError(t, err)
	assert.Equal(t, "node-server", result.SDKID)
	assert.True(t, result.Success)
}

func TestService_Verify_Active(t *testing.T) {
	svc := Service{Clients: Clients{Resources: &resources.MockClient{Response: []byte(`{"active":true}`)}}}

	result, err := svc.Verify(testAuth, "p1", "production", "node-server")

	require.NoError(t, err)
	assert.True(t, result.Active)
	assert.Equal(t, 1, result.Attempts)
}

func TestService_ListProjects_FollowsPagination(t *testing.T) {
	first := make([]string, listPageSize)
	for i := range first {
		first[i] = fmt.Sprintf(`{"key":"p%d","name":"Project %d"}`, i, i)
	}
	mockProjects := &projects.MockClient{}
	mockProjects.On("List", testAuth.AccessToken, testAuth.BaseURI, int64(listPageSize), int64(0)).
		Return([]byte(`{"items":[`+strings.Join(first, ",")+`]}`), nil)
	mockProjects.On("List", testAuth.AccessToken, testAuth.BaseURI, int64(listPageSize), int64(listPageSize)).
		Return([]byte(`{"items":[{"key":"last","name":"Last"}]}`), nil)
	svc := Service{Clients: Clients{Projects: mockProjects}}

	got, err := svc.ListProjects(testAuth)

	require.NoError(t, err)
	assert.Len(t, got, listPageSize+1)
	assert.Equal(t, ProjectSummary{Key: "last", Name: "Last"}, got[len(got)-1])
	mockProjects.AssertExpectations(t)
}

func TestService_ListEnvironments_FollowsPagination(t *testing.T) {
	first := make([]string, listPageSize)
	for i := range first {
		first[i] = fmt.Sprintf(`{"key":"e%d","name":"Env %d"}`, i, i)
	}
	mockEnvs := &environments.MockClient{}
	mockEnvs.On("List", testAuth.AccessToken, testAuth.BaseURI, "p1", int64(listPageSize), int64(0)).
		Return([]byte(`{"items":[`+strings.Join(first, ",")+`]}`), nil)
	mockEnvs.On("List", testAuth.AccessToken, testAuth.BaseURI, "p1", int64(listPageSize), int64(listPageSize)).
		Return([]byte(`{"items":[{"key":"last","name":"Last"}]}`), nil)
	svc := Service{Clients: Clients{Environments: mockEnvs}}

	got, err := svc.ListEnvironments(testAuth, "p1")

	require.NoError(t, err)
	assert.Len(t, got, listPageSize+1)
	mockEnvs.AssertExpectations(t)
}

// A flag a browser SDK is meant to read has to be created with client-side
// availability: the API leaves usingEnvironmentId false, so without it the SDK
// evaluates the fallback forever while setup reports success.
func TestService_CreateFlag_ClientSideAvailability(t *testing.T) {
	tests := []struct {
		sdkID string
		want  *flags.ClientSideAvailability
	}{
		{"js-client-sdk", &flags.ClientSideAvailability{UsingEnvironmentID: true, UsingMobileKey: true}},
		{"react-client-sdk", &flags.ClientSideAvailability{UsingEnvironmentID: true, UsingMobileKey: true}},
		{"node-server", nil},
		{"go-server-sdk", nil},
		{"react-native", nil},
		{"android", nil},
		{"swift-client-sdk", nil},
	}
	for _, tt := range tests {
		t.Run(tt.sdkID, func(t *testing.T) {
			mockFlags := &flags.MockClient{}
			mockFlags.On("Create", testAuth.AccessToken, testAuth.BaseURI, "My New Flag", "my-new-flag", "p1").
				Return([]byte(`{"key":"my-new-flag"}`), nil)
			svc := Service{Clients: Clients{Flags: mockFlags}}

			_, err := svc.CreateFlag(testAuth, "p1", "my-new-flag", "My New Flag", tt.sdkID)

			require.NoError(t, err)
			assert.Equal(t, tt.want, mockFlags.CreatedAvailability)
		})
	}
}

// The classification comes from each SDK's own init template, so this pins which
// credential every known SDK is understood to take.
func TestUsesClientSideID_MatchesTemplateCredentials(t *testing.T) {
	clientSide := map[string]bool{"js-client-sdk": true, "react-client-sdk": true}
	for _, sdk := range KnownSDKs {
		assert.Equal(t, clientSide[sdk.ID], UsesClientSideID(sdk.ID), "sdk %s", sdk.ID)
	}
}
