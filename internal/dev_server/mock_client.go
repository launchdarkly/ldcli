package dev_server

import "context"

type MockClient struct {
	RunServerCalled bool
	RunServerParams ServerParams
}

var _ Client = &MockClient{}

func (c *MockClient) RunServer(ctx context.Context, params ServerParams) {
	c.RunServerCalled = true
	c.RunServerParams = params
}
