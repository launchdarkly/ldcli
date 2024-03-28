package members

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/client"
	"ldcli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, accessToken string, baseURI string, emails []string, role string) ([]byte, error)
}

type MembersClient struct{}

func NewClient() Client {
	return MembersClient{}
}

func (c MembersClient) Create(ctx context.Context, accessToken string, baseURI string, emails []string, role string) ([]byte, error) {
	client := client.New(accessToken, baseURI)
	memberForms := make([]ldapi.NewMemberForm, 0, len(emails))
	for _, e := range emails {
		memberForms = append(memberForms, ldapi.NewMemberForm{Email: e, Role: &role})
	}

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
