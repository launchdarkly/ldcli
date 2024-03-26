package members

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, accessToken, baseURI, email, role string) ([]byte, error)
}

type MembersClient struct{}

func NewClient() Client {
	return MembersClient{}
}

func (c MembersClient) Create(ctx context.Context, accessToken, baseURI, email, role string) ([]byte, error) {
	client := c.client(accessToken, baseURI)
	memberForm := ldapi.NewMemberForm{Email: email, Role: &role}
	members, _, err := client.AccountMembersApi.PostMembers(ctx).NewMemberForm([]ldapi.NewMemberForm{memberForm}).Execute()
	if err != nil {
		return nil, errors.NewAPIError(err)
	}
	memberJson, err := json.Marshal(members.Items[0])
	if err != nil {
		return nil, err
	}

	return memberJson, nil
}

func (c MembersClient) client(accessToken string, baseURI string) *ldapi.APIClient {
	config := ldapi.NewConfiguration()
	config.AddDefaultHeader("Authorization", accessToken)
	config.Servers[0].URL = baseURI

	return ldapi.NewAPIClient(config)
}
