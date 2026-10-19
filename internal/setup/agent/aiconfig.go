package agent

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/launchdarkly/go-ai-sdk/aiserver"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
)

// SetupAgentConfigKey is the key of the setup-agent AI Config.
const SetupAgentConfigKey = "ldcli-setup-agent"

// Environment variables for development against catfood. They are temporary:
// the relay in gonfalon will replace them, and then no SDK key is on the
// user's machine. They do not start with LD_, because ldcli and the AI SDK
// both read LD_ variables for other purposes.
const (
	// SDKKeyEnv turns on the AI Config source. Its value is the SDK key of the
	// environment that holds the setup-agent AI Config.
	SDKKeyEnv = "LDCLI_SETUP_AGENT_SDK_KEY"
	// BaseURIEnv is the app URL of the LaunchDarkly instance that holds the AI
	// Config. The default is catfood.
	BaseURIEnv = "LDCLI_SETUP_AGENT_BASE_URI"
	// OTLPEndpointEnv is the trace endpoint. The default is the AI SDK default.
	OTLPEndpointEnv = "LDCLI_SETUP_AGENT_OTLP_ENDPOINT"
)

const (
	defaultSetupAgentBaseURI = "https://app.ld.catamorphic.com"
	contextKind              = "ldcli-setup"
	initTimeout              = 5 * time.Second
)

// AIConfigSource reads the setup-agent AI Config with the LaunchDarkly AI SDK.
// It evaluates the config locally with the detected facts as context
// attributes, so targeting can give each kind of project its own
// instructions and model.
//
// If the SDK cannot start, or the config is missing or off, it returns the
// configuration of Fallback. Setup must not fail because the config is
// unavailable.
type AIConfigSource struct {
	Key      string
	Options  aiserver.InitOptions
	Fallback ConfigSource

	// initClient and inspect are fields so that tests can replace them.
	initClient func(ctx context.Context, opts aiserver.InitOptions) error
	inspect    func(ctx context.Context, key string, c ldcontext.Context) aiserver.InspectResult

	once    sync.Once
	initErr error
	// started reports that this source started the SDK's global client, so
	// Close must stop it.
	started bool
}

// ConfigSourceFromEnv returns an AIConfigSource if SDKKeyEnv is set, and
// DefaultConfigSource if not.
func ConfigSourceFromEnv() ConfigSource {
	key := strings.TrimSpace(os.Getenv(SDKKeyEnv))
	if key == "" {
		return DefaultConfigSource{}
	}
	return &AIConfigSource{
		Key: SetupAgentConfigKey,
		Options: initOptions(key,
			strings.TrimSpace(os.Getenv(BaseURIEnv)),
			strings.TrimSpace(os.Getenv(OTLPEndpointEnv))),
		Fallback: DefaultConfigSource{},
	}
}

// initOptions sets every connection option of the AI SDK. The SDK fills an
// empty option from an environment variable, such as LD_BASE_URI or
// OTEL_EXPORTER_OTLP_ENDPOINT. ldcli uses LD_BASE_URI for its own API
// address, so an empty option would connect the SDK to the wrong place.
func initOptions(sdkKey, baseURI, otlpEndpoint string) aiserver.InitOptions {
	if baseURI == "" {
		baseURI = defaultSetupAgentBaseURI
	}
	baseURI = strings.TrimRight(baseURI, "/")
	if otlpEndpoint == "" {
		otlpEndpoint = aiserver.DefaultOTLPEndpoint
	}
	return aiserver.InitOptions{
		SDKKey:       sdkKey,
		BaseURI:      baseURI,
		StreamURI:    serviceURI(baseURI, "stream"),
		EventsURI:    serviceURI(baseURI, "events"),
		OTLPEndpoint: otlpEndpoint,
		ServiceName:  "ldcli",
		Environment:  "ldcli-setup",
		WaitFor:      initTimeout,
	}
}

// serviceURI derives a service address from the app address of a LaunchDarkly
// instance. An instance serves the SDK streams and events on subdomains named
// for the service, for example https://stream.ld.catamorphic.com for
// https://app.ld.catamorphic.com. An address that does not start with app. is
// used as it is.
func serviceURI(baseURI, service string) string {
	scheme, host, ok := strings.Cut(baseURI, "://")
	if !ok || !strings.HasPrefix(host, "app.") {
		return baseURI
	}
	return scheme + "://" + service + "." + strings.TrimPrefix(host, "app.")
}

func (s *AIConfigSource) Resolve(ctx context.Context, task Task) (Config, error) {
	s.once.Do(func() {
		if s.initClient != nil {
			s.initErr = s.initClient(ctx, s.Options)
			return
		}
		_, s.initErr = aiserver.InitClient(ctx, s.Options)
		s.started = s.initErr == nil
	})
	if s.initErr != nil {
		return s.fallback(ctx, task)
	}

	inspect := s.inspect
	if inspect == nil {
		inspect = aiserver.InspectConfig
	}
	key := s.Key
	if key == "" {
		key = SetupAgentConfigKey
	}
	res := inspect(ctx, key, setupContext(task))
	if !res.Enabled || res.Config == nil {
		return s.fallback(ctx, task)
	}
	return configFromAI(res.Config), nil
}

func (s *AIConfigSource) fallback(ctx context.Context, task Task) (Config, error) {
	if s.Fallback == nil {
		return DefaultConfigSource{}.Resolve(ctx, task)
	}
	return s.Fallback.Resolve(ctx, task)
}

// Close flushes the SDK events and stops the SDK client.
func (s *AIConfigSource) Close(ctx context.Context) error {
	if !s.started {
		return nil
	}
	return aiserver.Shutdown(ctx)
}

// setupContext is the evaluation context for one run. Its attributes are the
// detected facts that targeting rules can match. It has no file paths and no
// names from the project, because the context goes to LaunchDarkly in
// analytics events.
func setupContext(t Task) ldcontext.Context {
	key, err := randomHex(16)
	if err != nil {
		key = "ldcli-setup"
	}
	return ldcontext.NewBuilder(key).
		Kind(contextKind).
		Anonymous(true).
		SetString("sdkId", t.SDKID).
		SetString("language", t.Language).
		SetString("framework", t.Framework).
		SetString("packageManager", t.PackageManager).
		SetBool("hasEntryPoint", t.EntryPoint != "").
		SetBool("sdkInstalled", t.SDKInstalled).
		Build()
}

// openCodeProviders maps AI Config provider names to OpenCode provider IDs
// where the two are different.
var openCodeProviders = map[string]string{
	"bedrock": "amazon-bedrock",
	"gemini":  "google",
}

// configFromAI turns an AI Config variation into an agent configuration. An
// agent-mode config has instructions. A completion-mode config has messages,
// and its system messages become the instructions.
func configFromAI(c *aiserver.AiConfig) Config {
	cfg := Config{Instructions: c.Instructions, ModelID: c.Model.Name}
	if cfg.Instructions == "" {
		var system []string
		for _, m := range c.Messages {
			if m.Role == "system" && m.Text != "" {
				system = append(system, m.Text)
			}
		}
		cfg.Instructions = strings.Join(system, "\n\n")
	}
	if cfg.Instructions == "" {
		cfg.Instructions = DefaultInstructions
	}
	provider := strings.ToLower(strings.TrimSpace(c.Provider.Name))
	if mapped, ok := openCodeProviders[provider]; ok {
		provider = mapped
	}
	cfg.ProviderID = provider
	// OpenCode needs both values to pick a model. With only one, OpenCode
	// uses its default model.
	if cfg.ProviderID == "" || cfg.ModelID == "" {
		cfg.ProviderID, cfg.ModelID = "", ""
	}
	return cfg
}
