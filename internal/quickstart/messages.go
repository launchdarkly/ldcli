package quickstart

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/environments"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/flags"
	"github.com/launchdarkly/ldcli/internal/sdks"
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
	time time.Time
	on   bool
}

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

		return toggledFlagMsg{
			time: time.Now(),
			on:   enabled,
		}
	}
}

type createdFlagMsg struct {
	flag         flag
	existingFlag bool
}

type confirmedFlagMsg struct {
	flag flag
}

func confirmedFlag(flag flag) tea.Cmd {
	return func() tea.Msg {
		return confirmedFlagMsg{flag}
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
	err := json.Unmarshal([]byte(errStr), &e)
	if err != nil {
		return msgRequestError{}, err
	}

	return msgRequestError{
		code:    e.Code,
		message: e.Message,
	}, nil
}

func (e msgRequestError) Error() string {
	return e.message
}

func (e msgRequestError) IsConflict() bool {
	return e.code == "conflict"
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
			msgRequestErr, err := newMsgRequestError(err.Error())
			if err != nil {
				return errMsg{err: err}
			}

			if !msgRequestErr.IsConflict() {
				return errMsg{
					err: errors.NewError(
						fmt.Sprintf(
							"Error creating flag: %s. Press \"ctrl + c\" to quit.",
							msgRequestErr.message,
						),
					),
				}
			} else {
				existingFlag = true
			}
		}

		return createdFlagMsg{flag: flag{
			key:  flagKey,
			name: flagName,
		}, existingFlag: existingFlag}
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
			sdk.url = fmt.Sprintf("https://github.com/launchdarkly/hello-%s", sdk.id)
		}

		return choseSDKMsg{
			sdk: sdk,
		}
	}
}

func readSDKInstructions(filename string) tea.Cmd {
	return func() tea.Msg {
		content, err := sdks.InstructionFiles.ReadFile(fmt.Sprintf("sdk_instructions/%s.md", filename))
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

type clientSideFlagMsg struct{} // todo: rename

func updateClientSideFlag(
	client flags.Client,
	accessToken string,
	baseUri string,
	key string,
) tea.Cmd {
	return func() tea.Msg {
		_, err := client.Update(
			context.Background(),
			accessToken,
			baseUri,
			key,
			defaultProjKey,
			[]flags.UpdateInput{{Op: "replace", Path: "/clientSideAvailability/usingEnvironmentId", Value: true}},
		)
		if err != nil {
			return errMsg{err: err}
		}

		return clientSideFlagMsg{}
	}
}

type fetchedFlagStatusMsg struct {
	enabled bool
}

func fetchFlagStatus(
	client flags.Client,
	accessToken string,
	baseUri string,
	key,
	envKey,
	projKey string,
) tea.Cmd {
	return func() tea.Msg {
		response, err := client.Get(context.Background(), accessToken, baseUri, key, projKey, envKey)
		if err != nil {
			return errMsg{err: err}
		}

		var resp struct {
			Environments map[string]interface{} `json:"environments"`
		}
		err = json.Unmarshal(response, &resp)
		if err != nil {
			return errMsg{err: err}
		}
		return fetchedFlagStatusMsg{enabled: resp.Environments[envKey].(map[string]interface{})["on"].(bool)}
	}
}

type selectedSDKMsg struct {
	index int
}

func selectedSDK(index int) tea.Cmd {
	return func() tea.Msg {
		return selectedSDKMsg{index: index}
	}
}

type eventTrackedMsg struct{}

func trackSetupStepStartedEvent(tracker analytics.Tracker, step string) tea.Cmd {
	return func() tea.Msg {
		tracker.SendSetupStepStartedEvent(step)

		return eventTrackedMsg{}
	}
}

func trackSetupSDKSelectedEvent(tracker analytics.Tracker, sdk string) tea.Cmd {
	return func() tea.Msg {
		tracker.SendSetupSDKSelectedEvent(sdk)

		return eventTrackedMsg{}
	}
}

func trackSetupFlagToggledEvent(tracker analytics.Tracker, on bool, count int, duration_ms int64) tea.Cmd {
	return func() tea.Msg {
		tracker.SendSetupFlagToggledEvent(on, count, duration_ms)

		return eventTrackedMsg{}
	}
}

func trackSendCommandCompletedEvent(tracker analytics.Tracker) tea.Cmd {
	return func() tea.Msg {
		tracker.SendCommandCompletedEvent(analytics.SUCCESS)

		return eventTrackedMsg{}
	}
}

type flagToggleThrottleMsg int

func throttleFlagToggle(count int) tea.Cmd {
	return tea.Tick(throttleDuration, func(_ time.Time) tea.Msg {
		return flagToggleThrottleMsg(count)
	})
}
