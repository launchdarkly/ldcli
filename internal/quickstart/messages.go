package quickstart

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"ldcli/internal/environments"
	"log"
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

type fetchSDKInstructionsMsg struct {
	canonicalName string
	flagKey       string
	name          string
	url           string
}

type fetchedSDKInstructions struct {
	instructions []byte
}

type choseSDKMsg struct {
	canonicalName string
	displayName   string
	flagKey       string
	url           string
}

func sendChoseSDKMsg(sdk sdkDetail, flagKey string) tea.Cmd {
	return func() tea.Msg {
		if sdk.url == "" {
			sdk.url = fmt.Sprintf("https://raw.githubusercontent.com/launchdarkly/hello-%s/main/README.md", sdk.canonicalName)
		}

		return choseSDKMsg{
			canonicalName: sdk.canonicalName,
			displayName:   sdk.displayName,
			flagKey:       flagKey,
			url:           sdk.url,
		}
	}
}

// TODO: rename
func sendFetchSDKInstructionsMsg2(url string) tea.Cmd {
	return func() tea.Msg {
		log.Println("sendFetchSDKInstructionsMsg2")
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

type fetchEnv struct {
	key     string
	projKey string
}

type fetchedEnv struct {
	sdkKey string
}

func sendFetchEnv(key string, projKey string) tea.Cmd {
	return func() tea.Msg {
		client := environments.NewClient("0.2.0")
		response, err := client.Get(context.Background(), "api-1fe1f428-bf2e-453e-9790-4a260fdf3391", "http://localhost:3000", key, projKey)
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

func sendFetchSDKInstructionsMsg(sdk sdkDetail, flagKey string) tea.Cmd {
	return func() tea.Msg {
		return fetchSDKInstructionsMsg{
			canonicalName: sdk.canonicalName,
			flagKey:       flagKey,
			name:          sdk.displayName,
			url:           sdk.url,
		}
	}
}

// noInstructionsMsg is sent when we can't find the SDK instructions repository for the given SDK.
type noInstructionsMsg struct{}

func sendNoInstructions() tea.Cmd {
	return func() tea.Msg {
		return noInstructionsMsg{}
	}
}
