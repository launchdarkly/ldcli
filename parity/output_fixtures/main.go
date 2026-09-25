// Fixture generator for the output formatters.
//
// This is a separate program from ldcli. It renders a fixed matrix of payloads
// through the real Go formatters and writes what they produced, so the Rust
// port has an oracle to compare against without reimplementing the Go tests.
// It does not change ldcli's behavior.
//
// Run it with `make output-fixtures`.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	errs "github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/output"
)

// Case is one rendering of one payload. Stdout, stderr, and any error text are
// whatever the Go formatter produced.
type Case struct {
	Name     string   `json:"name"`
	Action   string   `json:"action"`
	Kind     string   `json:"kind"`
	Resource string   `json:"resource,omitempty"`
	Fields   []string `json:"fields,omitempty"`
	Input    string   `json:"input"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// ErrorCase is one rendering of an error rather than a payload.
type ErrorCase struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Body   string `json:"body,omitempty"`
	Output string `json:"output"`
}

// KindCase records what NewOutputKind accepts and what it says when it does not.
type KindCase struct {
	Input string `json:"input"`
	Kind  string `json:"kind"`
	Error string `json:"error,omitempty"`
}

// ValueCase records how Go writes one decoded value: back out as JSON for
// numbers.json, and through fmt.Sprint for sprint.json.
type ValueCase struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

type payload struct {
	name     string
	resource string
	input    string
}

func main() {
	outDir := "parity/fixtures/output"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fail(err)
	}

	cases := renderCases()
	errorCases := renderErrorCases()
	kinds := kindCases()
	numbers := valueCases(marshalValue, []string{
		"1", "1.0", "1.5", "-2.25", "0", "-0.0",
		"1418684722483", "9007199254740993",
		"1e21", "1e-7", "1e-9", "1.5e-7", "1e-15", "1e20", "123456789.123456789",
		`"a&b<c>d"`, `{"z":1,"a":{"c":[1,2],"b":true}}`, "[]", "{}", "null",
	})
	sprints := valueCases(sprintValue, []string{
		"0", "-0.0", "1", "1.0", "2.5", "-2.25", "0.0001", "0.00001",
		"123456", "1234567", "1418684722483", "1e21", "1e-7", "1e20",
		"9007199254740993", "123456789.123456789",
	})

	write(filepath.Join(outDir, "cases.json"), cases)
	write(filepath.Join(outDir, "errors.json"), errorCases)
	write(filepath.Join(outDir, "kinds.json"), kinds)
	write(filepath.Join(outDir, "numbers.json"), numbers)
	write(filepath.Join(outDir, "sprint.json"), sprints)
	fmt.Printf(
		"wrote %d render, %d error, %d kind, %d number, and %d sprint cases to %s\n",
		len(cases), len(errorCases), len(kinds), len(numbers), len(sprints), outDir,
	)
}

func renderCases() []Case {
	var cases []Case
	for _, p := range payloads() {
		for _, action := range []string{"list", "get", "create", "update", "delete"} {
			for _, kind := range []string{"json", "plaintext", "markdown"} {
				cases = append(cases, render(p, action, kind, nil))
			}
		}
	}
	// --fields applies to json, and prints a note on the other two.
	for _, p := range []payload{payloadByName("flags-list"), payloadByName("flags-single")} {
		for _, kind := range []string{"json", "plaintext", "markdown"} {
			cases = append(cases, named(render(p, "list", kind, []string{"key", "name"}), "fields"))
		}
	}
	// A field that no item has, and a field list that is only whitespace.
	cases = append(cases, named(
		render(payloadByName("flags-list"), "list", "json", []string{"nope"}),
		"unknown-field",
	))
	cases = append(cases, named(
		render(payloadByName("flags-list"), "list", "json", []string{"", "  "}),
		"blank-fields",
	))
	// CmdOutput itself does not validate the kind: anything that is not json
	// or markdown renders as plaintext. NewOutputKind is what rejects a bad
	// value, and kinds.json records that.
	cases = append(cases, named(render(payloadByName("flags-list"), "list", "yaml", nil), "unvalidated-kind"))
	return cases
}

func named(c Case, suffix string) Case {
	c.Name += "." + suffix
	return c
}

func render(p payload, action string, kind string, fields []string) Case {
	c := Case{
		Name:     fmt.Sprintf("%s.%s.%s", p.name, action, kind),
		Action:   action,
		Kind:     kind,
		Resource: p.resource,
		Fields:   fields,
		Input:    p.input,
	}
	stdout, stderr, err := captureStderr(func() (string, error) {
		return output.CmdOutput(action, kind, []byte(p.input), output.CmdOutputOpts{
			Fields:       fields,
			ResourceName: p.resource,
		})
	})
	c.Stdout = stdout
	c.Stderr = stderr
	if err != nil {
		c.Error = err.Error()
	}
	return c
}

func renderErrorCases() []ErrorCase {
	type source struct {
		name string
		body string
		err  error
	}
	apiError := func(body string) error {
		return errs.NewLDAPIError(errs.NewAPIError([]byte(body), errors.New("400 Bad Request"), nil))
	}
	withBody := func(name, body string) source {
		return source{name: name, body: body, err: apiError(body)}
	}

	sources := []source{
		withBody("code-message-suggestion", `{"code":"invalid_request","message":"flag key is required","suggestion":"Pass --key."}`),
		withBody("code-and-message", `{"code":"not_found","message":"unknown flag"}`),
		withBody("message-only", `{"message":"something went wrong"}`),
		withBody("empty-message", `{"code":"conflict","message":""}`),
		withBody("empty-body", `{}`),
		// The 401 has no body, so the client synthesizes one.
		{name: "unauthorized-401", err: errs.NewLDAPIError(errs.NewAPIError(nil, errors.New("401 Unauthorized"), nil))},
		{name: "plain-error", err: errors.New("dial tcp: connection refused")},
		{name: "cli-error", err: errs.NewError("output is invalid. Use 'json', 'plaintext', or 'markdown'")},
		{name: "json-unmarshal-type-error", err: &json.UnmarshalTypeError{Value: "string"}},
	}

	var cases []ErrorCase
	for _, s := range sources {
		for _, kind := range []string{"json", "plaintext", "markdown"} {
			cases = append(cases, ErrorCase{
				Name:   fmt.Sprintf("%s.%s", s.name, kind),
				Kind:   kind,
				Source: s.name,
				Body:   s.body,
				Output: output.CmdOutputError(kind, s.err),
			})
		}
	}
	return cases
}

func kindCases() []KindCase {
	var cases []KindCase
	for _, input := range []string{"json", "plaintext", "markdown", "yaml", "", "JSON", " json"} {
		kind, err := output.NewOutputKind(input)
		c := KindCase{Input: input, Kind: kind.String()}
		if err != nil {
			c.Error = err.Error()
		}
		cases = append(cases, c)
	}
	return cases
}

func marshalValue(v interface{}) string {
	out, err := json.Marshal(v)
	if err != nil {
		fail(err)
	}
	return string(out)
}

func sprintValue(v interface{}) string {
	return fmt.Sprint(v)
}

func valueCases(render func(interface{}) string, inputs []string) []ValueCase {
	var cases []ValueCase
	for _, input := range inputs {
		var v interface{}
		if err := json.Unmarshal([]byte(input), &v); err != nil {
			fail(fmt.Errorf("%s: %w", input, err))
		}
		cases = append(cases, ValueCase{Input: input, Output: render(v)})
	}
	return cases
}

// captureStderr runs fn with os.Stderr redirected, because CmdOutput writes
// its --fields note straight to the process stderr.
func captureStderr(fn func() (string, error)) (string, string, error) {
	read, write, err := os.Pipe()
	if err != nil {
		fail(err)
	}
	original := os.Stderr
	os.Stderr = write

	stdout, fnErr := fn()

	os.Stderr = original
	_ = write.Close()
	captured, _ := io.ReadAll(read)
	_ = read.Close()

	return stdout, string(captured), fnErr
}

func payloadByName(name string) payload {
	for _, p := range payloads() {
		if p.name == name {
			return p
		}
	}
	fail(fmt.Errorf("no payload named %s", name))
	return payload{}
}

func write(path string, value interface{}) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fail(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// payloads are shaped after the inputs in internal/output's own tests: one
// list and one singular response per registered resource, plus the shapes that
// have no column registry.
func payloads() []payload {
	list := []payload{
		{
			name:     "flags-list",
			resource: "flags",
			input: `{
  "_links": {
    "self": {
      "href": "/api/v2/flags/default?limit=2&offset=0",
      "type": "application/json"
    }
  },
  "items": [
    {
      "key": "alt-page",
      "name": "Alternate page",
      "kind": "boolean",
      "temporary": true,
      "tags": ["ops", "experiments", "beta", "internal"]
    },
    {
      "key": "checkout|redesign",
      "name": "Checkout redesign",
      "kind": "multivariate",
      "temporary": false,
      "tags": []
    }
  ],
  "totalCount": 7
}`,
		},
		{
			name:     "flags-single",
			resource: "flags",
			input: `{
  "key": "alt-page",
  "name": "Alternate page",
  "kind": "boolean",
  "temporary": true,
  "creationDate": 1418684722483,
  "description": "Serves the alternate product page.",
  "tags": ["ops", "experiments"],
  "_maintainer": {
    "name": "Ada Lovelace",
    "email": "ada@example.com"
  },
  "variations": [
    { "name": "on", "value": true },
    { "value": false }
  ],
  "environments": {
    "production": {
      "on": true,
      "fallthrough": { "variation": 0 },
      "rules": [{ "id": "r1" }, { "id": "r2" }]
    },
    "test": {
      "on": false,
      "fallthrough": { "variation": 1 },
      "rules": []
    }
  }
}`,
		},
		{
			name:     "projects-list",
			resource: "projects",
			input: `{
  "items": [
    { "key": "default", "name": "Default", "tags": ["a", "b"] },
    { "key": "mobile", "name": "Mobile", "tags": [] }
  ],
  "totalCount": 2
}`,
		},
		{
			name:     "projects-single",
			resource: "projects",
			input:    `{ "key": "default", "name": "Default", "tags": ["a", "b"] }`,
		},
		{
			name:     "environments-list",
			resource: "environments",
			input: `{
  "items": [
    { "key": "production", "name": "Production", "color": "417505" },
    { "key": "test", "name": "Test", "color": "F5A623" }
  ],
  "totalCount": 2
}`,
		},
		{
			name:     "members-list",
			resource: "members",
			input: `{
  "items": [
    { "_id": "1", "email": "ada@example.com", "role": "admin", "firstName": "Ada", "lastName": "Lovelace" },
    { "_id": "2", "email": "grace@example.com", "role": "writer", "firstName": "Grace", "lastName": "Hopper" }
  ],
  "totalCount": 2
}`,
		},
		{
			name:     "segments-list",
			resource: "segments",
			input: `{
  "items": [
    { "key": "beta-users", "name": "Beta users", "creationDate": 1418684722483 },
    { "key": "internal", "name": "Internal", "creationDate": 1618684722483 }
  ],
  "totalCount": 2
}`,
		},
		{
			// No column registry: falls back to the plaintext name/key form.
			name:     "unregistered-list",
			resource: "webhooks",
			input: `{
  "items": [
    { "_id": "hook-1", "name": "Deploy hook" },
    { "_id": "hook-2" }
  ],
  "totalCount": 2
}`,
		},
		{
			name:     "unregistered-single",
			resource: "webhooks",
			input:    `{ "_id": "hook-1", "name": "Deploy hook" }`,
		},
		{
			name:  "no-resource-name-list",
			input: `{ "items": [ { "key": "test-key", "name": "test-name" } ], "totalCount": 1 }`,
		},
		{
			name:  "id-only-list",
			input: `{ "items": [ { "_id": "test-id" } ] }`,
		},
		{
			name:  "email-only-list",
			input: `{ "items": [ { "email": "ada@example.com" } ] }`,
		},
		{
			name:  "unreadable-list",
			input: `{ "items": [ { "color": "blue" } ] }`,
		},
		{
			name: "scalar-list",
			input: `{
  "items": ["tag1", "tag2"],
  "_links": { "self": { "href": "/api/v2/tags", "type": "application/json" } },
  "totalCount": 2
}`,
		},
		{
			// Numbers reach the plaintext path through fmt.Sprint of a
			// float64, which is not how they are written back out as JSON.
			name:  "numeric-scalar-list",
			input: `{ "items": [1, 2.5, 1418684722483], "totalCount": 3 }`,
		},
		{
			name:  "numeric-fields-single",
			input: `{ "_id": 1418684722483, "name": 2.5 }`,
		},
		{
			name:     "empty-list",
			resource: "flags",
			input:    `{ "items": [], "totalCount": 0 }`,
		},
		{
			name: "paginated-first-page",
			input: `{
  "_links": { "self": { "href": "/my-resources?limit=5&offset=0", "type": "application/json" } },
  "items": [ { "key": "test-key", "name": "test-name" } ],
  "totalCount": 100
}`,
		},
		{
			name: "paginated-middle-page",
			input: `{
  "_links": { "self": { "href": "/my-resources?limit=5&offset=5", "type": "application/json" } },
  "items": [ { "key": "test-key", "name": "test-name" } ],
  "totalCount": 100
}`,
		},
		{
			name: "paginated-last-page",
			input: `{
  "_links": { "self": { "href": "/my-resources?limit=5&offset=95", "type": "application/json" } },
  "items": [ { "key": "test-key", "name": "test-name" } ],
  "totalCount": 100
}`,
		},
		{
			name:  "singular-no-columns",
			input: `{ "key": "test-key", "name": "test-name" }`,
		},
	}
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
	return list
}
