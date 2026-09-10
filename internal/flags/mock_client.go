package flags

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type MockClient struct {
	mock.Mock
	// CreatedAvailability is the client-side availability the last Create call
	// asked for, or nil if it asked for none.
	CreatedAvailability *ClientSideAvailability
}

var _ Client = &MockClient{}

func (c *MockClient) Create(
	ctx context.Context,
	accessToken,
	baseURI,
	name,
	key,
	projKey string,
	opts ...CreateOption,
) ([]byte, error) {
	// Recorded rather than passed to Called so existing expectations, which set no
	// options, keep matching.
	c.CreatedAvailability = resolveCreateOptions(opts).availability
	args := c.Called(accessToken, baseURI, name, key, projKey)

	return args.Get(0).([]byte), args.Error(1)
}

func (c *MockClient) Get(
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
