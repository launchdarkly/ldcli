package members

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/client"
	"ldcli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, accessToken string, baseURI string, memberInputs []MemberInput) ([]byte, error)
}

type MembersClient struct {
	cliVersion string
}

var _ Client = MembersClient{}

func NewClient(cliVersion string) MembersClient {
	return MembersClient{
		cliVersion: cliVersion,
	}
}

type MemberInput struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (c MembersClient) Create(ctx context.Context, accessToken string, baseURI string, memberInputs []MemberInput) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	memberForms := make([]ldapi.NewMemberForm, 0, len(memberInputs))
	for _, m := range memberInputs {
		memberForms = append(memberForms, ldapi.NewMemberForm{Email: m.Email, Role: &m.Role})
	}

	members, _, err := client.AccountMembersApi.PostMembers(ctx).NewMemberForm(memberForms).Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)
	}
	membersJson, err := json.Marshal(members)
	if err != nil {
		return nil, err
	}

	return membersJson, nil
}
