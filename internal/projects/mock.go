package projects

import (
	"context"

	"github.com/stretchr/testify/mock"

	"ld-cli/internal/errors"
)

type MockClient struct {
	mock.Mock

	HasForbiddenErr    bool
	HasUnauthorizedErr bool

	AccessToken string
	BaseURI     string
}

var _ Client2 = &MockClient{}

func (c *MockClient) Create2(
	ctx context.Context,
	accessToken,
	baseURI,
	name,
	key string,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, name, key)

	return args.Get(0).([]byte), args.Error(1)
}

func (c *MockClient) List2(
	ctx context.Context,
	accessToken,
	baseURI string,
) ([]byte, error) {
	if c.HasForbiddenErr {
		return nil, errors.ErrForbidden
	}
	if c.HasUnauthorizedErr {
		return nil, errors.ErrUnauthorized
	}

	args := c.Called(accessToken, baseURI)

	return args.Get(0).([]byte), args.Error(1)
}
