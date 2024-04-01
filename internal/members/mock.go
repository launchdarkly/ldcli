package members

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
	accessToken string,
	baseURI string,
	members []ldapi.NewMemberForm,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, members)

	return args.Get(0).([]byte), args.Error(1)
}
