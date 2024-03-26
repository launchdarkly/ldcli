package members_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/internal/errors"
)

type MockClient struct {
	hasForbiddenErr    bool
	hasUnauthorizedErr bool
}

func (c MockClient) Create(ctx context.Context, email string, role string) ([]byte, error) {
	if c.hasForbiddenErr {
		return nil, errors.NewError("You do not have permission to make this request")
	}
	if c.hasUnauthorizedErr {
		return nil, errors.NewError("You are not authorized to make this request")
	}

	return []byte(fmt.Sprintf(`{
		   "_id":"000000000000000000000001",
		   "_lastSeen":0,
		   "_pendingInvite":true,
		   "_verified":true,
		   "creationDate":1711469912302,
		   "customRoles":[],
		   "email":%q,
		   "excludedDashboards":[],
		   "mfa":"disabled",
		   "role":%q
		}`,
		email,
		role,
	)), nil
}

func TestCreateMember(t *testing.T) {
	t.Run("return a new member", func(t *testing.T) {
		expected := `{
		   "_id":"000000000000000000000001",
		   "_lastSeen":0,
		   "_pendingInvite":true,
		   "_verified":true,
		   "creationDate":1711469912302,
		   "customRoles":[],
		   "email":"testemail@test.com",
		   "excludedDashboards":[],
		   "mfa":"disabled",
		   "role":"writer"
		}`

		mockClient := MockClient{}

		response, err := mockClient.Create(context.Background(), "testemail@test.com", "writer")

		require.NoError(t, err)
		assert.JSONEq(t, expected, string(response))
	})

	t.Run("without access is forbidden", func(t *testing.T) {
		mockClient := MockClient{
			hasForbiddenErr: true,
		}

		_, err := mockClient.Create(context.Background(), "email@test.com", "reader")

		assert.EqualError(t, err, "You do not have permission to make this request")
	})
	t.Run("with invalid accessToken is unauthorized", func(t *testing.T) {
		mockClient := MockClient{
			hasUnauthorizedErr: true,
		}

		_, err := mockClient.Create(context.Background(), "email@test.com", "reader")

		assert.EqualError(t, err, "You are not authorized to make this request")
	})
}
