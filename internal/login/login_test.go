package login_test

import (
	"testing"

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

	return data, args.Error(1)
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
		"expiresIn": 0000000001,
		"userCode": "0001",
		"verificationUri": "/confirm-auth/test-device-code"
	}`), nil)
	expected := login.DeviceAuthorization{}
	result, err := login.FetchDeviceAuthorization(
		&mockClient,
		"test-client-id",
		"local-device",
		baseURI,
	)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}
