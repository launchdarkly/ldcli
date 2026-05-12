package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/launchdarkly/sdk-meta/api/sdkmeta"

	"github.com/launchdarkly/ldcli/internal/environments"
)

const stepCountHeight = 4

type injectCodeModel struct {
	accessToken        string
	baseURI            string
	environmentsClient environments.Client
	sdk                sdkDetail
	flagKey            string
	workDir            string
	env                *envData
	filename           string
	content            string
	err                error
	writing            bool
	spinner            spinner.Model
	viewport           viewport.Model
	help               help.Model
	helpKeys           keyMap
	height             int
	width              int
}

func NewInjectCodeModel(
	environmentsClient environments.Client,
	accessToken, baseURI string,
	height, width int,
	sdk sdkDetail,
	flagKey, workDir string,
) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Points

	m := injectCodeModel{
		accessToken:        accessToken,
		baseURI:            baseURI,
		environmentsClient: environmentsClient,
		sdk:                sdk,
		flagKey:            flagKey,
		workDir:            workDir,
		spinner:            s,
		help:               help.New(),
		helpKeys: keyMap{
			CursorDown: BindingCursorDown,
			CursorUp:   BindingCursorUp,
			Quit:       BindingQuit,
		},
		height: height,
		width:  width,
	}

	vp := viewport.New(width, m.viewportHeight(height))
	m.viewport = vp

	return m
}

func (m injectCodeModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		fetchEnv(m.environmentsClient, m.accessToken, m.baseURI),
	)
}

func (m injectCodeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		m.viewport.Width = msg.Width
		m.viewport.Height = m.viewportHeight(msg.Height)
	case fetchedEnvMsg:
		m.env = &msg.env
		m.help.ShowAll = true
		m.filename = InitFilename(m.sdk.id, m.workDir)
		m.content = BuildSnippet(m.sdk.id, m.flagKey)
		rendered, err := m.renderContent()
		if err != nil {
			return m, sendErrMsg(err)
		}
		m.viewport.SetContent(rendered)
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, pressableKeys.Enter):
			if m.env != nil && !m.writing {
				m.writing = true
				cmd = writeInitFile(m.filename, m.content)
			}
		default:
			m.viewport, cmd = m.viewport.Update(msg)
		}
	case wroteInitFileMsg:
		// container handles this
	case errMsg:
		m.err = msg.err
	}
	return m, cmd
}

func (m injectCodeModel) View() string {
	if m.err != nil {
		return footerView(m.help.View(m.helpKeys), m.err)
	}

	if m.env == nil {
		return m.spinner.View() + " Preparing init code...\n" +
			footerView(m.help.View(m.helpKeys), nil)
	}

	return m.viewport.View() + m.footerView()
}

func (m injectCodeModel) footerView() string {
	style := lipgloss.NewStyle().
		BorderForeground(lipgloss.Color("62")).
		BorderStyle(lipgloss.ThickBorder()).
		BorderTop(true).
		Width(m.width - 2)

	envVarLine := fmt.Sprintf("Set %s=%s", envVarName(m.sdk), envVarValue(m.sdk, m.env))
	return style.Render(
		"\n" + envVarLine + "\n(press enter to write " + filepath.Base(m.filename) + ")" +
			footerView(m.help.View(m.helpKeys), nil),
	)
}

func (m injectCodeModel) renderContent() (string, error) {
	header := fmt.Sprintf("## Init code for %s\n\nFile: `%s`\n\n",
		m.sdk.displayName, m.filename)
	md := header + fenceSnippet(m.sdk.id, m.content)

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(m.viewport.Width),
	)
	if err != nil {
		return "", err
	}
	return renderer.Render(md)
}

func (m injectCodeModel) viewportHeight(h int) int {
	footer := lipgloss.Height(m.footerView())
	if footer < 4 {
		footer = 4
	}
	result := h - footer - stepCountHeight
	if result < 1 {
		result = 1
	}
	return result
}

// envVarName returns the environment variable name for the SDK key type.
func envVarName(sdk sdkDetail) string {
	if sdk.sdkType == sdkmeta.ClientSideType {
		return "REACT_APP_LD_CLIENT_SIDE_ID"
	}
	return "LAUNCHDARKLY_SDK_KEY"
}

// envVarValue returns the actual credential value to set, based on SDK type.
func envVarValue(sdk sdkDetail, env *envData) string {
	if env == nil {
		return "<your-sdk-key>"
	}
	if sdk.sdkType == sdkmeta.ClientSideType {
		return env.clientSideId
	}
	return env.sdkKey
}

// InitFilename returns the path of the init file to write for a given SDK.
func InitFilename(sdkID, workDir string) string {
	switch sdkID {
	case "go-server-sdk":
		return filepath.Join(workDir, "launchdarkly.go")
	case "python-server-sdk":
		return filepath.Join(workDir, "launchdarkly_client.py")
	case "node-server":
		return filepath.Join(workDir, "launchdarkly_client.js")
	case "react-client-sdk":
		srcDir := filepath.Join(workDir, "src")
		if _, err := os.Stat(srcDir); err == nil {
			return filepath.Join(srcDir, "launchdarkly.tsx")
		}
		return filepath.Join(workDir, "launchdarkly.tsx")
	case "java-server-sdk":
		javaDir := filepath.Join(workDir, "src", "main", "java")
		if _, err := os.Stat(javaDir); err == nil {
			return filepath.Join(javaDir, "LaunchDarklyConfig.java")
		}
		return filepath.Join(workDir, "LaunchDarklyConfig.java")
	default:
		return filepath.Join(workDir, "launchdarkly_init.txt")
	}
}

// BuildSnippet returns the init file content for the given SDK with the real flag key inserted.
func BuildSnippet(sdkID, flagKey string) string {
	raw := rawSnippet(sdkID)
	return strings.ReplaceAll(raw, "MY_FLAG_KEY", flagKey)
}

// fenceSnippet wraps content in the appropriate markdown code fence for rendering.
func fenceSnippet(sdkID, content string) string {
	lang := snippetLang(sdkID)
	return fmt.Sprintf("```%s\n%s\n```\n", lang, content)
}

func snippetLang(sdkID string) string {
	switch sdkID {
	case "go-server-sdk":
		return "go"
	case "python-server-sdk":
		return "python"
	case "node-server":
		return "javascript"
	case "react-client-sdk":
		return "tsx"
	case "java-server-sdk":
		return "java"
	default:
		return ""
	}
}

func rawSnippet(sdkID string) string {
	switch sdkID {
	case "go-server-sdk":
		return goSnippet
	case "python-server-sdk":
		return pythonSnippet
	case "node-server":
		return nodeSnippet
	case "react-client-sdk":
		return reactSnippet
	case "java-server-sdk":
		return javaSnippet
	default:
		return "// No template available for this SDK.\n// Visit https://docs.launchdarkly.com for setup instructions.\n"
	}
}

const goSnippet = `package main

import (
	"fmt"
	"os"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	ld "github.com/launchdarkly/go-server-sdk/v7"
)

// initLaunchDarkly creates a LaunchDarkly client.
// Set the LAUNCHDARKLY_SDK_KEY environment variable before running.
func initLaunchDarkly() (*ld.LDClient, error) {
	sdkKey := os.Getenv("LAUNCHDARKLY_SDK_KEY")
	return ld.MakeClient(sdkKey, 5*time.Second)
}

func evaluateFlag(client *ld.LDClient, userKey string) bool {
	ctx := ldcontext.NewBuilder(userKey).Build()
	value, err := client.BoolVariation("MY_FLAG_KEY", ctx, false)
	if err != nil {
		fmt.Printf("Error evaluating flag: %v\n", err)
	}
	fmt.Printf("Feature flag 'MY_FLAG_KEY' evaluates to: %v\n", value)
	return value
}
`

const pythonSnippet = `import os
import ldclient
from ldclient import Context
from ldclient.config import Config


def init_launchdarkly():
    """Initialize the LaunchDarkly client.
    Set the LAUNCHDARKLY_SDK_KEY environment variable before running.
    """
    sdk_key = os.getenv("LAUNCHDARKLY_SDK_KEY")
    ldclient.set_config(Config(sdk_key))
    return ldclient.get()


def evaluate_flag(client, user_key: str) -> bool:
    context = Context.builder(user_key).kind("user").build()
    value = client.variation("MY_FLAG_KEY", context, False)
    print(f"Feature flag 'MY_FLAG_KEY' evaluates to: {value}")
    return value
`

const nodeSnippet = `const LaunchDarkly = require('@launchdarkly/node-server-sdk');

// Set the LAUNCHDARKLY_SDK_KEY environment variable before running.
const sdkKey = process.env.LAUNCHDARKLY_SDK_KEY;
const ldClient = LaunchDarkly.init(sdkKey);

async function evaluateFlag(userKey) {
  await ldClient.waitForInitialization();
  const context = { kind: 'user', key: userKey };
  const value = await ldClient.variation('MY_FLAG_KEY', context, false);
  console.log("Feature flag 'MY_FLAG_KEY' evaluates to: " + value);
  return value;
}

module.exports = { ldClient, evaluateFlag };
`

const reactSnippet = `import { asyncWithLDProvider } from 'launchdarkly-react-client-sdk';

// Wrap your root component with LDProvider in index.tsx.
// Set REACT_APP_LD_CLIENT_SIDE_ID in your .env file.
export const getLDProvider = async () =>
  asyncWithLDProvider({
    clientSideID: process.env.REACT_APP_LD_CLIENT_SIDE_ID ?? '',
    context: {
      kind: 'user',
      key: 'example-user',
    },
  });

// To evaluate MY_FLAG_KEY in a component:
// import { useFlags } from 'launchdarkly-react-client-sdk';
// const { myFlagKey } = useFlags();
// Note: flag keys are camelCased in useFlags.
`

const javaSnippet = `import com.launchdarkly.sdk.*;
import com.launchdarkly.sdk.server.*;

/**
 * LaunchDarkly client helper.
 * Set the LAUNCHDARKLY_SDK_KEY environment variable before running.
 * Add to pom.xml: com.launchdarkly:launchdarkly-java-server-sdk:7.4.0
 */
public class LaunchDarklyConfig {

    private static LDClient client;

    public static LDClient getClient() {
        if (client == null) {
            String sdkKey = System.getenv("LAUNCHDARKLY_SDK_KEY");
            client = new LDClient(sdkKey);
        }
        return client;
    }

    public static boolean evaluateFlag(String userKey) {
        LDContext context = LDContext.builder(userKey).build();
        boolean value = getClient().boolVariation("MY_FLAG_KEY", context, false);
        System.out.println("Feature flag 'MY_FLAG_KEY' evaluates to: " + value);
        return value;
    }
}
`
