package rollouts

import (
	"context"

	"github.com/stretchr/testify/mock"
)

// MockClient is the testify-based test double for Client. Mirrors `internal/flags/mock_client.go`
// but with nil-safe pointer extraction since rollouts methods return `*RolloutList` / `*Rollout`
// (the flags analog returns []byte slices which can't be nil-cast through args.Get).
type MockClient struct {
	mock.Mock
}

// Compile-time assertion: *MockClient satisfies Client.
var _ Client = &MockClient{}

func (c *MockClient) List(
	_ context.Context,
	accessToken,
	baseURI,
	projKey,
	flagKey string,
	opts ListOpts,
) (*RolloutList, error) {
	args := c.Called(accessToken, baseURI, projKey, flagKey, opts)

	var list *RolloutList
	if v := args.Get(0); v != nil {
		list = v.(*RolloutList)
	}
	return list, args.Error(1)
}

func (c *MockClient) Get(
	_ context.Context,
	accessToken,
	baseURI,
	projKey,
	envKey,
	rolloutID string,
) (*Rollout, error) {
	args := c.Called(accessToken, baseURI, projKey, envKey, rolloutID)

	var r *Rollout
	if v := args.Get(0); v != nil {
		r = v.(*Rollout)
	}
	return r, args.Error(1)
}

func (c *MockClient) Start(
	_ context.Context,
	accessToken,
	baseURI,
	projKey,
	flagKey,
	envKey string,
	instr StartInstruction,
) (*Rollout, error) {
	args := c.Called(accessToken, baseURI, projKey, flagKey, envKey, instr)

	var r *Rollout
	if v := args.Get(0); v != nil {
		r = v.(*Rollout)
	}
	return r, args.Error(1)
}

func (c *MockClient) GetMetricResult(
	_ context.Context,
	accessToken,
	baseURI,
	projKey,
	flagKey,
	envKey,
	rolloutID,
	metricKey string,
) (*MetricResult, *float64, error) {
	args := c.Called(accessToken, baseURI, projKey, flagKey, envKey, rolloutID, metricKey)

	var mr *MetricResult
	if v := args.Get(0); v != nil {
		mr = v.(*MetricResult)
	}
	var p *float64
	if v := args.Get(1); v != nil {
		p = v.(*float64)
	}
	return mr, p, args.Error(2)
}
