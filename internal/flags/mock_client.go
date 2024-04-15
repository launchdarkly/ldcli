package flags

import (
	"context"

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

func (c *MockClient) Read(
	ctx context.Context,
	accessToken,
	baseURI,
	key,
	projKey,
	envKey string,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, key, projKey, envKey)

	return args.Get(0).([]byte), args.Error(1)
}

func (c *MockClient) Update(
	ctx context.Context,
	accessToken,
	baseURI,
	key,
	projKey string,
	patch []UpdateInput,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, projKey, key, patch)

	return args.Get(0).([]byte), args.Error(1)
}
