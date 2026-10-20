package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/launchdarkly/go-ai-sdk/aiserver"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	ld "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces"
	"github.com/launchdarkly/go-server-sdk/v7/ldcomponents"
)

// SetupAgentConfigKey is the key of the setup-agent AI Config.
const SetupAgentConfigKey = "ldcli-setup-agent"

const (
	contextKind = "ldcli-setup"
	initTimeout = 5 * time.Second
	// environmentIDEnv is the only setting that the AI SDK reads from the
	// environment and that InitOptions cannot set.
	environmentIDEnv = "LD_ENVIRONMENT_ID"
	// RelayEnvironmentIDHeader is the response header in which the relay sends
	// the client-side ID of the environment that it forwards to. The AI SDK
	// sends that ID with its events and spans, so AgentControl can link runs
	// to the config's Monitoring tab. The ID is not a secret, and the relay
	// knows it because it holds that environment's SDK key. Like
	// RelayTokenHeader, the name does not start with X-LD-.
	RelayEnvironmentIDHeader = "LD-Setup-Agent-Environment-ID"
)

// AIConfigSource reads the setup-agent AI Config with the LaunchDarkly AI SDK.
// It evaluates the config locally with the detected facts as context
// attributes, so targeting can give each kind of project its own
// instructions and model.
//
// The SDK reaches the config through the relay in gonfalon, which holds the
// real SDK key. The SDK gets a placeholder key, and a loopback proxy adds the
// user's access token to each request. So no SDK key is on the user's
// machine.
//
// If the SDK cannot start, for example because the relay refuses the request,
// or the config is missing or off, it returns the configuration of Fallback.
// Setup must not fail because the config is unavailable.
type AIConfigSource struct {
	Key string
	// RelayBaseURI is the user's base URI. The relay is at RelayPath under it.
	RelayBaseURI string
	AccessToken  string
	Fallback     ConfigSource

	// checkRelay, initClient, and inspect are fields so that tests can replace
	// them.
	checkRelay func(ctx context.Context, pollingBase string) (string, error)
	initClient func(ctx context.Context, opts aiserver.InitOptions) error
	inspect    func(ctx context.Context, key string, c ldcontext.Context) aiserver.InspectResult

	once    sync.Once
	initErr error
	proxy   *relayProxy
	// started reports that this source started the SDK's global client, so
	// Close must stop it.
	started bool
}

// NewAIConfigSource returns an AIConfigSource for a logged-in user, and
// DefaultConfigSource if there is no access token.
func NewAIConfigSource(baseURI, accessToken string) ConfigSource {
	if strings.TrimSpace(accessToken) == "" || strings.TrimSpace(baseURI) == "" {
		return DefaultConfigSource{}
	}
	return &AIConfigSource{
		Key:          SetupAgentConfigKey,
		RelayBaseURI: baseURI,
		AccessToken:  accessToken,
		Fallback:     DefaultConfigSource{},
	}
}

// relayOptions sets every connection option of the AI SDK to the loopback
// proxy. The SDK fills an empty option from an environment variable, such as
// LD_BASE_URI or OTEL_EXPORTER_OTLP_ENDPOINT. ldcli uses LD_BASE_URI for its own
// API address, so an empty option would connect the SDK to the wrong place.
//
// The paths under the proxy match the relay routes: the SDK adds its own
// paths, such as /all for the stream and /v1/traces for spans.
func relayOptions(proxyBase string) aiserver.InitOptions {
	return aiserver.InitOptions{
		SDKKey:       RelayPlaceholderKey,
		BaseURI:      proxyBase + "/polling",
		StreamURI:    proxyBase + "/stream",
		EventsURI:    proxyBase + "/events",
		OTLPEndpoint: proxyBase + "/otel",
		ServiceName:  "ldcli",
		Environment:  "ldcli-setup",
		WaitFor:      initTimeout,
	}
}

func (s *AIConfigSource) start(ctx context.Context) error {
	proxy, err := startRelayProxy(s.RelayBaseURI, s.AccessToken)
	if err != nil {
		return err
	}
	s.proxy = proxy
	opts := relayOptions(proxy.baseURL)
	check := s.checkRelay
	if check == nil {
		check = checkRelay
	}
	environmentID, err := check(ctx, opts.BaseURI)
	if err != nil {
		return err
	}
	setEnvironmentID(environmentID)
	if s.initClient != nil {
		return s.initClient(ctx, opts)
	}
	if err := initQuietClient(ctx, opts); err != nil {
		return err
	}
	s.started = true
	return nil
}

// setEnvironmentID sets LD_ENVIRONMENT_ID for this process, and clears it when
// the relay sent no ID, so a value in the user's shell for their own app does
// not reach the SDK. OpenCode does not get the variable, because the CLI
// removes LD_ variables from OpenCode's environment.
func setEnvironmentID(id string) {
	if id == "" {
		_ = os.Unsetenv(environmentIDEnv)
		return
	}
	_ = os.Setenv(environmentIDEnv, id)
}

// initQuietClient starts the AI SDK with a LaunchDarkly client that does not
// log. The SDK's own client logs to the terminal, which breaks the wizard's
// full-screen view, and InitOptions cannot turn its logs off.
//
// The client polls and does not open the flag stream. Setup reads the config
// once per run, so one polling request is all it needs, and it does not
// depend on a long-lived connection through the relay and the load balancer.
func initQuietClient(ctx context.Context, opts aiserver.InitOptions) error {
	aiserver.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	client, err := ld.MakeCustomClient(opts.SDKKey, ld.Config{
		ServiceEndpoints: interfaces.ServiceEndpoints{
			Polling:   opts.BaseURI,
			Streaming: opts.StreamURI,
			Events:    opts.EventsURI,
		},
		DataSource:       ldcomponents.PollingDataSource(),
		Logging:          ldcomponents.NoLogging(),
		DiagnosticOptOut: true,
	}, opts.WaitFor)
	if err != nil {
		if client != nil {
			_ = client.Close()
		}
		return err
	}
	if _, err := aiserver.InitWithClient(ctx, client, opts); err != nil {
		_ = client.Close()
		return err
	}
	return nil
}

func (s *AIConfigSource) Resolve(ctx context.Context, task Task) (Config, error) {
	s.once.Do(func() {
		s.initErr = s.start(ctx)
		if s.initErr != nil {
			s.closeProxy(ctx)
		}
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
	return configFromAI(res.Config, task), nil
}

func (s *AIConfigSource) fallback(ctx context.Context, task Task) (Config, error) {
	if s.Fallback == nil {
		return DefaultConfigSource{}.Resolve(ctx, task)
	}
	return s.Fallback.Resolve(ctx, task)
}

// Close flushes the SDK events through the relay, stops the SDK client, and
// then stops the proxy.
func (s *AIConfigSource) Close(ctx context.Context) error {
	var err error
	if s.started {
		err = aiserver.Shutdown(ctx)
		s.started = false
	}
	s.closeProxy(ctx)
	return err
}

func (s *AIConfigSource) closeProxy(ctx context.Context) {
	if s.proxy != nil {
		_ = s.proxy.close(ctx)
		s.proxy = nil
	}
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

// placeholderPattern matches a {{name}} variable, with the same rule as
// aiserver.ParseTemplate.
var placeholderPattern = regexp.MustCompile(`\{\{[\w.]+\}\}`)

// configFromAI turns an AI Config variation into an agent configuration. An
// agent-mode config has instructions. A completion-mode config has messages,
// and its system messages become the instructions.
//
// If the instructions use at least one {{variable}}, they describe the task
// themselves: the CLI fills the variables from TaskVariables and sends only
// StartMessage. Instructions without variables keep the CLI's own task
// message, so a config can add guidance without taking over the task.
func configFromAI(c *aiserver.AiConfig, task Task) Config {
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
	} else if placeholderPattern.MatchString(cfg.Instructions) {
		cfg.Instructions = aiserver.ParseTemplate(cfg.Instructions, TaskVariables(task))
		cfg.Task = StartMessage
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

// RelayError is a refusal from the relay, for example because the kill switch
// is off or the access token is not valid.
type RelayError struct {
	Status  int
	Code    string
	Message string
}

func (e *RelayError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("the setup agent service refused the request (%s): %s", e.Code, e.Message)
	}
	if e.Code != "" {
		return fmt.Sprintf("the setup agent service refused the request (%s)", e.Code)
	}
	return fmt.Sprintf("the setup agent service returned HTTP %d", e.Status)
}

// checkRelay makes one polling request through the proxy before the SDK
// starts. The Go server SDK retries a refused stream until its start timeout
// ends, so without this check a refusal would delay every setup run. It
// returns the environment ID that the relay sends, or an empty string.
func checkRelay(ctx context.Context, pollingBase string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollingBase+"/sdk/latest-all", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", RelayPlaceholderKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach the setup agent service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 {
		return strings.TrimSpace(resp.Header.Get(RelayEnvironmentIDHeader)), nil
	}
	refusal := &RelayError{Status: resp.StatusCode}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body) == nil {
		refusal.Code, refusal.Message = body.Code, body.Message
	}
	return "", refusal
}
