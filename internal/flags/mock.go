package flags

import (
	"context"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"github.com/stretchr/testify/mock"
)

type MockClient struct {
	mock.Mock
}

var _ Client = &MockClient{}

func (c *MockClient) Create(
	ctx context.Context,
	accessToken,
	baseURI,
	name,
	key,
	projKey string,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, name, key, projKey)

	return args.Get(0).([]byte), args.Error(1)
}

func (c *MockClient) Update(
	ctx context.Context,
	accessToken,
	baseURI,
	key,
	projKey string,
	patch []ldapi.PatchOperation,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, projKey, key, patch)

	return args.Get(0).([]byte), args.Error(1)
}
