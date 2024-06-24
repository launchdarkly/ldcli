package login_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/login"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockClient struct {
	mock.Mock
}

var _ login.UnauthenticatedClient = &mockClient{}

func (c *mockClient) MakeRequest(
	method string,
	path string,
	data []byte,
) ([]byte, error) {
	args := c.Called(method, path, data)

	return args.Get(0).([]byte), args.Error(1)
}

func TestFetchDeviceAuthorization(t *testing.T) {
	baseURI := "http://test.com"
	mockClient := mockClient{}
	mockClient.On(
		"MakeRequest",
		"POST",
		"http://test.com/internal/device-authorization",
		[]byte(`{
			"clientId": "test-client-id",
			"deviceName": "local-device"
		}`),
	).Return([]byte(`{
		"deviceCode": "test-device-code",
		"expiresIn": 1,
		"userCode": "0001",
		"verificationUri": "/confirm-auth/test-device-code"
	}`), nil)
	expected := login.DeviceAuthorization{
		DeviceCode:      "test-device-code",
		ExpiresIn:       1,
		UserCode:        "0001",
		VerificationURI: "/confirm-auth/test-device-code",
	}

	result, err := login.FetchDeviceAuthorization(
		&mockClient,
		"test-client-id",
		"local-device",
		baseURI,
	)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestFetchToken(t *testing.T) {
	t.Run("with a token response", func(t *testing.T) {
		input, _ := json.Marshal(map[string]string{
			"deviceCode": "test-device-code",
		})
		output, _ := json.Marshal(map[string]string{
			"accessToken": "test-access-token",
		})
		mockClient := mockClient{}
		mockClient.On(
			"MakeRequest",
			"POST",
			"http://test.com/internal/device-authorization/token",
			input,
		).Return(output, nil)

		result, err := login.FetchToken(
			&mockClient,
			"test-device-code",
			"http://test.com",
			1*time.Microsecond,
			2,
		)

		require.NoError(t, err)
		assert.Equal(t, "test-access-token", result.AccessToken)
	})

	t.Run("with an authorization pending response", func(t *testing.T) {
		input, _ := json.Marshal(map[string]string{
			"deviceCode": "test-device-code",
		})
		output, _ := json.Marshal(map[string]string{
			"code":    "authorization_pending",
			"message": "error message",
		})
		responseErr := errors.NewError(string(output))
		mockClient := mockClient{}
		mockClient.On(
			"MakeRequest",
			"POST",
			"http://test.com/internal/device-authorization/token",
			input,
		).Return([]byte(""), responseErr)

		_, err := login.FetchToken(
			&mockClient,
			"test-device-code",
			"http://test.com",
			1*time.Microsecond,
			2,
		)

		assert.EqualError(t, err, "The request timed-out after too many attempts.")
	})

	t.Run("with an access denied response", func(t *testing.T) {
		input, _ := json.Marshal(map[string]string{
			"deviceCode": "test-device-code",
		})
		output, _ := json.Marshal(map[string]string{
			"code":    "access_denied",
			"message": "error message",
		})
		responseErr := errors.NewError(string(output))
		mockClient := mockClient{}
		mockClient.On(
			"MakeRequest",
			"POST",
			"http://test.com/internal/device-authorization/token",
			input,
		).Return([]byte(""), responseErr)

		_, err := login.FetchToken(
			&mockClient,
			"test-device-code",
			"http://test.com",
			1*time.Microsecond,
			2,
		)

		assert.EqualError(t, err, "Your request has been denied. Please try logging in again.")
	})

	t.Run("with an expired token response", func(t *testing.T) {
		input, _ := json.Marshal(map[string]string{
			"deviceCode": "test-device-code",
		})
		output, _ := json.Marshal(map[string]string{
			"code":    "expired_token",
			"message": "error message",
		})
		responseErr := errors.NewError(string(output))
		mockClient := mockClient{}
		mockClient.On(
			"MakeRequest",
			"POST",
			"http://test.com/internal/device-authorization/token",
			input,
		).Return([]byte(""), responseErr)

		_, err := login.FetchToken(
			&mockClient,
			"test-device-code",
			"http://test.com",
			1*time.Microsecond,
			2,
		)

		assert.EqualError(t, err, "Your request has expired. Please try logging in again.")
	})

	t.Run("with an error response", func(t *testing.T) {
		input, _ := json.Marshal(map[string]string{
			"deviceCode": "test-device-code",
		})
		output, _ := json.Marshal(map[string]string{
			"code":    "error_code",
			"message": "error message",
		})
		responseErr := errors.NewError(string(output))
		mockClient := mockClient{}
		mockClient.On(
			"MakeRequest",
			"POST",
			"http://test.com/internal/device-authorization/token",
			input,
		).Return([]byte(""), responseErr)

		_, err := login.FetchToken(
			&mockClient,
			"test-device-code",
			"http://test.com",
			1*time.Microsecond,
			2,
		)

		assert.EqualError(t, err, "We cannot complete your request.")
	})
}
