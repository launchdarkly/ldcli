package projects

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
	key string,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, name, key)

	return args.Get(0).([]byte), args.Error(1)
}

func (c *MockClient) List(
	ctx context.Context,
	accessToken,
	baseURI string,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI)

	return args.Get(0).([]byte), args.Error(1)
}
