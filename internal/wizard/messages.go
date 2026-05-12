package wizard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/launchdarkly/ldcli/internal/environments"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/flags"
)

// errMsg is sent when an error occurs in any step.
type errMsg struct {
	err error
}

func sendErrMsg(err error) tea.Cmd {
	return func() tea.Msg {
		return errMsg{err: err}
	}
}

// choseSDKMsg is sent when the user selects an SDK.
type choseSDKMsg struct {
	sdk sdkDetail
}

func chooseSDKCmd(sdk sdkDetail) tea.Cmd {
	return func() tea.Msg {
		if sdk.url == "" {
			sdk.url = fmt.Sprintf("https://github.com/launchdarkly/hello-%s", sdk.id)
		}
		return choseSDKMsg{sdk: sdk}
	}
}

// installedSDKMsg is sent when the SDK install command finishes successfully.
type installedSDKMsg struct {
	output string
}

// installSkippedMsg is sent when there is no install command for the SDK (e.g. Java).
type installSkippedMsg struct{}

// installErrMsg is sent when the install command fails.
type installErrMsg struct {
	err error
}

// runInstall executes the install command in dir and returns the result as a message.
func runInstall(args []string, dir string) tea.Cmd {
	return func() tea.Msg {
		if len(args) == 0 {
			return installSkippedMsg{}
		}
		cmd := exec.Command(args[0], args[1:]...) //nolint:gosec
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return installErrMsg{err: fmt.Errorf("%w\n%s", err, string(out))}
		}
		return installedSDKMsg{output: string(out)}
	}
}

// InstallArgs returns the shell command to install the given SDK using packageManager.
// Returns nil for SDKs that require manual installation (e.g. Java).
func InstallArgs(sdkID, packageManager string) []string {
	switch sdkID {
	case "react-client-sdk":
		return []string{packageManager, "install", "launchdarkly-react-client-sdk"}
	case "node-server":
		return []string{packageManager, "install", "@launchdarkly/node-server-sdk"}
	case "python-server-sdk":
		if packageManager == "" {
			packageManager = "pip"
		}
		return []string{packageManager, "install", "launchdarkly-server-sdk"}
	case "go-server-sdk":
		return []string{"go", "get", "github.com/launchdarkly/go-server-sdk/v7"}
	default:
		// Java and others require manual steps
		return nil
	}
}

// flag holds the key and name of a created feature flag.
type flag struct {
	key  string
	name string
}

// createdFlagMsg is sent when a flag is successfully created (or already exists).
type createdFlagMsg struct {
	flag         flag
	existingFlag bool
}

// confirmedFlagMsg is sent when the user confirms the created flag and continues.
type confirmedFlagMsg struct {
	flag flag
}

func confirmedFlagCmd(f flag) tea.Cmd {
	return func() tea.Msg {
		return confirmedFlagMsg{flag: f}
	}
}

type msgRequestError struct {
	code    string
	message string
}

func newMsgRequestError(errStr string) (msgRequestError, error) {
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(errStr), &e); err != nil {
		return msgRequestError{}, err
	}
	return msgRequestError{code: e.Code, message: e.Message}, nil
}

func (e msgRequestError) Error() string  { return e.message }
func (e msgRequestError) IsConflict() bool { return e.code == "conflict" }

const defaultProjKey = "default"
const defaultEnvKey = "test"
const defaultFlagName = "My New Flag"

func createFlag(client flags.Client, accessToken, baseURI, flagName, flagKey string) tea.Cmd {
	return func() tea.Msg {
		var existingFlag bool
		_, err := client.Create(
			context.Background(),
			accessToken,
			baseURI,
			flagName,
			flagKey,
			defaultProjKey,
		)
		if err != nil {
			msgRequestErr, parseErr := newMsgRequestError(err.Error())
			if parseErr != nil {
				return errMsg{err: parseErr}
			}
			if !msgRequestErr.IsConflict() {
				return errMsg{
					err: errors.NewError(fmt.Sprintf(
						"Error creating flag: %s. Press \"ctrl + c\" to quit.",
						msgRequestErr.message,
					)),
				}
			}
			existingFlag = true
		}
		return createdFlagMsg{flag: flag{key: flagKey, name: flagName}, existingFlag: existingFlag}
	}
}

// envData holds the SDK credentials for an environment.
type envData struct {
	sdkKey       string
	mobileKey    string
	clientSideId string
}

// fetchedEnvMsg is sent when the environment credentials are retrieved.
type fetchedEnvMsg struct {
	env envData
}

func fetchEnv(client environments.Client, accessToken, baseURI string) tea.Cmd {
	return func() tea.Msg {
		response, err := client.Get(
			context.Background(),
			accessToken,
			baseURI,
			defaultEnvKey,
			defaultProjKey,
		)
		if err != nil {
			return errMsg{err: err}
		}

		var resp struct {
			SDKKey       string `json:"apiKey"`
			ClientSideId string `json:"_id"`
			MobileKey    string `json:"mobileKey"`
		}
		if err = json.Unmarshal(response, &resp); err != nil {
			return errMsg{err: err}
		}

		return fetchedEnvMsg{env: envData{
			sdkKey:       resp.SDKKey,
			mobileKey:    resp.MobileKey,
			clientSideId: resp.ClientSideId,
		}}
	}
}

// wroteInitFileMsg is sent after the init file is successfully written to disk.
type wroteInitFileMsg struct {
	filename string
}

func writeInitFile(filename, content string) tea.Cmd {
	return func() tea.Msg {
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil { //nolint:gosec
			return errMsg{err: fmt.Errorf("failed to write %s: %w", filename, err)}
		}
		return wroteInitFileMsg{filename: filename}
	}
}
