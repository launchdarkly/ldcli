package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type toolResponse struct {
	syncdomain.Tool
	Version int `json:"version"`
}

type skillResponse struct {
	syncdomain.Skill
	Version int `json:"version"`
}

// The request bodies send every field that sync owns, so that an empty local
// value also clears the value in LaunchDarkly.
type toolMutationRequest struct {
	Key              string         `json:"key,omitempty"`
	Description      *string        `json:"description"`
	Schema           map[string]any `json:"schema"`
	CustomParameters map[string]any `json:"customParameters"`
	Tags             []string       `json:"tags"`
}

type skillMutationRequest struct {
	Description string `json:"description"`
	Markdown    string `json:"markdown"`
}

// SearchAttachments returns one page of the latest tools or skills that match query.
func (client Client) SearchAttachments(
	projectKey string,
	kind syncdomain.AttachmentKind,
	query string,
	limit, offset int,
) (Page[syncdomain.Attachment], error) {
	path, err := attachmentPath(projectKey, kind)
	if err != nil {
		return Page[syncdomain.Attachment]{}, err
	}
	values := pageQuery(limit, offset)
	if query != "" {
		values.Set("filter", "query equals "+strconv.Quote(query))
	}
	response, err := client.read(fmt.Sprintf("search %ss in project %q", kind, projectKey), projectKey, values, path...)
	if err != nil {
		return Page[syncdomain.Attachment]{}, err
	}
	page, err := decodeJSON[Page[json.RawMessage]](response, string(kind)+" search response")
	if err != nil {
		return Page[syncdomain.Attachment]{}, err
	}

	result := Page[syncdomain.Attachment]{
		Items:      make([]syncdomain.Attachment, 0, len(page.Items)),
		TotalCount: page.TotalCount,
	}
	for _, item := range page.Items {
		// A search result can omit the version, so do not require it here.
		attachment, err := parseAttachment(kind, item)
		if err != nil {
			return Page[syncdomain.Attachment]{}, err
		}
		// Tool search can return an older version that matches the query.
		// Read the key again to get the latest version.
		if kind == syncdomain.AttachmentTool {
			if attachment, err = client.ReadAttachment(projectKey, kind, attachment.Key()); err != nil {
				return Page[syncdomain.Attachment]{}, err
			}
		}
		result.Items = append(result.Items, attachment)
	}
	return result, nil
}

// ReadAttachment returns the latest version of one tool or skill.
func (client Client) ReadAttachment(projectKey string, kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error) {
	path, err := attachmentPath(projectKey, kind)
	if err != nil {
		return syncdomain.Attachment{}, err
	}
	response, err := client.read(fmt.Sprintf("get %s %q in project %q", kind, key, projectKey), projectKey, nil, append(path, key)...)
	if err != nil {
		return syncdomain.Attachment{}, err
	}
	attachment, err := decodeAttachment(kind, response)
	if err != nil {
		return syncdomain.Attachment{}, err
	}
	if attachment.Key() != key {
		return syncdomain.Attachment{}, fmt.Errorf("get %s %q: response key was %q", kind, key, attachment.Key())
	}
	return attachment, nil
}

// CreateAttachment creates the first version of a tool. Sync cannot create a skill.
func (client Client) CreateAttachment(projectKey string, attachment syncdomain.Attachment) error {
	if attachment.Kind != syncdomain.AttachmentTool {
		return fmt.Errorf("creating %ss is not supported", attachment.Kind)
	}
	return client.mutateAttachment(projectKey, attachment, true)
}

// UpdateAttachment creates a new version of a tool or skill with new content.
func (client Client) UpdateAttachment(projectKey string, attachment syncdomain.Attachment) error {
	return client.mutateAttachment(projectKey, attachment, false)
}

func (client Client) mutateAttachment(projectKey string, attachment syncdomain.Attachment, create bool) error {
	request := mutation{method: http.MethodPatch, action: "update", resource: string(attachment.Kind), key: attachment.Key()}
	if create {
		request.method, request.action = http.MethodPost, "create"
	}

	switch {
	case attachment.Kind == syncdomain.AttachmentTool && attachment.Tool != nil:
		body := toolMutationRequest{
			Description:      attachment.Tool.Description,
			Schema:           attachment.Tool.Schema,
			CustomParameters: emptyIfNil(attachment.Tool.CustomParameters),
			Tags:             attachment.Tool.Tags,
		}
		if body.Tags == nil {
			body.Tags = []string{}
		}
		if create {
			body.Key = attachment.Key()
		}
		request.body = body
	case attachment.Kind == syncdomain.AttachmentSkill && attachment.Skill != nil:
		request.body = skillMutationRequest{Description: attachment.Skill.Description, Markdown: attachment.Skill.Markdown}
	default:
		return fmt.Errorf("%s %q has no content", attachment.Kind, attachment.Key())
	}

	path, err := attachmentPath(projectKey, attachment.Kind)
	if err != nil {
		return err
	}
	if !create {
		path = append(path, attachment.Key())
	}
	_, err = client.mutate(request, path...)
	return err
}

// attachmentPath returns the path of the tool or skill collection in a project.
func attachmentPath(projectKey string, kind syncdomain.AttachmentKind) ([]string, error) {
	switch kind {
	case syncdomain.AttachmentTool:
		return projectPath(projectKey, "ai-tools"), nil
	case syncdomain.AttachmentSkill:
		return projectPath(projectKey, "ai-configs/skills"), nil
	default:
		return nil, fmt.Errorf("unsupported attachment kind %q", kind)
	}
}

// decodeAttachment decodes the response of one tool or skill read. A read
// must return the key and a positive version.
func decodeAttachment(kind syncdomain.AttachmentKind, data []byte) (syncdomain.Attachment, error) {
	attachment, err := parseAttachment(kind, data)
	if err != nil {
		return syncdomain.Attachment{}, err
	}
	switch {
	case attachment.Key() == "":
		return syncdomain.Attachment{}, fmt.Errorf("decode %s response: key is required", kind)
	case attachment.Version < 1:
		return syncdomain.Attachment{}, fmt.Errorf("decode %s response: version must be positive", kind)
	}
	return attachment, nil
}

// parseAttachment decodes one tool or skill without checking its identity.
func parseAttachment(kind syncdomain.AttachmentKind, data []byte) (syncdomain.Attachment, error) {
	switch kind {
	case syncdomain.AttachmentTool:
		response, err := decodeJSON[toolResponse](data, "tool response")
		if err != nil {
			return syncdomain.Attachment{}, err
		}
		return syncdomain.Attachment{Kind: kind, Version: response.Version, Tool: &response.Tool}, nil
	case syncdomain.AttachmentSkill:
		response, err := decodeJSON[skillResponse](data, "skill response")
		if err != nil {
			return syncdomain.Attachment{}, err
		}
		return syncdomain.Attachment{Kind: kind, Version: response.Version, Skill: &response.Skill}, nil
	default:
		return syncdomain.Attachment{}, fmt.Errorf("unsupported attachment kind %q", kind)
	}
}
