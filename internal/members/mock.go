package members

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
	email,
	role string,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, email, role)

	return args.Get(0).([]byte), args.Error(1)
}
