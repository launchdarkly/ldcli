package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/launchdarkly/go-ai-sdk/aiserver"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitOptions_SetsEveryConnectionOption(t *testing.T) {
	// These are what the SDK reads when an option is empty. None of them may
	// reach it.
	t.Setenv("LD_SDK_KEY", "sdk-wrong")
	t.Setenv("LD_BASE_URI", "https://wrong.example.com")
	t.Setenv("LD_STREAM_URI", "https://wrong.example.com")
	t.Setenv("LD_EVENTS_URI", "https://wrong.example.com")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://wrong.example.com")

	opts := initOptions("sdk-test", "", "")

	assert.Equal(t, aiserver.InitOptions{
		SDKKey:       "sdk-test",
		BaseURI:      "https://app.ld.catamorphic.com",
		StreamURI:    "https://stream.ld.catamorphic.com",
		EventsURI:    "https://events.ld.catamorphic.com",
		OTLPEndpoint: aiserver.DefaultOTLPEndpoint,
		ServiceName:  "ldcli",
		Environment:  "ldcli-setup",
		WaitFor:      initTimeout,
	}, opts)
}

func TestServiceURI(t *testing.T) {
	assert.Equal(t, "https://stream.launchdarkly.com", serviceURI("https://app.launchdarkly.com", "stream"))
	assert.Equal(t, "https://events.eu.launchdarkly.com", serviceURI("https://app.eu.launchdarkly.com", "events"))
	assert.Equal(t, "http://localhost:8765", serviceURI("http://localhost:8765", "stream"), "an address without app. is used as it is")
}

func TestConfigSourceFromEnv(t *testing.T) {
	t.Setenv(SDKKeyEnv, "")
	assert.Equal(t, DefaultConfigSource{}, ConfigSourceFromEnv())

	t.Setenv(SDKKeyEnv, "sdk-test")
	t.Setenv(BaseURIEnv, "https://app.example.com/")
	source, ok := ConfigSourceFromEnv().(*AIConfigSource)
	require.True(t, ok)
	assert.Equal(t, SetupAgentConfigKey, source.Key)
	assert.Equal(t, "https://app.example.com", source.Options.BaseURI)
	assert.Equal(t, "https://stream.example.com", source.Options.StreamURI)
}

type inspectCall struct {
	key string
	ctx ldcontext.Context
}

func fakeSource(initErr error, res aiserver.InspectResult) (*AIConfigSource, *[]inspectCall) {
	var calls []inspectCall
	s := &AIConfigSource{
		Key:        "setup",
		initClient: func(context.Context, aiserver.InitOptions) error { return initErr },
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
		initClient: func(context.Context, aiserver.InitOptions) error { inits++; return nil },
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
