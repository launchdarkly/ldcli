package members

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/client"
	"ldcli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, accessToken, baseURI, email, role string) ([]byte, error)
}

type MembersClient struct {
	cliVersion string
}

func NewClient(cliVersion string) Client {
	return MembersClient{
		cliVersion: cliVersion,
	}
}

func (c MembersClient) Create(ctx context.Context, accessToken, baseURI, email, role string) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
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
