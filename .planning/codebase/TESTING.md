# Testing Patterns

**Analysis Date:** 2026-05-11

## Go Test Framework

**Runner:**
- `go test` (standard toolchain, Go 1.23)
- No separate test runner config file — tests run with `go test ./...`

**Assertion Library:**
- `github.com/stretchr/testify v1.11.1` — `assert` and `require` packages

**Mocking:**
- Two mock strategies used (see Mocking section below):
  1. `go.uber.org/mock v0.5.2` + `mockgen` — for complex interfaces (generated)
  2. `github.com/stretchr/testify/mock` — for simpler hand-written mocks

**Run Commands:**
```bash
make test              # Run all tests (go test ./...)
go test ./path/to/pkg  # Run tests for a specific package
go test ./...          # Run all tests directly
```

No coverage target or coverage CI enforcement detected.

## Go Test File Organization

**Location:**
- Co-located with source files in the same directory (not in a separate `tests/` tree)
- Named `<source_file>_test.go` (e.g., `client.go` → `client_test.go`)

**Package Naming:**
- External test packages are the strong default: `package flags_test`, `package model_test`, `package resources_test`, `package config_test`
- White-box tests (accessing unexported symbols) use the source package: `package output` (`internal/output/plaintext_fns_internal_test.go`), `package sdk` (`internal/dev_server/sdk/http_test.go`)
- The suffix `_internal_test.go` signals white-box test intent

**Test Data:**
- Golden files: `cmd/config/testdata/help.golden` — compared with `os.ReadFile` + `assert.Equal`
- JSON fixtures: `cmd/resources/test_data/expected_template_data.json`, `cmd/resources/test_data/test-openapi.json`
- Inline JSON strings used for simple response stubs

## Go Test Structure

**Suite Organization:**
```go
func TestToggleOn(t *testing.T) {
    mockClient := &resources.MockClient{
        Response: []byte(`{ "key": "test-flag", ... }`),
    }

    t.Run("succeeds with plaintext output", func(t *testing.T) {
        args := []string{"flags", "toggle-on", "--access-token", "abcd1234", ...}
        output, err := cmd.CallCmd(t, cmd.APIClients{ResourcesClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)

        require.NoError(t, err)
        assert.Contains(t, string(output), "Successfully updated")
    })

    t.Run("returns error with missing required flags", func(t *testing.T) {
        ...
        assert.Error(t, err)
        assert.Contains(t, err.Error(), `required flag(s) "project" not set`)
    })
}
```

**Patterns:**
- Top-level `Test*` function groups related sub-tests under `t.Run(...)`
- `require.NoError(t, err)` before asserting on results (fails fast)
- `assert.*` for non-fatal checks (all run even if one fails)
- Table-driven tests use `map[string]struct{...}` with a `name` key (not slice-indexed)
- Loop variable capture: `tt := tt` present in some tests (pre-Go 1.22 pattern, still in use)
- `t.Parallel()` used selectively in `internal/dev_server/model/` tests

## Command Integration Tests (`cmd.CallCmd`)

The primary pattern for testing Cobra commands is `cmd.CallCmd` defined in `cmd/cmdtest.go`:

```go
// cmd/cmdtest.go
func CallCmd(t *testing.T, clients APIClients, trackerFn analytics.TrackerFn, args []string) ([]byte, error) {
    rootCmd, err := NewRootCommand(
        config.NewService(&resources.MockClient{}),
        trackerFn,
        clients,
        "test",
        false,
        func() bool { return true }, // always TTY → plaintext default
        nil,
    )
    cmd := rootCmd.Cmd()
    b := bytes.NewBufferString("")
    cmd.SetOut(b)
    cmd.SetArgs(args)
    err = cmd.Execute()
    ...
    return io.ReadAll(b)
}
```

- Builds the full root command tree with injected mock clients
- Sets `cmd.SetOut(b)` so output is captured in a buffer
- `isTerminal: true` so default output is plaintext (not JSON)
- Tests call CLI commands end-to-end via `args` slices
- Use `resources.MockClient` (struct with `Response []byte`, `Input []byte`, `Query url.Values` fields) for the generic API client

**Environment Variable Tests:**
```go
teardown := cmd.SetupTestEnvVars(t)
defer teardown(t)
// ... test with LD_ACCESS_TOKEN, LD_BASE_URI set
```

## Mocking

**Two Mock Strategies:**

### 1. Hand-written Testify Mocks (simple domain clients)

Used for `internal/flags`, `internal/environments`, `internal/members`, `internal/projects`, `internal/resources`, `internal/dev_server`, `internal/analytics`:

```go
// internal/flags/mock_client.go
type MockClient struct {
    mock.Mock
}

var _ Client = &MockClient{}

func (c *MockClient) Create(ctx context.Context, accessToken, baseURI, name, key, projKey string) ([]byte, error) {
    args := c.Called(accessToken, baseURI, name, key, projKey)
    return args.Get(0).([]byte), args.Error(1)
}
```

- Lives in the same package as the real implementation (not a `mocks/` subdirectory)
- Named `mock_client.go` or `mock.go`
- Uses `testify/mock.Mock` embedding
- Note: `resources.MockClient` is simpler — a struct with exported fields, no `mock.Mock` embedding

### 2. MockGen-generated Mocks (complex/dev_server interfaces)

Used for `internal/dev_server/model` interfaces (`Store`, `Observer`, `EventStore`) and `internal/dev_server/adapters` interfaces (`Sdk`, `Api`):

```go
//go:generate go run go.uber.org/mock/mockgen -destination mocks/store.go -package mocks . Store
```

Generated output in a `mocks/` subdirectory:
- `internal/dev_server/model/mocks/store.go`
- `internal/dev_server/model/mocks/observer.go`
- `internal/dev_server/model/mocks/event_store.go`
- `internal/dev_server/adapters/mocks/sdk.go`
- `internal/dev_server/adapters/mocks/api.go`
- `internal/dev_server/adapters/internal/mocks.go` (MockableTime)

**Using gomock in tests:**
```go
func TestUpsertOverride(t *testing.T) {
    t.Parallel()
    mockController := gomock.NewController(t)
    defer mockController.Finish()
    store := mocks.NewMockStore(mockController)

    store.EXPECT().GetDevProject(gomock.Any(), projKey).Return(project, nil)
    store.EXPECT().UpsertOverride(gomock.Any(), override).Return(override, nil)
    // ...
}
```

**What to Mock:**
- All `Client` interfaces when testing command behavior
- `Store`, `Observer`, `EventStore`, `Sdk` when testing `internal/dev_server/model` and `api`
- Analytics: use `analytics.NoopClientFn{}.Tracker()` for commands that don't need analytics assertions

**What NOT to Mock:**
- The SQLite store in integration-style DB tests — use a real in-memory/tmp SQLite (see `internal/dev_server/db/sqlite_test.go`)
- HTTP servers in client tests — use `net/http/httptest.NewServer` (see `internal/resources/client_test.go`)

## HTTP Server Tests

For testing actual HTTP behavior (not mocking the client):

```go
// internal/resources/client_test.go
func makeServer(t *testing.T, statusCode int, body string) *httptest.Server { ... }

server := makeServer(t, http.StatusOK, `{"message": "success"}`)
defer server.Close()
c := resources.NewClient("test-version")
response, err := c.MakeUnauthenticatedRequest("POST", server.URL, []byte(`{}`))
```

Used in `internal/resources/client_test.go` and `internal/dev_server/sdk/http_test.go` (which wires up a full gorilla/mux router).

## Frontend Tests (Vitest)

**Framework:**
- Vitest v2.1.9
- Config: `internal/dev_server/ui/vitest.config.ts`
- Environment: `jsdom`
- `globals: true` (no need to import `describe`, `it`, `expect`)

**Run Commands:**
```bash
cd internal/dev_server/ui
npm test          # vitest run (single pass)
```

**Test File Organization:**
- Separate `__tests__/` directory: `internal/dev_server/ui/src/__tests__/`
- Named `<Component>.test.tsx`
- Currently only `SubmitButton.test.tsx` exists

**Test Structure:**
```tsx
// internal/dev_server/ui/src/__tests__/SubmitButton.test.tsx
import { ProjectEditButton } from '../SubmitButton';
import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';

describe('ProjectEditButton', () => {
  it('renders "Environment" when no environment is selected', () => {
    render(<ProjectEditButton isSubmitting={false} selectedEnvironment={null} />);
    expect(screen.getByText('Environment')).toBeTruthy();
  });
});
```

**Frontend Mocking:**
- `@launchpad-ui` component library is mocked via manual mocks in `src/__mocks__/@launchpad-ui/`
  - `components.js` — renders lightweight `<button>` / `<div>` stubs using CommonJS `require('react')`
  - `core.js`, `icons.js` — similar stubs
- ESLint ignores `**/__mocks__/` via `eslint.config.js`

## Fixtures and Factories

**Go Test Data:**
- JSON response stubs: inline string literals in tests (e.g., `[]byte(\`{"key": "test-flag", ...}\`)`)
- `cmd/cmdtest.go` exports: `StubbedSuccessResponse = \`{"key": "test-key", "name": "test-name"}\`` for common stubs
- Golden file: `cmd/config/testdata/help.golden` — full expected command output

**SQLite Integration Tests:**
- Create a real `test.db` file, defer `os.Remove(dbName)` for cleanup
- No factories — test data constructed inline as `model.Project{...}` structs

## Coverage

**Requirements:** No enforced coverage target detected.

**View Coverage:**
```bash
go test ./... -cover
go test ./path/to/pkg -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Test Types

**Unit Tests:**
- Pure function tests: `internal/output/plaintext_fns_internal_test.go`, `internal/errors/errors_test.go`
- Model logic tests: `internal/dev_server/model/*_test.go` with mocked `Store`

**Integration/Command Tests:**
- End-to-end Cobra command execution via `cmd.CallCmd` helper
- Tests the full command pipeline: flag parsing → client call → output formatting
- Located in `cmd/*/` packages, use `package <name>_test`

**HTTP Tests:**
- `httptest.NewServer` for real HTTP round-trips: `internal/resources/client_test.go`
- Gorilla/mux router wired in test: `internal/dev_server/sdk/http_test.go`

**Database Tests:**
- Real SQLite operations: `internal/dev_server/db/sqlite_test.go`, `internal/dev_server/events_db/sqlite_test.go`

**E2E Tests:**
- Not used — no Playwright, no external test harness

## Pre-commit Hook

Installed via `make install-hooks` (copies from `git/hooks/` to `.git/hooks/`). Checks enforced before every commit:
- `go fmt` formatting
- `go.mod` / `go.sum` tidiness
- Dev server UI tests (`npm test`) and build (`npm run build`)

---

*Testing analysis: 2026-05-11*
