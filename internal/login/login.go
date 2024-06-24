package login

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/launchdarkly/ldcli/internal/errors"
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

type UnauthenticatedClient interface {
	MakeRequest(
		method string,
		path string,
		data []byte,
	) ([]byte, error)
}

type Client struct {
	cliVersion string
}

func NewClient(cliVersion string) Client {
	return Client{
		cliVersion: cliVersion,
	}
}

func (c Client) MakeRequest(
	method string,
	path string,
	data []byte,
) ([]byte, error) {
	client := http.Client{}

	req, _ := http.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Add("Content-type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		return body, errors.NewError(string(body))
	}

	return body, nil
}

// FetchDeviceAuthorization makes a request to create a device authorization that will later be
// used to set a local access token if the user grants access.
func FetchDeviceAuthorization(
	client UnauthenticatedClient,
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
	res, err := client.MakeRequest("POST", path, []byte(body))
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
	client UnauthenticatedClient,
	deviceCode string,
	baseURI string,
	interval time.Duration,
	maxAttempts int,
) (DeviceAuthorizationToken, error) {
	var attempts int
	for {
		if attempts > maxAttempts {
			return DeviceAuthorizationToken{}, errors.NewError("The request timed-out after too many attempts.")
		}
		deviceAuthorizationToken, err := fetchToken(
			client,
			deviceCode,
			baseURI,
		)
		if err == nil {
			return deviceAuthorizationToken, nil
		}

		var e struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		err = json.Unmarshal([]byte(err.Error()), &e)
		if err != nil {
			return DeviceAuthorizationToken{}, errors.NewErrorWrapped("error reading response", err)
		}
		switch e.Code {
		case "authorization_pending":
			attempts += 1
		case "access_denied":
			return DeviceAuthorizationToken{}, errors.NewError("Your request has been denied. Please try logging in again.")
		case "expired_token":
			return DeviceAuthorizationToken{}, errors.NewError("Your request has expired. Please try logging in again.")
		default:
			return DeviceAuthorizationToken{}, errors.NewErrorWrapped("We cannot complete your request.", err)
		}
		time.Sleep(interval)
	}
}

func fetchToken(
	client UnauthenticatedClient,
	deviceCode string,
	baseURI string,
) (DeviceAuthorizationToken, error) {
	path := fmt.Sprintf("%s/internal/device-authorization/token", baseURI)
	body, _ := json.Marshal(map[string]string{
		"deviceCode": deviceCode,
	})
	res, err := client.MakeRequest("POST", path, body)
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
