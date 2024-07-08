package mocks

import (
	"context"

	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
	"go.uber.org/mock/gomock"
)

func WithMockApiAndSdk(ctx context.Context, controller *gomock.Controller) (context.Context, *MockApi, *MockSdk) {
	api := NewMockApi(controller)
	ctx = adapters.WithApi(ctx, api)
	sdk := NewMockSdk(controller)
	ctx = adapters.WithSdk(ctx, sdk)

	return ctx, api, sdk
}
