package quickstart

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"ldcli/internal/environments"
	"ldcli/internal/errors"
	"ldcli/internal/flags"
)

// errMsg is sent when there is an error in one of the steps that the container model needs to
// know about.
type errMsg struct {
	err error
}

func sendErrMsg(err error) tea.Cmd {
	return func() tea.Msg {
		return errMsg{err: err}
	}
}

type toggledFlagMsg struct {
	hasToggleConflict bool
}

func toggleFlag(client flags.Client, accessToken, baseUri, flagKey string, enabled bool) tea.Cmd {
	var hasToggleConflict bool
	return func() tea.Msg {
		_, err := client.Update(
			context.Background(),
			accessToken,
			baseUri,
			flagKey,
			defaultProjKey,
			flags.BuildToggleFlagPatch(defaultEnvKey, enabled),
		)
		if err != nil {
			var errorMsg errMsg
			hasToggleConflict, errorMsg = handleError(err, "Error toggling flag")
			if !hasToggleConflict {
				return errorMsg
			}
		}

		return toggledFlagMsg{hasToggleConflict: hasToggleConflict}
	}
}

type createdFlagMsg struct {
	flag             flag
	existingFlagUsed bool
}

type confirmedFlagMsg struct {
	flag flag
}

func confirmedFlag(flag flag) tea.Cmd {
	return func() tea.Msg {
		return confirmedFlagMsg{flag}
	}
}

func createFlag(client flags.Client, accessToken, baseUri, flagName, flagKey, projKey string) tea.Cmd {
	return func() tea.Msg {
		var existingFlag bool

		_, err := client.Create(
			context.Background(),
			accessToken,
			baseUri,
			flagName,
			flagKey,
			projKey,
		)
		if err != nil {
			var errorMsg errMsg
			existingFlag, errorMsg = handleError(err, "Error creating flag")
			if !existingFlag {
				return errorMsg
			}
		}

		return createdFlagMsg{flag: flag{
			key:  flagKey,
			name: flagName,
		}, existingFlagUsed: existingFlag}
	}
}

type fetchedSDKInstructionsMsg struct {
	instructions []byte
}

type choseSDKMsg struct {
	sdk sdkDetail
}

func chooseSDK(sdk sdkDetail) tea.Cmd {
	return func() tea.Msg {
		if sdk.url == "" {
			sdk.url = fmt.Sprintf("https://raw.githubusercontent.com/launchdarkly/hello-%s/main/README.md", sdk.canonicalName)
		}

		return choseSDKMsg{
			sdk: sdk,
		}
	}
}

func fetchSDKInstructions(url string) tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get(url)
		if err != nil {
			return errMsg{err: err}
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errMsg{err: err}
		}

		if resp.StatusCode == 404 {
			// m.sdk = msg.name
			return noInstructionsMsg{}
		}

		return fetchedSDKInstructionsMsg{instructions: body}
	}
}

func readSDKInstructions(filename string) tea.Cmd {
	return func() tea.Msg {
		content, err := os.ReadFile(fmt.Sprintf("internal/sdks/sdk_instructions/%s.md", filename))
		if err != nil {
			return errMsg{err: err}
		}

		return fetchedSDKInstructionsMsg{instructions: content}
	}
}

type showToggleFlagMsg struct{}

func showToggleFlag() tea.Cmd {
	return func() tea.Msg {
		return showToggleFlagMsg{}
	}
}

type fetchedEnvMsg struct {
	clientSideId string
	mobileKey    string
	sdkKey       string
}

func fetchEnv(
	client environments.Client,
	accessToken string,
	baseUri string,
	key string,
	projKey string,
) tea.Cmd {
	return func() tea.Msg {
		response, err := client.Get(context.Background(), accessToken, baseUri, key, projKey)
		if err != nil {
			return errMsg{err: err}
		}

		var resp struct {
			SDKKey       string `json:"apiKey"`
			ClientSideId string `json:"_id"`
			MobileKey    string `json:"mobileKey"`
		}
		err = json.Unmarshal(response, &resp)
		if err != nil {
			var e struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal([]byte(err.Error()), &e)
			return errMsg{err: errors.NewError(fmt.Sprintf("Error fetching environment: %s. Press \"ctrl + c\" to quit.", e.Message))}
		}

		return fetchedEnvMsg{
			clientSideId: resp.ClientSideId,
			sdkKey:       resp.SDKKey,
			mobileKey:    resp.MobileKey,
		}
	}
}

// noInstructionsMsg is sent when we can't find the SDK instructions repository for the given SDK.
type noInstructionsMsg struct{}

type selectedSDKMsg struct {
	index int
}

func selectedSDK(index int) tea.Cmd {
	return func() tea.Msg {
		return selectedSDKMsg{index: index}
	}
}

func handleError(err error, message string) (bool, errMsg) {
	var errorMsg errMsg
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal([]byte(err.Error()), &e)
	conflict := e.Code == "conflict"
	if !conflict {
		errorMsg = errMsg{err: errors.NewError(fmt.Sprintf("%s: %s. Press \"ctrl + c\" to quit.", message, e.Message))}
	}

	return conflict, errorMsg
}
