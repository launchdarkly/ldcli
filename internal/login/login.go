package login

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/launchdarkly/ldcli/internal/errors"
)

const clientID = "e6506150369268abae3ed46152687201"

type DeviceAuthorization struct {
	DeviceCode      string `json:"deviceCode"`
	ExpiresIn       int    `json:"expiresIn"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
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

func GetDeviceName() string {
	deviceName, err := os.Hostname()
	if err != nil {
		deviceName = "unknown"
	}

	return deviceName
}
