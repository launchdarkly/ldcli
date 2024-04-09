package quickstart

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

func sendErr(err error) tea.Cmd {
	return func() tea.Msg {
		return errMsg{err: err}
	}
}

type toggledFlagMsg struct{}

func sendToggleFlagMsg(client flags.Client, accessToken, baseUri, flagKey string, enabled bool) tea.Cmd {
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

func sendConfirmedFlagMsg(flag flag) tea.Cmd {
	return func() tea.Msg {
		return confirmedFlagMsg{flag}
	}
}

func sendCreateFlagMsg(client flags.Client, accessToken, baseUri, flagName, flagKey, projKey string) tea.Cmd {
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

type fetchedSDKInstructions struct {
	instructions []byte
}

type choseSDKMsg struct {
	sdk sdkDetail
}

func sendChoseSDKMsg(sdk sdkDetail) tea.Cmd {
	return func() tea.Msg {
		if sdk.url == "" {
			sdk.url = fmt.Sprintf("https://raw.githubusercontent.com/launchdarkly/hello-%s/main/README.md", sdk.canonicalName)
		}

		return choseSDKMsg{
			sdk: sdk,
		}
	}
}

func sendFetchSDKInstructionsMsg(url string) tea.Cmd {
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

		return fetchedSDKInstructions{instructions: body}
	}
}

type showToggleFlagMsg struct{}

func sendShowToggleFlagMsg() tea.Cmd {
	return func() tea.Msg {
		return showToggleFlagMsg{}
	}
}

func sendEnableMouseCellMotionMsg() tea.Cmd {
	return func() tea.Msg {
		return tea.EnableMouseCellMotion()
	}
}

type fetchedEnv struct {
	sdkKey string
}

func sendFetchEnv(accessToken string, baseUri string, key string, projKey string) tea.Cmd {
	return func() tea.Msg {
		client := environments.NewClient("0.2.0")
		response, err := client.Get(context.Background(), accessToken, baseUri, key, projKey)
		if err != nil {
			return errMsg{err: err}
		}

		var resp struct {
			SDKKey string `json:"apiKey"`
		}
		err = json.Unmarshal(response, &resp)
		if err != nil {
			return errMsg{err: err}
		}

		return fetchedEnv{sdkKey: resp.SDKKey}
	}
}

// noInstructionsMsg is sent when we can't find the SDK instructions repository for the given SDK.
type noInstructionsMsg struct{}

type selectedSDKMsg struct {
	index int
}

func sendSelectedSDKMsg(index int) tea.Cmd {
	return func() tea.Msg {
		return selectedSDKMsg{index: index}
	}
}
