package prompt_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synccreate "github.com/launchdarkly/ldcli/internal/sync/create"
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
)

var refundID = syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "refund"}

const refundCreated = `{"resources": [{"resourceKind": "tool", "projectKey": "production", "lookupKey": "refund",
	"action": "create_server", "status": "succeeded", "version": 1}]}`

func TestCreateMakesAToolFromFlags(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{}
	action := createAction("refund", synccreate.Input{
		JSONSchema:  pointer(`{"type":"object","properties":{"orderId":{"type":"string"}}}`),
		Description: pointer("Issue a refund"),
	})

	stdout, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeTools, Action: action, Yes: true})

	require.NoError(t, err)
	assert.JSONEq(t, refundCreated, stdout)
	assert.Equal(t, "Note: Created .launchdarkly/production/tools/refund.json.\n", notes)
	assert.Equal(t, `{
  "formatVersion": 1,
  "upsert": true,
  "key": "refund",
  "description": "Issue a refund",
  "schema": {
    "properties": {
      "orderId": {
        "type": "string"
      }
    },
    "type": "object"
  }
}
`, readLocalFile(t, root, "tools/refund.json"))
	assert.Equal(t, pointer("Issue a refund"), api.tools["refund"].Description)
	assert.Equal(t, []syncdomain.ResourceID{refundID}, lockResourceIDs(readLock(t, root)))

	stdout, err = runSyncAction(t, root, api, syncdomain.ScopeTools, syncprompt.SyncAction{DryRun: true}, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"tool refund in_sync"}, planRows(t, stdout))
}

func TestCreateMakesASkillFromFlags(t *testing.T) {
	tests := map[string]struct {
		name       *string
		wantName   string
		wantSource string
	}{
		"name from the key": {wantName: "Incident triage", wantSource: "key"},
		"name from --name":  {name: pointer("Triage"), wantName: "Triage", wantSource: "flag"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			api := &directAPI{}
			// "$(cat file)" drops the final newline of the file.
			action := createAction("incident-triage", synccreate.Input{
				Content: pointer("Sort incidents by severity."), Name: test.name, Description: pointer("Sort incidents"),
			})

			stdout, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeSkills, Action: action, Yes: true})

			require.NoError(t, err)
			assert.JSONEq(t, fmt.Sprintf(`{"resources": [{"resourceKind": "skill", "projectKey": "production",
				"lookupKey": "incident-triage", "action": "create_server", "status": "succeeded", "version": 1,
				"create": {"name": %q, "nameSource": %q}}]}`, test.wantName, test.wantSource), stdout)
			assert.Equal(t, "Note: Created .launchdarkly/production/skills/incident-triage.md.\n", notes)
			assert.Equal(t, "---\nkey: incident-triage\ndescription: Sort incidents\nname: "+test.wantName+"\nupsert: true\n---\n\n"+
				"Sort incidents by severity.\n", readLocalFile(t, root, "skills/incident-triage.md"))
			assert.Equal(t, syncdomain.Skill{
				Key: "incident-triage", Name: test.wantName, Description: "Sort incidents", Markdown: "Sort incidents by severity.\n",
			}, api.skills["incident-triage"].Skill)

			stdout, err = runSyncAction(t, root, api, syncdomain.ScopeSkills, syncprompt.SyncAction{DryRun: true}, false)
			require.NoError(t, err)
			assert.Equal(t, []string{"skill incident-triage in_sync"}, planRows(t, stdout))
		})
	}
}

// A sync that creates a skill from a hand-written file reports the name in
// the same way as add.
func TestSyncReportsTheNameOfASkillThatItCreatesFromAFile(t *testing.T) {
	tests := map[string]struct {
		frontMatter string
		want        string
	}{
		"name in the file":  {frontMatter: "name: Human handoff\n", want: `{"name": "Human handoff", "nameSource": "frontMatter"}`},
		"name from the key": {want: `{"name": "Handoff", "nameSource": "key"}`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			writeLocalFile(t, root, "skills/handoff.md",
				"---\nkey: handoff\ndescription: Hand off\n"+test.frontMatter+"upsert: true\n---\n\nAsk for a person.\n")

			stdout, err := runSyncAction(t, root, &directAPI{}, syncdomain.ScopeSkills, syncprompt.SyncAction{}, true)

			require.NoError(t, err)
			assert.JSONEq(t, `{"resources": [{"resourceKind": "skill", "projectKey": "production", "lookupKey": "handoff",
				"action": "create_server", "status": "succeeded", "version": 1, "create": `+test.want+`}]}`, stdout)
		})
	}
}

func TestCreateLeavesOtherPendingChangesAsTheyAre(t *testing.T) {
	// The workspace has a tool file with upsert that LaunchDarkly does not
	// have, and a local edit of a synced tool.
	root, api := writeStandaloneAttachmentWorkspace(t)
	writeLocalFile(t, root, "tools/search.json", `{"formatVersion": 1, "key": "search", "description": "Local", "schema": {"type": "object"}}`)
	before := lockResourceIDs(readLock(t, root))
	issueCredit := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "issue-credit"}

	_, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools, Action: createAction("issue-credit", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)}), Yes: true,
	})

	require.NoError(t, err)
	assert.Contains(t, api.tools, "issue-credit")
	assert.NotContains(t, api.tools, "refund", "the other new tool waits for a sync")
	assert.Equal(t, 1, countRequests(api.requests, "POST", "/ai-tools"))
	assert.Zero(t, countRequests(api.requests, "PATCH", "/ai-tools/search"))
	assert.ElementsMatch(t, append(before, issueCredit), lockResourceIDs(readLock(t, root)))
}

func TestCreateAdoptsATaggedToolWithTheSameSchema(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{tools: map[string]versionedTool{"refund": {Tool: syncdomain.Tool{
		Key: "refund", Description: pointer("Issue a refund"), Schema: map[string]any{"type": "object"}, Tags: []string{"billing"},
	}, Version: 4}}}
	action := createAction("refund", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)})

	stdout, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeTools, Action: action, Yes: true})

	require.NoError(t, err)
	assert.Equal(t, "Bootstrapped 1 tool file in .launchdarkly.\n", stdout)
	assert.Equal(t, "Note: tool production/refund already exists in LaunchDarkly with this content, so add started syncing it.\n", notes)
	requireOnlyReads(t, api.requests)
	file := readLocalFile(t, root, "tools/refund.json")
	assert.Contains(t, file, `"billing"`, "the file takes the fields without a flag from LaunchDarkly")
	assert.NotContains(t, file, "upsert")
	assert.Equal(t, []syncdomain.ResourceID{refundID}, lockResourceIDs(readLock(t, root)))
}

func TestCreateAdoptsASkillAndKeepsItsLaunchDarklyName(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{skills: map[string]versionedSkill{"triage": {Skill: syncdomain.Skill{
		Key: "triage", Name: "Triage", Description: "Triage a ticket", Markdown: "Pick a queue.\n",
	}, Version: 3}}}
	action := createAction("triage", synccreate.Input{
		Content: pointer("Pick a queue."), Name: pointer("Queue triage"), Description: pointer("Triage a ticket"),
	})

	_, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeSkills, Action: action, Yes: true})

	require.NoError(t, err)
	assert.Equal(t, "Note: skill production/triage already exists in LaunchDarkly with this content, so add started syncing it.\n"+
		"Note: skill production/triage keeps its LaunchDarkly name \"Triage\".\n", notes)
	requireOnlyReads(t, api.requests)
	assert.Equal(t, "---\nkey: triage\ndescription: Triage a ticket\n---\n\nPick a queue.\n", readLocalFile(t, root, "skills/triage.md"))
}

func TestCreateRefusesALaunchDarklyResourceWithOtherContent(t *testing.T) {
	refusal := "%s already exists in LaunchDarkly with different content, and add does not change it. " +
		"To sync the LaunchDarkly version, run ldcli sync %ss add --project production --key %s, then edit the file and run ldcli sync %ss or ldcli sync all."
	tests := map[string]struct {
		scope  syncdomain.Scope
		action syncprompt.AddAction
		want   string
	}{
		"tool schema": {
			scope:  syncdomain.ScopeTools,
			action: createAction("lookup", synccreate.Input{JSONSchema: pointer(`{"type": "object", "required": ["id"]}`)}),
			want:   fmt.Sprintf(refusal, "tool production/lookup", "tool", "lookup", "tool"),
		},
		"tool description": {
			scope:  syncdomain.ScopeTools,
			action: createAction("lookup", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`), Description: pointer("Look up")}),
			want:   fmt.Sprintf(refusal, "tool production/lookup", "tool", "lookup", "tool"),
		},
		"skill Markdown": {
			scope:  syncdomain.ScopeSkills,
			action: createAction("triage", synccreate.Input{Content: pointer("Pick a team.")}),
			want:   fmt.Sprintf(refusal, "skill production/triage", "skill", "triage", "skill"),
		},
		"skill description": {
			scope:  syncdomain.ScopeSkills,
			action: createAction("triage", synccreate.Input{Content: pointer("Pick a queue."), Description: pointer("")}),
			want:   fmt.Sprintf(refusal, "skill production/triage", "skill", "triage", "skill"),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			api := &directAPI{
				tools: map[string]versionedTool{
					"lookup": {Tool: syncdomain.Tool{Key: "lookup", Schema: map[string]any{"type": "object"}}, Version: 1},
				},
				skills: map[string]versionedSkill{"triage": {Skill: syncdomain.Skill{
					Key: "triage", Name: "Triage", Description: "Triage a ticket", Markdown: "Pick a queue.\n",
				}, Version: 1}},
			}

			_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: test.scope, Action: test.action, Yes: true})

			require.EqualError(t, err, test.want)
			requireOnlyReads(t, api.requests)
			assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
		})
	}
}

func TestAddWithoutAContentFlagNeverCreates(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{}
	action := syncprompt.AddAction{Filter: syncdomain.Filter{Project: "production", Keys: []string{"refund"}}}

	_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeTools, Action: action, Yes: true})

	require.EqualError(t, err, "tool production/refund does not exist in LaunchDarkly. "+
		"To create it, run ldcli sync tools add --project production --key refund with --json-schema, and optionally --description.")
	requireOnlyReads(t, api.requests)
	assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
}

func TestCreateChecksTheFlagsBeforeAnyRequest(t *testing.T) {
	tool := func(input synccreate.Input) syncprompt.AddAction { return createAction("refund", input) }
	tests := map[string]struct {
		scope  syncdomain.Scope
		action syncprompt.AddAction
		want   string
	}{
		"schema that is not an object": {
			scope: syncdomain.ScopeTools, action: tool(synccreate.Input{JSONSchema: pointer(`[]`)}),
			want: "--json-schema must be one JSON object",
		},
		"pasted tool file": {
			scope: syncdomain.ScopeTools, action: tool(synccreate.Input{JSONSchema: pointer(`{"formatVersion": 1, "schema": {}}`)}),
			want: `--json-schema is the input schema only. Pass the value of "inputSchema", and pass the description with --description.`,
		},
		"description without a schema": {
			scope: syncdomain.ScopeTools, action: tool(synccreate.Input{Description: pointer("Issue a refund")}),
			want: "--description needs --json-schema. add creates a tool only when --json-schema is set.",
		},
		"long description": {
			scope:  syncdomain.ScopeTools,
			action: tool(synccreate.Input{JSONSchema: pointer(`{}`), Description: pointer(strings.Repeat("d", 1025))}),
			want:   "tool description must be at most 1024 characters",
		},
		"two keys": {
			scope: syncdomain.ScopeTools,
			action: syncprompt.AddAction{
				Filter: syncdomain.Filter{Project: "production", Keys: []string{"refund", "credit"}},
				Create: &synccreate.Input{JSONSchema: pointer(`{}`)},
			},
			want: "add creates one tool at a time. Pass at most one --key.",
		},
		"blank Markdown": {
			scope: syncdomain.ScopeSkills, action: createAction("handoff", synccreate.Input{Content: pointer(" \n")}),
			want: "--content cannot be blank",
		},
		"skill key with uppercase letters": {
			scope: syncdomain.ScopeSkills, action: createAction("Handoff", synccreate.Input{Content: pointer("Ask for a person.")}),
			want: `skill key "Handoff" must use only lowercase letters, digits, and hyphens, and start with a letter or digit`,
		},
		"no key": {
			scope: syncdomain.ScopeTools,
			action: syncprompt.AddAction{
				Filter: syncdomain.Filter{Project: "production"}, Create: &synccreate.Input{JSONSchema: pointer(`{}`)},
			},
			want: "tool creation requires --key without interactive input",
		},
		"no project and no key": {
			scope:  syncdomain.ScopeSkills,
			action: syncprompt.AddAction{Create: &synccreate.Input{Content: pointer("Ask for a person.")}},
			want:   "skill creation requires --project and --key without interactive input",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			api := &directAPI{}

			_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: test.scope, Action: test.action, Yes: true})

			require.EqualError(t, err, test.want)
			assert.Empty(t, api.requests)
			assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
		})
	}
}

// The refusals name ldcli sync tools and ldcli sync all, and each of them
// restores a standalone file that only sync.lock has.
func TestCreateRefusesAResourceThatIsAlreadySynced(t *testing.T) {
	tool := synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)}
	for _, scope := range []syncdomain.Scope{syncdomain.ScopeTools, syncdomain.ScopeAll} {
		root, api := writeStandaloneAttachmentWorkspace(t)
		_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeAll, Action: syncprompt.SyncAction{}, Yes: true})
		require.NoError(t, err)
		api.requests = nil
		lookupFile := filepath.Join(root, syncdomain.RootDir, "production", "tools", "lookup.json")

		_, _, err = runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeTools, Action: createAction("lookup", tool), Yes: true})
		require.EqualError(t, err, "tool production/lookup is already synced. "+
			"Edit .launchdarkly/production/tools/lookup.json and run ldcli sync tools or ldcli sync all.")

		require.NoError(t, os.Remove(lookupFile))
		_, _, err = runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeTools, Action: createAction("lookup", tool), Yes: true})
		require.EqualError(t, err, "tool production/lookup has a sync.lock entry but no local file. "+
			"Run ldcli sync tools or ldcli sync all to restore it.")
		assert.Empty(t, api.requests)

		_, _, err = runScoped(t, root, api, syncprompt.Options{Scope: scope, Action: syncprompt.SyncAction{}, Yes: true})
		require.NoError(t, err)
		assert.FileExists(t, lookupFile, "ldcli sync %s restores the file", scope)
	}
}

func TestCreateDryRunPlansTheCreateAndWritesNothing(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{}
	action := createAction("refund", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)})
	action.DryRun = true

	stdout, _, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeTools, Action: action})

	require.NoError(t, err)
	assert.Equal(t, []string{"tool refund create_server"}, planRows(t, stdout))
	assert.Contains(t, stdout, `"schema"`, "the plan shows the content of the new tool")
	assert.Equal(t, []string{"GET https://example.test/api/v2/projects/production/ai-tools/refund"}, api.requests)
	assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
}

// A create asks before it writes the file. So a cancel leaves nothing, and a
// watch that runs during the question finds no file to create.
func TestCreateAsksBeforeItWritesTheFile(t *testing.T) {
	for _, answer := range []string{"n", "y"} {
		t.Run(answer, func(t *testing.T) {
			root := initRepository(t)
			api := &directAPI{}
			path := filepath.Join(root, syncdomain.RootDir, "production", "tools", "refund.json")
			input := &answerReader{answer: answer + "\n", path: path}

			_, notes, err := runWith(t, root, api, syncprompt.NewTerminalRunner(api), syncprompt.Options{
				Scope: syncdomain.ScopeTools, Action: createAction("refund", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)}),
				Input: input,
			})

			require.NoError(t, err)
			require.True(t, input.answered)
			assert.False(t, input.fileAtQuestion, "the file does not exist before the answer")
			assert.Contains(t, notes, "1 change: 1 tool create.")
			if answer == "n" {
				assert.Contains(t, notes, "Sync canceled.")
				assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
				assert.Zero(t, countRequests(api.requests, "POST", "/ai-tools"))
				return
			}
			assert.FileExists(t, path)
			assert.Equal(t, 1, countRequests(api.requests, "POST", "/ai-tools"))
		})
	}
}

func TestCreateWithoutATerminalNeedsYes(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{}

	_, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools, Action: createAction("refund", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)}),
	})

	require.EqualError(t, err, "interactive confirmation requires a terminal; rerun with --yes to apply non-interactively")
	assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
	assert.Zero(t, countRequests(api.requests, "POST", "/ai-tools"))
}

func TestCreateStopsWhenTheStateChangesAfterTheReview(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{}
	input := &answerReader{answer: "y\n", onAnswer: func() {
		// Another working copy creates the tool while the question waits.
		api.tools = map[string]versionedTool{"refund": {Tool: syncdomain.Tool{Key: "refund", Schema: map[string]any{"type": "string"}}, Version: 1}}
	}}

	_, notes, err := runWith(t, root, api, syncprompt.NewTerminalRunner(api), syncprompt.Options{
		Scope: syncdomain.ScopeTools, Action: createAction("refund", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)}),
		Input: input,
	})

	require.EqualError(t, err, "sync state changed after review; run sync again")
	assert.Contains(t, notes, "Note: Removed .launchdarkly/production/tools/refund.json.\n")
	assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
	assert.Zero(t, countRequests(api.requests, "POST", "/ai-tools"))
}

func TestCreateRemovesTheFileWhenLaunchDarklyRejectsTheCreate(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{createErr: errors.New(`{"code":"invalid_request","message":"schema is invalid","statusCode":400}`)}

	stdout, notes, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools, Action: createAction("refund", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)}), Yes: true,
	})

	require.ErrorContains(t, err, "schema is invalid")
	assert.Contains(t, stdout, `"status": "failed"`)
	assert.Equal(t, "Note: Removed .launchdarkly/production/tools/refund.json.\n", notes)
	assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
}

func TestCreateKeepsTheFileAfterAWriteWithNoResponse(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{createErr: errors.New("connection reset by peer")}
	action := createAction("refund", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)})

	_, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeTools, Action: action, Yes: true})

	require.ErrorContains(t, err, "connection reset by peer")
	assert.Empty(t, notes)
	assert.Contains(t, readLocalFile(t, root, "tools/refund.json"), `"upsert": true`)
	assert.Empty(t, lockResourceIDs(readLock(t, root)))

	// The next sync creates the tool from the file that add kept.
	api.createErr = nil
	stdout, err := runSyncAction(t, root, api, syncdomain.ScopeTools, syncprompt.SyncAction{}, true)
	require.NoError(t, err)
	assert.JSONEq(t, refundCreated, stdout)
}

func TestCreateReadsAgainAfterAConflict(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{createErr: errors.New(`{"code":"conflict","message":"tool exists","statusCode":409}`)}
	api.onCreate = func() {
		// Another working copy created the same tool first.
		api.tools = map[string]versionedTool{"refund": {Tool: syncdomain.Tool{Key: "refund", Schema: map[string]any{"type": "object"}}, Version: 1}}
	}
	action := createAction("refund", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)})

	stdout, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeTools, Action: action, Yes: true})

	require.NoError(t, err)
	assert.JSONEq(t, refundCreated, stdout)
	assert.Equal(t, "Note: Created .launchdarkly/production/tools/refund.json.\n", notes)
	assert.Equal(t, []syncdomain.ResourceID{refundID}, lockResourceIDs(readLock(t, root)))
}

// The plan saw no tool, and another working copy creates one with other
// content just before the read of the publish. Neither add nor sync changes
// that tool.
func TestCreateRowDoesNotUpdateAToolThatAppearsBeforeThePublish(t *testing.T) {
	theirs := versionedTool{Tool: syncdomain.Tool{Key: "refund", Description: pointer("Theirs"), Schema: map[string]any{"type": "object"}}, Version: 1}
	for name, test := range map[string]struct {
		// publishRead is the number of the read of the tool in which publish
		// looks before it writes.
		publishRead int
		run         func(root string, api *directAPI) (string, error)
		notes       string
	}{
		"add": {
			publishRead: 3,
			run: func(root string, api *directAPI) (string, error) {
				action := createAction("refund", synccreate.Input{JSONSchema: pointer(`{"type": "object"}`)})
				_, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeTools, Action: action, Yes: true})
				return notes, err
			},
			notes: "Note: Removed .launchdarkly/production/tools/refund.json.\n",
		},
		"sync": {
			publishRead: 3,
			run: func(root string, api *directAPI) (string, error) {
				writeLocalFile(t, root, "tools/refund.json", `{"formatVersion": 1, "upsert": true, "key": "refund", "schema": {"type": "object"}}`)
				_, notes, err := runScoped(t, root, api, syncprompt.Options{
					Scope: syncdomain.ScopeTools, Action: syncprompt.SyncAction{Filter: syncdomain.Filter{Project: "production", Keys: []string{"refund"}}}, Yes: true,
				})
				return notes, err
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			api := &directAPI{}
			reads := 0
			api.onRequest = func(method, path string) {
				if method == http.MethodGet && strings.HasSuffix(path, "/ai-tools/refund") {
					if reads++; reads == test.publishRead {
						api.tools = map[string]versionedTool{"refund": theirs}
					}
				}
			}

			notes, err := test.run(root, api)

			require.ErrorContains(t, err, `tool "refund" changed concurrently`)
			assert.Equal(t, test.publishRead, reads, "the read of the publish is the last read")
			assert.Zero(t, countRequests(api.requests, "PATCH", "/ai-tools/refund"))
			assert.Zero(t, countRequests(api.requests, "POST", "/ai-tools"))
			assert.Equal(t, theirs, api.tools["refund"])
			assert.Equal(t, test.notes, notes)
		})
	}
}

// createAction is the add that creates the tool or the skill key in the
// project "production".
func createAction(key string, input synccreate.Input) syncprompt.AddAction {
	return syncprompt.AddAction{Filter: syncdomain.Filter{Project: "production", Keys: []string{key}}, Create: &input}
}

// answerReader answers one confirmation. When the runner reads the answer, it
// records whether the file at path exists, and then runs onAnswer.
type answerReader struct {
	answer         string
	path           string
	onAnswer       func()
	answered       bool
	fileAtQuestion bool
}

func (reader *answerReader) Read(buffer []byte) (int, error) {
	if reader.answered {
		return 0, io.EOF
	}
	reader.answered = true
	if reader.path != "" {
		_, err := os.Stat(reader.path)
		reader.fileAtQuestion = err == nil
	}
	if reader.onAnswer != nil {
		reader.onAnswer()
	}
	return copy(buffer, reader.answer), nil
}

var (
	escalationID   = syncdomain.VariationID("production", "support", "escalation")
	escalationFile = "configs/support/escalation.prompt.md"
	claude         = syncapi.ModelConfig{Key: "claude", ID: "claude-sonnet", Name: "Claude", Version: 3}
)

func TestCreateMakesAnAgentVariationFromFlags(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{modelConfigs: []syncapi.ModelConfig{{Key: "gpt", ID: "gpt-5", Version: 1}, claude}}
	action := createVariationAction("escalation", synccreate.Input{
		Content: pointer("Hand the conversation to a person."), ModelConfigKey: pointer("claude"),
	})

	stdout, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopePrompts, Action: action, Yes: true})

	require.NoError(t, err)
	assert.JSONEq(t, `{"resources": [{"resourceKind": "variation", "projectKey": "production", "lookupKey": "support/escalation",
		"action": "create_server", "status": "succeeded"}]}`, stdout)
	assert.Equal(t, "Note: Created .launchdarkly/production/"+escalationFile+".\n", notes)
	assert.Equal(t, &syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "escalation", Name: "Escalation", Instructions: "Hand the conversation to a person.",
		ModelConfigKey: "claude", ModelConfigVersion: 3, Model: claude.VariationModel(),
	}, api.variation)
	file := readLocalFile(t, root, escalationFile)
	assert.Contains(t, file, "upsert: true\nmode: agent\nkey: escalation\nname: Escalation\nmodelConfigKey: claude\nmodelConfigVersion: 3\n")
	assert.NotContains(t, file, "ref:")
	assert.Equal(t, []syncdomain.ResourceID{escalationID}, lockResourceIDs(readLock(t, root)))

	stdout, err = runSyncAction(t, root, api, syncdomain.ScopePrompts, syncprompt.SyncAction{DryRun: true}, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"variation support/escalation in_sync"}, planRows(t, stdout))
}

func TestCreateMakesACompletionVariationFromFlags(t *testing.T) {
	tests := map[string]struct {
		content string
		want    []syncdomain.Message
	}{
		"role blocks": {
			content: "<system>Be brief.</system>\n<user>{{question}}</user>",
			want:    []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Be brief."}, {Role: "user", Content: "{{question}}"}},
		},
		"plain text": {
			content: "Be brief.",
			want:    []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Be brief."}},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			api := &directAPI{configMode: syncdomain.VariationModeCompletion, modelConfigs: []syncapi.ModelConfig{claude}}
			action := createVariationAction("escalation", synccreate.Input{Content: &test.content, ModelConfigKey: pointer("claude")})

			_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopePrompts, Action: action, Yes: true})

			require.NoError(t, err)
			require.NotNil(t, api.variation)
			assert.Equal(t, test.want, api.variation.Messages)
			assert.Empty(t, api.variation.Instructions)
		})
	}
}

func TestCreateVariationLeavesOtherPendingChangesAsTheyAre(t *testing.T) {
	root := initRepository(t)
	writeLocalFile(t, root, "tools/refund.json", `{"formatVersion": 1, "upsert": true, "key": "refund", "schema": {"type": "object"}}`)
	api := &directAPI{modelConfigs: []syncapi.ModelConfig{claude}}
	action := createVariationAction("escalation", synccreate.Input{Content: pointer("Escalate."), ModelConfigKey: pointer("claude")})

	_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopePrompts, Action: action, Yes: true})

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.NotContains(t, api.tools, "refund", "the new tool waits for a sync")
	assert.Equal(t, []syncdomain.ResourceID{escalationID}, lockResourceIDs(readLock(t, root)))
}

func TestCreateAdoptsAVariationWhoseModelConfigHasANewerVersion(t *testing.T) {
	root := initRepository(t)
	older := claude
	older.Version = 2
	existing := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "escalation", Name: "Escalation", Instructions: "Escalate.",
		ModelConfigKey: "claude", ModelConfigVersion: 2, Model: older.VariationModel(),
	}
	api := &directAPI{variation: &existing, variationState: "published", modelConfigs: []syncapi.ModelConfig{claude}}
	action := createVariationAction("escalation", synccreate.Input{
		Content: pointer("Escalate."), Name: pointer("Escalation"), ModelConfigKey: pointer("claude"),
	})

	stdout, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopePrompts, Action: action, Yes: true})

	require.NoError(t, err)
	assert.Equal(t, "Bootstrapped 1 variation file in .launchdarkly.\n", stdout)
	assert.Equal(t, "Note: variation production/support/escalation already exists in LaunchDarkly with this content, "+
		"so add started syncing it.\n", notes)
	requireOnlyReads(t, api.requests)
	assert.Contains(t, readLocalFile(t, root, escalationFile), "modelConfigVersion: 2\n", "the file takes the version from LaunchDarkly")
}

func TestCreateRefusesAVariationWithOtherContent(t *testing.T) {
	refusal := "variation production/support/escalation already exists in LaunchDarkly with different content, " +
		"and add does not change it. To sync the LaunchDarkly version, run " +
		"ldcli sync prompts add --project production --config-key support --key escalation, then edit the file and run ldcli sync prompts or ldcli sync all."
	tests := map[string]synccreate.Input{
		"prompt":       {Content: pointer("Escalate now."), ModelConfigKey: pointer("claude")},
		"name":         {Content: pointer("Escalate."), Name: pointer("Escalate"), ModelConfigKey: pointer("claude")},
		"model config": {Content: pointer("Escalate."), ModelConfigKey: pointer("gpt")},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			api := &directAPI{variation: &syncdomain.Variation{
				Mode: syncdomain.VariationModeAgent, Key: "escalation", Name: "Escalation", Instructions: "Escalate.",
				ModelConfigKey: "claude", ModelConfigVersion: 3, Model: claude.VariationModel(),
			}, variationState: "published", modelConfigs: []syncapi.ModelConfig{claude}}

			_, _, err := runScoped(t, root, api, syncprompt.Options{
				Scope: syncdomain.ScopePrompts, Action: createVariationAction("escalation", input), Yes: true,
			})

			require.EqualError(t, err, refusal)
			requireOnlyReads(t, api.requests)
			assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
		})
	}
}

func TestCreateRefusesTheKeyOfAnArchivedVariation(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: &syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "escalation", Name: "Escalation", Instructions: "Escalate.",
	}, variationState: "archived", modelConfigs: []syncapi.ModelConfig{claude}}
	action := createVariationAction("escalation", synccreate.Input{Content: pointer("Escalate."), ModelConfigKey: pointer("claude")})

	_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopePrompts, Action: action, Yes: true})

	require.EqualError(t, err, "variation production/support/escalation is archived in LaunchDarkly, "+
		"and a new variation cannot use its key. Pass another --key.")
	requireOnlyReads(t, api.requests)
	assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
}

func TestCreateVariationChecksTheFlagsBeforeAnyRequest(t *testing.T) {
	valid := synccreate.Input{Content: pointer("Escalate."), ModelConfigKey: pointer("claude")}
	tests := map[string]struct {
		action syncprompt.AddAction
		want   string
	}{
		"no target flags": {
			action: syncprompt.AddAction{Create: &valid},
			want:   "variation creation requires --project, --config-key, and --key without interactive input",
		},
		"no model config": {
			action: createVariationAction("escalation", synccreate.Input{Content: pointer("Escalate.")}),
			want:   "variation creation requires --model-config-key without interactive input",
		},
		"name without content": {
			action: createVariationAction("escalation", synccreate.Input{Name: pointer("Escalation")}),
			want:   "--name needs --content. add creates a variation only when --content is set.",
		},
		"blank content": {
			action: createVariationAction("escalation", synccreate.Input{Content: pointer("\t"), ModelConfigKey: pointer("claude")}),
			want:   "--content cannot be blank",
		},
		"positional selector": {
			action: syncprompt.AddAction{Resources: []syncdomain.ResourceID{escalationID}, Create: &valid},
			want: "a variation that add creates comes from --project, --config-key, and --key, " +
				"not from project-key/config-key/variation-key arguments",
		},
		"two keys": {
			action: syncprompt.AddAction{
				Filter: syncdomain.Filter{Project: "production", ConfigKey: "support", Keys: []string{"escalation", "handoff"}},
				Create: &valid,
			},
			want: "add creates one variation at a time. Pass at most one --key.",
		},
		"invalid key": {
			action: createVariationAction("escalate now", valid),
			want: `variation key "escalate now" must use only letters, digits, ".", "_", and "-", ` +
				"and start with a letter or digit",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			api := &directAPI{modelConfigs: []syncapi.ModelConfig{claude}}

			_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopePrompts, Action: test.action, Yes: true})

			require.EqualError(t, err, test.want)
			assert.Empty(t, api.requests)
		})
	}
}

func TestCreateVariationReadsTheContentBeforeTheModelConfig(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{configMode: syncdomain.VariationModeCompletion, modelConfigs: []syncapi.ModelConfig{claude}}
	action := createVariationAction("escalation", synccreate.Input{Content: pointer("<system>Be brief."), ModelConfigKey: pointer("claude")})

	_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopePrompts, Action: action, Yes: true})

	require.EqualError(t, err, "--content: unclosed <system> tag")
	assert.Equal(t, []string{"GET https://example.test/api/v2/projects/production/ai-configs/support"}, api.requests)
}

func TestCreateVariationNeedsAModelConfigThatExists(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{modelConfigs: []syncapi.ModelConfig{claude}}
	action := createVariationAction("escalation", synccreate.Input{Content: pointer("Escalate."), ModelConfigKey: pointer("llama")})

	_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopePrompts, Action: action, Yes: true})

	require.EqualError(t, err, `model config "llama" was not found`)
	requireOnlyReads(t, api.requests)
	assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
}

func TestCreateVariationRemovesTheFileWhenLaunchDarklyRejectsTheCreate(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{
		modelConfigs: []syncapi.ModelConfig{claude},
		createErr:    errors.New(`{"code":"invalid_request","message":"instructions are too long","statusCode":400}`),
	}
	action := createVariationAction("escalation", synccreate.Input{Content: pointer("Escalate."), ModelConfigKey: pointer("claude")})

	_, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopePrompts, Action: action, Yes: true})

	require.ErrorContains(t, err, "instructions are too long")
	assert.Equal(t, "Note: Removed .launchdarkly/production/"+escalationFile+".\n", notes)
	assert.NoDirExists(t, filepath.Join(root, syncdomain.RootDir))
}

func TestPromptsAddCreatesAVariationFromItsFlags(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{modelConfigs: []syncapi.ModelConfig{claude}}

	_, stderr, err := runPrompt(t, root, api, "add", "--project", "production", "--config-key", "support", "--key", "escalation",
		"--model-config-key", "claude", "--name", "Escalate to a person", "--content", "Hand the conversation to a person.",
		"--yes", "--no-input")

	require.NoError(t, err)
	assert.Contains(t, stderr, "Note: Created .launchdarkly/production/"+escalationFile+".\n")
	require.NotNil(t, api.variation)
	assert.Equal(t, "Escalate to a person", api.variation.Name)
	assert.Equal(t, "Hand the conversation to a person.", api.variation.Instructions)
}

func TestPromptsAddRefusesASelectorWithCreateFlags(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{}

	_, _, err := runPrompt(t, root, api, "add", "production/support/escalation",
		"--model-config-key", "claude", "--content", "Escalate.", "--yes", "--no-input")

	require.ErrorContains(t, err, "a variation that add creates comes from --project, --config-key, and --key, "+
		"not from project-key/config-key/variation-key arguments")
	assert.Empty(t, api.requests)
}

// createVariationAction is the add that creates the variation key in the
// config "support" of the project "production".
func createVariationAction(key string, input synccreate.Input) syncprompt.AddAction {
	return syncprompt.AddAction{
		Filter: syncdomain.Filter{Project: "production", ConfigKey: "support", Keys: []string{key}}, Create: &input,
	}
}
