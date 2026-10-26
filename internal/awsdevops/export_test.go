package awsdevops

import "context"

// Setup plans a full setup and runs it, for tests.
func Setup(ctx context.Context, clients Clients, opts SetupOptions) (SetupResult, error) {
	plan, err := PlanSetup(ctx, clients, opts, true)
	if err != nil {
		return SetupResult{}, err
	}

	return RunPlan(ctx, clients, opts, plan)
}

// ConnectExisting plans a connect run and runs it, for tests.
func ConnectExisting(ctx context.Context, clients Clients, opts SetupOptions) (SetupResult, error) {
	plan, err := PlanSetup(ctx, clients, opts, false)
	if err != nil {
		return SetupResult{}, err
	}

	return ConnectPlan(ctx, clients, opts, plan)
}
