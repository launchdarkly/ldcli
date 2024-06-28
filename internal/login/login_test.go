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
		minimalDuration := 1 * time.Microsecond
		minimalAttempts := 1
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
			minimalDuration,
			minimalAttempts,
		)

		require.NoError(t, err)
		assert.Equal(t, "test-access-token", result)
	})
}

func TestFetchToken_WithError(t *testing.T) {
	tests := map[string]struct {
		errCode     string
		expectedErr string
	}{
		"with an authorization pending response": {
			errCode:     "authorization_pending",
			expectedErr: "The request timed out after too many attempts.",
		},
		"with an access denied response": {
			errCode:     "access_denied",
			expectedErr: "Your request has been denied.",
		},
		"with an expired token response": {
			errCode:     "expired_token",
			expectedErr: "Your request has expired. Please try logging in again.",
		},
		"with an error response": {
			errCode:     "error_code",
			expectedErr: "We cannot complete your request: error message",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			minimalDuration := 1 * time.Microsecond
			minimalAttempts := 1
			input, _ := json.Marshal(map[string]string{
				"deviceCode": "test-device-code",
			})
			output, _ := json.Marshal(map[string]string{
				"code":    tt.errCode,
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
				minimalDuration,
				minimalAttempts,
			)

			assert.EqualError(t, err, tt.expectedErr)
		})
	}
}
