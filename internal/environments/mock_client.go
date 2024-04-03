package environments

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type MockClient struct {
	mock.Mock
}

var _ Client = &MockClient{}

func (c *MockClient) Get(
	ctx context.Context,
	accessToken,
	baseURI,
	key,
	projKey string,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, key, projKey)

	return args.Get(0).([]byte), args.Error(1)
}
