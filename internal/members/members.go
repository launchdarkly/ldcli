package members

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/client"
	"ldcli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, accessToken string, baseURI string, members []ldapi.NewMemberForm) ([]byte, error)
}

type MembersClient struct {
	cliVersion string
}

func NewClient(cliVersion string) Client {
	return MembersClient{
		cliVersion: cliVersion,
	}
}

func (c MembersClient) Create(ctx context.Context, accessToken string, baseURI string, memberForms []ldapi.NewMemberForm) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)

	members, _, err := client.AccountMembersApi.PostMembers(ctx).NewMemberForm(memberForms).Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)
	}
	memberJson, err := json.Marshal(members.Items)
	if err != nil {
		return nil, err
	}

	return memberJson, nil
}
