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
	accessToken string,
	baseURI string,
	emails []string,
	role string,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, emails, role)

	return args.Get(0).([]byte), args.Error(1)
}
