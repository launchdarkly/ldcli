package login

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/resources"
)

const (
	ClientID              = "e6506150369268abae3ed46152687201"
	MaxFetchTokenAttempts = 120 // two minutes assuming interval is one second
	TokenInterval         = 1 * time.Second
)

type DeviceAuthorization struct {
	DeviceCode      string `json:"deviceCode"`
	ExpiresIn       int    `json:"expiresIn"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
}

type DeviceAuthorizationToken struct {
	AccessToken string `json:"accessToken"`
}

// FetchDeviceAuthorization makes a request to create a device authorization that will later be
// used to set a local access token if the user grants access.
func FetchDeviceAuthorization(
	client resources.UnauthenticatedClient,
	clientID string,
	deviceName string,
	baseURI string,
) (DeviceAuthorization, error) {
	path := fmt.Sprintf("%s/internal/device-authorization", baseURI)
	body := fmt.Sprintf(
		`{
			"clientId": %q,
			"deviceName": %q
		}`,
		clientID,
		deviceName,
	)
	res, err := client.MakeUnauthenticatedRequest("POST", path, []byte(body))
	if err != nil {
		return DeviceAuthorization{}, err
	}

	var deviceAuthorization DeviceAuthorization
	err = json.Unmarshal(res, &deviceAuthorization)
	if err != nil {
		return DeviceAuthorization{}, err
	}

	return deviceAuthorization, nil
}

// FetchToken attempts to get an access token. It will continue to try while the user logs in to
// verify their request. If the user denies the request or does nothing long enough for this call
// to time out, we do not return an access token.
func FetchToken(
	client resources.UnauthenticatedClient,
	deviceCode string,
	baseURI string,
	interval time.Duration,
	maxAttempts int,
) (string, error) {
	var attempts int
	for {
		if attempts > maxAttempts {
			return "", errors.NewError("The request timed out after too many attempts.")
		}
		deviceAuthorizationToken, err := fetchToken(
			client,
			deviceCode,
			baseURI,
		)
		if err == nil {
			return deviceAuthorizationToken.AccessToken, nil
		}

		var e struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		err = json.Unmarshal([]byte(err.Error()), &e)
		if err != nil {
			return "", errors.NewErrorWrapped("error reading response", err)
		}
		switch e.Code {
		case "authorization_pending":
			attempts += 1
		case "access_denied":
			return "", errors.NewError("Your request has been denied.")
		case "expired_token":
			return "", errors.NewError("Your request has expired. Please try logging in again.")
		default:
			return "", errors.NewErrorWrapped(fmt.Sprintf("We cannot complete your request: %s", e.Message), err)
		}
		time.Sleep(interval)
	}
}

func fetchToken(
	client resources.UnauthenticatedClient,
	deviceCode string,
	baseURI string,
) (DeviceAuthorizationToken, error) {
	path := fmt.Sprintf("%s/internal/device-authorization/token", baseURI)
	body, _ := json.Marshal(map[string]string{
		"deviceCode": deviceCode,
	})
	res, err := client.MakeUnauthenticatedRequest("POST", path, body)
	if err != nil {
		return DeviceAuthorizationToken{}, err
	}

	var deviceAuthorizationToken DeviceAuthorizationToken
	err = json.Unmarshal(res, &deviceAuthorizationToken)
	if err != nil {
		return DeviceAuthorizationToken{}, err
	}

	return deviceAuthorizationToken, nil
}

func GetDeviceName() string {
	deviceName, err := os.Hostname()
	if err != nil {
		deviceName = "unknown"
	}

	return deviceName
}
