package quickstart

import (
	"context"
	"encoding/json"
	"fmt"
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

type toggledFlagMsg struct{}

func toggleFlag(client flags.Client, accessToken, baseUri, flagKey string, enabled bool) tea.Cmd {
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
			return errMsg{err: err}
		}

		return toggledFlagMsg{}
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
			var e struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal([]byte(err.Error()), &e)
			existingFlag = e.Code == "conflict"
			if !existingFlag {
				return errMsg{err: errors.NewError(fmt.Sprintf("Error creating flag: %s. Press \"ctrl + c\" to quit.", e.Message))}
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
			sdk.url = fmt.Sprintf("https://github.com/launchdarkly/hello-%s", sdk.canonicalName)
		}

		return choseSDKMsg{
			sdk: sdk,
		}
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
	environment environment
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
			return errMsg{err: err}
		}

		return fetchedEnvMsg{environment: environment{
			sdkKey:       resp.SDKKey,
			mobileKey:    resp.MobileKey,
			clientSideId: resp.ClientSideId,
		}}

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
