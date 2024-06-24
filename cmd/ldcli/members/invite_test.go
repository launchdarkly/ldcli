package members_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestInvite(t *testing.T) {
	mockClient := &resources.MockClient{
		Response: []byte(`{
		   "items":[
			  {
				 "_id":"000000000000000000000001",
				 "role":"writer",
				 "email":"test1@test.com"
			  },
			  {
				 "_id":"000000000000000000000002",
				 "role":"writer",
				 "email":"test2@test.com"
			  }
		   ]
		}`),
	}
	args := []string{
		"members", "invite",
		"--access-token", "abcd1234",
		"--emails", "test1@test.com,test2@test.com",
		"--role", "writer",
	}
	output, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: mockClient,
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.NoError(t, err)
	assert.Equal(t, `[{"email":"test1@test.com","role":"writer"},{"email":"test2@test.com","role":"writer"}]`, string(mockClient.Input))
	assert.Equal(t, "Successfully updated\n* test1@test.com (000000000000000000000001)\n* test2@test.com (000000000000000000000002)\n", string(output))
}
