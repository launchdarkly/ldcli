package awsdevopsagent

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

// stateIAM keeps managed roles by name.
type stateIAM struct {
	countingIAM
	roles map[string]bool
}

func (s stateIAM) GetRole(_ context.Context, in *iam.GetRoleInput, _ ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	name := aws.ToString(in.RoleName)
	if !s.roles[name] {
		return nil, &iamtypes.NoSuchEntityException{}
	}

	return &iam.GetRoleOutput{Role: &iamtypes.Role{Arn: aws.String("arn:aws:iam::" + testAccountID + ":role/" + name)}}, nil
}

func (s stateIAM) ListRoleTags(context.Context, *iam.ListRoleTagsInput, ...func(*iam.Options)) (*iam.ListRoleTagsOutput, error) {
	return &iam.ListRoleTagsOutput{Tags: []iamtypes.Tag{{
		Key:   aws.String(awsdevops.ManagedTagKey),
		Value: aws.String(awsdevops.ManagedTagValue),
	}}}, nil
}

func (s stateIAM) CreateRole(ctx context.Context, in *iam.CreateRoleInput, opts ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	if s.roles[aws.ToString(in.RoleName)] {
		return nil, &iamtypes.EntityAlreadyExistsException{}
	}

	return s.countingIAM.CreateRole(ctx, in, opts...)
}

// onRead runs change on the first read.
type onRead struct {
	r      io.Reader
	change func()
}

func (o *onRead) Read(p []byte) (int, error) {
	if o.change != nil {
		o.change()
		o.change = nil
	}

	return o.r.Read(p)
}

// The run uses the plan the user confirmed.
func TestSetupRunsTheConfirmedPlan(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	clients, w := countingClients()
	roles := map[string]bool{awsdevops.AgentSpaceRoleName: true, awsdevops.OperatorAppRoleName: true}
	clients.IAM = stateIAM{countingIAM: countingIAM{writes: w}, roles: roles}
	cmd.SetIn(&onRead{r: strings.NewReader("y\n"), change: func() { delete(roles, awsdevops.AgentSpaceRoleName) }})

	err := setupWith(cmd, clients, newSpaceOptions(), true, true)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, w.calls, "CreateRole")
}
