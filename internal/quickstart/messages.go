package quickstart

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"ldcli/internal/environments"
	"ldcli/internal/flags"
	"net/http"

	tea "github.com/charmbracelet/bubbletea"
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
			return sendErr(err)
		}
		return toggledFlagMsg{}
	}
}

type createdFlagMsg struct {
	flagKey string
}

func sendCreateFlagMsg(client flags.Client, accessToken, baseUri, flagName, flagKey, projKey string) tea.Cmd {
	return func() tea.Msg {
		_, err := client.Create(
			context.Background(),
			accessToken,
			baseUri,
			flagName,
			flagKey,
			projKey,
		)
		if err != nil {
			return sendErr(err)
		}
		//if err != nil {
		//	m.err = err
		//	// TODO: we may want a more robust error type so we don't need to do this
		//	var e struct {
		//		Code    string `json:"code"`
		//		Message string `json:"message"`
		//	}
		//	_ = json.Unmarshal([]byte(m.err.Error()), &e)
		//	switch {
		//	case e.Code == "unauthorized":
		//		m.quitting = true
		//		m.quitMsg = "Your API key is unauthorized. Try another API key or speak to a LaunchDarkly account administrator."
		//
		//		return m, tea.Quit
		//	case e.Code == "forbidden":
		//		m.quitting = true
		//		m.quitMsg = "You lack access to complete this action. Try authenticating with elevated access or speak to a LaunchDarkly account administrator."
		//
		//		return m, tea.Quit
		//	}
		//
		//	return m, nil

		return createdFlagMsg{flagKey: flagKey}
	}
}

type fetchedSDKInstructions struct {
	instructions []byte
}

type choseSDKMsg struct {
	canonicalName string
	displayName   string
	sdkKind       string
	url           string
}

func sendChoseSDKMsg(sdk sdkDetail) tea.Cmd {
	return func() tea.Msg {
		if sdk.url == "" {
			sdk.url = fmt.Sprintf("https://raw.githubusercontent.com/launchdarkly/hello-%s/main/README.md", sdk.canonicalName)
		}

		return choseSDKMsg{
			canonicalName: sdk.canonicalName,
			displayName:   sdk.displayName,
			url:           sdk.url,
			sdkKind:       sdk.kind,
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
