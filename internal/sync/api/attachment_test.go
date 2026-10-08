package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestSearchAttachmentsUsesServerFilterAndResolvesLatestTool(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"items": [{
			"key": "search",
			"description": "Search documentation",
			"schema": {"type": "object"},
			"version": 3
		}],
		"totalCount": 1
	}`), []byte(`{
		"key": "search",
		"description": "Search documentation",
		"schema": {"type": "object"},
		"version": 4
	}`)}}
	client := NewClient(transport, "token", "https://example.com")

	page, err := client.SearchAttachments("project", syncdomain.AttachmentTool, "docs search", 25, 50)

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "search", page.Items[0].Key())
	assert.Equal(t, 4, page.Items[0].Version)
	require.Len(t, transport.Requests, 2)
	request := transport.Requests[0]
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, "https://example.com/api/v2/projects/project/ai-tools", request.Path)
	assert.Equal(t, "query equals \"docs search\"", request.Query.Get("filter"))
	assert.Equal(t, "25", request.Query.Get("limit"))
	assert.Equal(t, "50", request.Query.Get("offset"))
	assert.Equal(t, "https://example.com/api/v2/projects/project/ai-tools/search", transport.Requests[1].Path)
}

func TestSearchAttachmentsDecodesLatestSkills(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"items": [{
			"key": "support",
			"name": "Support",
			"markdown": "# Support",
			"version": 3
		}],
		"totalCount": 1
	}`)}}
	client := NewClient(transport, "token", "https://example.com")

	page, err := client.SearchAttachments("project", syncdomain.AttachmentSkill, "support", 25, 0)

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "support", page.Items[0].Key())
	assert.Equal(t, 3, page.Items[0].Version)
	assert.Equal(t, "https://example.com/api/v2/projects/project/ai-configs/skills", transport.Requests[0].Path)
}

func TestSearchAttachmentsAcceptsResultsWithoutVersion(t *testing.T) {
	toolTransport := &recordingClient{Responses: [][]byte{
		[]byte(`{"items": [{"key": "search", "schema": {"type": "object"}}], "totalCount": 1}`),
		[]byte(`{"key": "search", "schema": {"type": "object"}, "version": 4}`),
	}}
	tools, err := NewClient(toolTransport, "token", "https://example.com").
		SearchAttachments("project", syncdomain.AttachmentTool, "", 25, 0)

	require.NoError(t, err)
	require.Len(t, tools.Items, 1)
	assert.Equal(t, 4, tools.Items[0].Version)

	skillTransport := &recordingClient{Responses: [][]byte{
		[]byte(`{"items": [{"key": "support", "name": "Support", "version": 0}], "totalCount": 1}`),
	}}
	skills, err := NewClient(skillTransport, "token", "https://example.com").
		SearchAttachments("project", syncdomain.AttachmentSkill, "", 25, 0)

	require.NoError(t, err)
	require.Len(t, skills.Items, 1)
	assert.Equal(t, "support", skills.Items[0].Key())
}

func TestUpdateSkillSendsEditableFields(t *testing.T) {
	description := "Customer support guidance"
	markdown := "Help the customer.\n"
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"key": "support",
		"name": "support",
		"description": "Customer support guidance",
		"markdown": "Help the customer.\n",
		"version": 3
	}`)}}
	client := NewClient(transport, "token", "https://example.com")
	skill := syncdomain.Skill{Key: "support", Name: "support", Description: description, Markdown: markdown}

	err := client.UpdateAttachment("project", syncdomain.Attachment{
		Kind: syncdomain.AttachmentSkill, Skill: &skill,
	})

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	assert.Equal(t, http.MethodPatch, transport.Requests[0].Method)

	var body map[string]any
	require.NoError(t, json.Unmarshal(transport.Requests[0].Body, &body))
	assert.Equal(t, map[string]any{"description": description, "markdown": markdown}, body)
	assert.NotContains(t, body, "name")
}

func TestUpdateSkillSendsEmptyDescriptionToClearRemoteValue(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"key": "support",
		"markdown": "Help the customer.\n",
		"version": 3
	}`)}}
	client := NewClient(transport, "token", "https://example.com")
	skill := syncdomain.Skill{Key: "support", Markdown: "Help the customer.\n"}

	err := client.UpdateAttachment("project", syncdomain.Attachment{
		Kind: syncdomain.AttachmentSkill, Skill: &skill,
	})

	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(transport.Requests[0].Body, &body))
	assert.Equal(t, "", body["description"])
}

func TestUpdateToolSendsEmptyCollectionsToClearRemoteValues(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"key": "search",
		"schema": {"type": "object"},
		"customParameters": {},
		"tags": [],
		"version": 3
	}`)}}
	client := NewClient(transport, "token", "https://example.com")

	err := client.UpdateAttachment("project", syncdomain.Attachment{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
	})

	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(transport.Requests[0].Body, &body))
	assert.Contains(t, body, "description")
	assert.Nil(t, body["description"])
	assert.Equal(t, map[string]any{}, body["customParameters"])
	assert.Equal(t, []any{}, body["tags"])
}

func TestCreateToolIncludesItsKey(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"key": "search",
		"schema": {"type": "object"},
		"customParameters": {},
		"tags": [],
		"version": 1
	}`)}}
	client := NewClient(transport, "token", "https://example.com")

	err := client.CreateAttachment("project", syncdomain.Attachment{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
	})

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	assert.Equal(t, http.MethodPost, transport.Requests[0].Method)
	assert.Equal(t, "https://example.com/api/v2/projects/project/ai-tools", transport.Requests[0].Path)
	var body map[string]any
	require.NoError(t, json.Unmarshal(transport.Requests[0].Body, &body))
	assert.Equal(t, "search", body["key"])
}

func TestReadAttachmentRequiresMatchingVersionedIdentity(t *testing.T) {
	for name, response := range map[string]string{
		"key":     `{"key":"other","schema":{},"version":2}`,
		"version": `{"key":"search","schema":{},"version":0}`,
	} {
		t.Run(name, func(t *testing.T) {
			client := NewClient(&recordingClient{Responses: [][]byte{[]byte(response)}}, "token", "https://example.com")

			_, err := client.ReadAttachment("project", syncdomain.AttachmentTool, "search")

			require.Error(t, err)
		})
	}
}

func TestIsNotFoundRecognizesAPIStatus(t *testing.T) {
	assert.True(t, IsNotFound(errors.New(`{"code":"not_found","statusCode":404}`)))
	assert.False(t, IsNotFound(errors.New(`{"code":"invalid_request","statusCode":400}`)))
}
