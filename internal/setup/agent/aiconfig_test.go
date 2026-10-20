package agent

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/launchdarkly/go-ai-sdk/aiserver"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayOptions_SetsEveryConnectionOption(t *testing.T) {
	opts := relayOptions("http://127.0.0.1:5000/secret")

	assert.Equal(t, aiserver.InitOptions{
		SDKKey:       RelayPlaceholderKey,
		BaseURI:      "http://127.0.0.1:5000/secret/polling",
		StreamURI:    "http://127.0.0.1:5000/secret/stream",
		EventsURI:    "http://127.0.0.1:5000/secret/events",
		OTLPEndpoint: "http://127.0.0.1:5000/secret/otel",
		ServiceName:  "ldcli",
		Environment:  "ldcli-setup",
		WaitFor:      initTimeout,
	}, opts, "an empty option would make the SDK read an LD_ variable")
}

func TestNewAIConfigSource(t *testing.T) {
	assert.Equal(t, DefaultConfigSource{}, NewAIConfigSource("https://app.launchdarkly.com", ""), "no login, no AI Config")

	source, ok := NewAIConfigSource("https://app.launchdarkly.com", "api-123").(*AIConfigSource)
	require.True(t, ok)
	assert.Equal(t, SetupAgentConfigKey, source.Key)
	assert.Equal(t, "https://app.launchdarkly.com", source.RelayBaseURI)
	assert.Equal(t, "api-123", source.AccessToken)
}

func TestAIConfigSource_StartsTheSDKBehindTheProxy(t *testing.T) {
	t.Setenv(environmentIDEnv, "the-users-own-app")
	var got aiserver.InitOptions
	s := &AIConfigSource{
		RelayBaseURI: "https://app.launchdarkly.com",
		AccessToken:  "api-123",
		checkRelay:   func(context.Context, string) (string, error) { return "catfood-client-id", nil },
		initClient: func(_ context.Context, opts aiserver.InitOptions) error {
			got = opts
			return nil
		},
		inspect: func(context.Context, string, ldcontext.Context) aiserver.InspectResult {
			return aiserver.InspectResult{}
		},
	}
	defer s.Close(context.Background())

	_, err := s.Resolve(context.Background(), Task{})

	require.NoError(t, err)
	assert.Equal(t, RelayPlaceholderKey, got.SDKKey)
	assert.True(t, strings.HasPrefix(got.StreamURI, "http://127.0.0.1:"), got.StreamURI)
	assert.NotContains(t, got.StreamURI, "api-123", "the token is never in an SDK option")
	assert.Equal(t, "catfood-client-id", os.Getenv(environmentIDEnv), "the ID comes from the relay")
}

func TestAIConfigSource_NoEnvironmentID_ClearsTheUsersValue(t *testing.T) {
	t.Setenv(environmentIDEnv, "the-users-own-app")
	s, _ := fakeSource(nil, aiserver.InspectResult{})
	defer s.Close(context.Background())

	_, _ = s.Resolve(context.Background(), Task{})

	_, set := os.LookupEnv(environmentIDEnv)
	assert.False(t, set)
}

type inspectCall struct {
	key string
	ctx ldcontext.Context
}

func fakeSource(initErr error, res aiserver.InspectResult) (*AIConfigSource, *[]inspectCall) {
	var calls []inspectCall
	s := &AIConfigSource{
		Key:          "setup",
		RelayBaseURI: "https://app.launchdarkly.com",
		AccessToken:  "api-123",
		checkRelay:   noRelayCheck,
		initClient:   func(context.Context, aiserver.InitOptions) error { return initErr },
		inspect: func(_ context.Context, key string, c ldcontext.Context) aiserver.InspectResult {
			calls = append(calls, inspectCall{key: key, ctx: c})
			return res
		},
	}
	return s, &calls
}

func enabled(c aiserver.AiConfig) aiserver.InspectResult {
	return aiserver.InspectResult{Enabled: true, Config: &c}
}

func agentConfig(provider, model, instructions string) aiserver.AiConfig {
	c := aiserver.AiConfig{Instructions: instructions, Model: aiserver.Model{Name: model}}
	c.Provider.Name = provider
	return c
}

func TestAIConfigSource_AgentMode(t *testing.T) {
	s, calls := fakeSource(nil, enabled(agentConfig("anthropic", "claude-sonnet-5", "Edit the entry point.")))

	cfg, err := s.Resolve(context.Background(), Task{SDKID: "node-server", Language: "JavaScript", Framework: "nextjs", EntryPoint: "app/layout.tsx", Dir: "/home/me/secret-project"})

	require.NoError(t, err)
	assert.Equal(t, Config{ProviderID: "anthropic", ModelID: "claude-sonnet-5", Instructions: "Edit the entry point."}, cfg)
	require.Len(t, *calls, 1)
	call := (*calls)[0]
	assert.Equal(t, "setup", call.key)
	assert.Equal(t, ldcontext.Kind("ldcli-setup"), call.ctx.Kind())
	assert.True(t, call.ctx.Anonymous())
	assert.Equal(t, "nextjs", call.ctx.GetValue("framework").StringValue())
	assert.Equal(t, "node-server", call.ctx.GetValue("sdkId").StringValue())
	assert.True(t, call.ctx.GetValue("hasEntryPoint").BoolValue())
	for _, name := range call.ctx.GetOptionalAttributeNames(nil) {
		value := call.ctx.GetValue(name).StringValue()
		assert.NotContains(t, value, "secret-project", "the context must not carry paths")
		assert.NotContains(t, value, "layout.tsx", "the context must not carry paths")
	}
}

func TestAIConfigSource_InstructionsWithVariables_OwnTheTask(t *testing.T) {
	s, _ := fakeSource(nil, enabled(agentConfig("anthropic", "claude-sonnet-5",
		"Set up {{sdkId}} in {{entryPoint}}, read {{credentialEnvVar}}, evaluate {{flagKey}}. Installed: {{sdkInstalled}}. Keep {{unknown}}.")))

	cfg, err := s.Resolve(context.Background(), Task{SDKID: "python-server-sdk", EntryPoint: "main.py", FlagKey: "my-new-flag", SDKInstalled: true})

	require.NoError(t, err)
	assert.Equal(t, "Set up python-server-sdk in main.py, read LAUNCHDARKLY_SDK_KEY, evaluate my-new-flag. Installed: true. Keep {{unknown}}.", cfg.Instructions)
	assert.Equal(t, StartMessage, cfg.Task, "the config describes the task, so the CLI sends only the start message")
}

func TestAIConfigSource_InstructionsWithoutVariables_KeepTheCLITask(t *testing.T) {
	s, _ := fakeSource(nil, enabled(agentConfig("anthropic", "claude-sonnet-5", "Prefer small diffs.")))

	cfg, err := s.Resolve(context.Background(), Task{SDKID: "python-server-sdk"})

	require.NoError(t, err)
	assert.Equal(t, "Prefer small diffs.", cfg.Instructions)
	assert.Empty(t, cfg.Task, "the run sends the CLI's own task message")
}

func TestAIConfigSource_CompletionModeUsesSystemMessages(t *testing.T) {
	c := agentConfig("openai", "gpt-6", "")
	c.Messages = []aiserver.Message{{Role: "system", Text: "One"}, {Role: "user", Text: "skip"}, {Role: "system", Text: "Two"}}
	s, _ := fakeSource(nil, enabled(c))

	cfg, err := s.Resolve(context.Background(), Task{})

	require.NoError(t, err)
	assert.Equal(t, "One\n\nTwo", cfg.Instructions)
}

func TestAIConfigSource_MapsProviderNames(t *testing.T) {
	s, _ := fakeSource(nil, enabled(agentConfig("Bedrock", "anthropic.claude", "x")))

	cfg, err := s.Resolve(context.Background(), Task{})

	require.NoError(t, err)
	assert.Equal(t, "amazon-bedrock", cfg.ProviderID)
}

func TestAIConfigSource_ModelWithoutProvider_UsesOpenCodeDefault(t *testing.T) {
	s, _ := fakeSource(nil, enabled(agentConfig("", "claude-sonnet-5", "")))

	cfg, err := s.Resolve(context.Background(), Task{})

	require.NoError(t, err)
	assert.Equal(t, Config{Instructions: DefaultInstructions}, cfg)
}

func TestAIConfigSource_FallsBack(t *testing.T) {
	for name, s := range map[string]*AIConfigSource{
		"init fails":     first(fakeSource(errors.New("no network"), aiserver.InspectResult{})),
		"config is off":  first(fakeSource(nil, aiserver.InspectResult{Enabled: false})),
		"config missing": first(fakeSource(nil, aiserver.InspectResult{Enabled: true})),
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := s.Resolve(context.Background(), Task{})

			require.NoError(t, err)
			assert.Equal(t, Config{Instructions: DefaultInstructions}, cfg)
		})
	}
}

func TestAIConfigSource_InitsOnce(t *testing.T) {
	inits := 0
	s := &AIConfigSource{
		RelayBaseURI: "https://app.launchdarkly.com",
		checkRelay:   noRelayCheck,
		initClient:   func(context.Context, aiserver.InitOptions) error { inits++; return nil },
		inspect: func(context.Context, string, ldcontext.Context) aiserver.InspectResult {
			return aiserver.InspectResult{}
		},
	}

	_, _ = s.Resolve(context.Background(), Task{})
	_, _ = s.Resolve(context.Background(), Task{})

	assert.Equal(t, 1, inits)
	assert.NoError(t, s.Close(context.Background()), "a source that did not start the SDK has nothing to stop")
}

func first(s *AIConfigSource, _ *[]inspectCall) *AIConfigSource { return s }

// noRelayCheck stands in for the request to the relay, so unit tests make no
// network calls.
func noRelayCheck(context.Context, string) (string, error) { return "", nil }
