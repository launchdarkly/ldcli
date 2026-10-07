package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

// SearchAttachments returns one API-filtered page of latest attachments.
func (client Client) SearchAttachments(
	projectKey string,
	kind syncdomain.AttachmentKind,
	query string,
	limit, offset int,
) (Page[syncdomain.Attachment], error) {
	endpoint, err := client.attachmentEndpoint(projectKey, kind, "")
	if err != nil {
		return Page[syncdomain.Attachment]{}, err
	}

	values := url.Values{
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa(offset)},
	}
	if query != "" {
		values.Set("filter", "query equals "+strconv.Quote(query))
	}

	response, err := client.transport.MakeRequest(client.accessToken, http.MethodGet, endpoint, "", values, nil, false)
	if err != nil {
		return Page[syncdomain.Attachment]{}, contextualAPIError(
			err,
			fmt.Sprintf("search %ss in project %q", kind, projectKey),
			projectKey,
		)
	}

	switch kind {
	case syncdomain.AttachmentTool:
		var page Page[toolResponse]
		if err := json.Unmarshal(response, &page); err != nil {
			return Page[syncdomain.Attachment]{}, fmt.Errorf("decode tool search response: %w", err)
		}
		items := make([]syncdomain.Attachment, 0, len(page.Items))
		for index := range page.Items {
			item := page.Items[index]
			// Tool search can return an older matching version, so resolve each
			// key before presenting the result as the latest version.
			attachment, err := client.ReadAttachment(projectKey, kind, item.Key)
			if err != nil {
				return Page[syncdomain.Attachment]{}, err
			}
			items = append(items, attachment)
		}
		return Page[syncdomain.Attachment]{Items: items, TotalCount: page.TotalCount}, nil

	case syncdomain.AttachmentSkill:
		var page Page[skillResponse]
		if err := json.Unmarshal(response, &page); err != nil {
			return Page[syncdomain.Attachment]{}, fmt.Errorf("decode skill search response: %w", err)
		}
		items := make([]syncdomain.Attachment, 0, len(page.Items))
		for index := range page.Items {
			item := page.Items[index]
			items = append(items, syncdomain.Attachment{Kind: kind, Version: item.Version, Skill: &item.Skill})
		}
		return Page[syncdomain.Attachment]{Items: items, TotalCount: page.TotalCount}, nil

	default:
		return Page[syncdomain.Attachment]{}, fmt.Errorf("unsupported attachment kind %q", kind)
	}
}

// ReadAttachment returns the latest version of one project-scoped attachment.
func (client Client) ReadAttachment(projectKey string, kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error) {
	endpoint, err := client.attachmentEndpoint(projectKey, kind, key)
	if err != nil {
		return syncdomain.Attachment{}, err
	}

	response, err := client.transport.MakeRequest(client.accessToken, http.MethodGet, endpoint, "", nil, nil, false)
	if err != nil {
		return syncdomain.Attachment{}, contextualAPIError(
			err,
			fmt.Sprintf("get %s %q in project %q", kind, key, projectKey),
			projectKey,
		)
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

// UpdateAttachment creates a new version when canonical attachment content changed.
func (client Client) UpdateAttachment(projectKey string, attachment syncdomain.Attachment) error {
	key := attachment.Key()
	endpoint, err := client.attachmentEndpoint(projectKey, attachment.Kind, key)
	if err != nil {
		return err
	}

	request, err := attachmentMutationRequest(attachment, false)
	if err != nil {
		return err
	}
	return client.mutateAttachment(http.MethodPatch, endpoint, "update", attachment, request)
}

// CreateAttachment creates the first version of a locally defined tool.
func (client Client) CreateAttachment(projectKey string, attachment syncdomain.Attachment) error {
	if attachment.Kind != syncdomain.AttachmentTool {
		return fmt.Errorf("creating %ss is not supported", attachment.Kind)
	}
	endpoint, err := client.attachmentEndpoint(projectKey, attachment.Kind, "")
	if err != nil {
		return err
	}
	request, err := attachmentMutationRequest(attachment, true)
	if err != nil {
		return err
	}
	return client.mutateAttachment(http.MethodPost, endpoint, "create", attachment, request)
}

func attachmentMutationRequest(attachment syncdomain.Attachment, includeKey bool) (any, error) {
	switch attachment.Kind {
	case syncdomain.AttachmentTool:
		if attachment.Tool == nil {
			return nil, fmt.Errorf("tool content is required")
		}
		customParameters := attachment.Tool.CustomParameters
		if customParameters == nil {
			customParameters = map[string]any{}
		}
		tags := attachment.Tool.Tags
		if tags == nil {
			tags = []string{}
		}
		request := toolMutationRequest{
			Description:      attachment.Tool.Description,
			Schema:           attachment.Tool.Schema,
			CustomParameters: customParameters,
			Tags:             tags,
		}
		if includeKey {
			request.Key = attachment.Key()
		}
		return request, nil
	case syncdomain.AttachmentSkill:
		if attachment.Skill == nil {
			return nil, fmt.Errorf("skill content is required")
		}
		return skillMutationRequest{Description: attachment.Skill.Description, Markdown: attachment.Skill.Markdown}, nil
	default:
		return nil, fmt.Errorf("unsupported attachment kind %q", attachment.Kind)
	}
}

func (client Client) mutateAttachment(method, endpoint, action string, attachment syncdomain.Attachment, request any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode %s %q: %w", attachment.Kind, attachment.Key(), err)
	}
	_, err = client.transport.MakeRequest(client.accessToken, method, endpoint, "application/json", nil, body, false)
	if err != nil {
		return newResourceMutationError(action, string(attachment.Kind), attachment.Key(), err)
	}
	return nil
}

func (client Client) attachmentEndpoint(projectKey string, kind syncdomain.AttachmentKind, key string) (string, error) {
	var parts []string
	switch kind {
	case syncdomain.AttachmentTool:
		parts = []string{"api/v2/projects", projectKey, "ai-tools"}
	case syncdomain.AttachmentSkill:
		parts = []string{"api/v2/projects", projectKey, "ai-configs/skills"}
	default:
		return "", fmt.Errorf("unsupported attachment kind %q", kind)
	}
	if key != "" {
		parts = append(parts, key)
	}
	endpoint, err := url.JoinPath(client.baseURI, parts...)
	if err != nil {
		return "", fmt.Errorf("build %s endpoint: %w", kind, err)
	}
	return endpoint, nil
}

func decodeAttachment(kind syncdomain.AttachmentKind, data []byte) (syncdomain.Attachment, error) {
	var attachment syncdomain.Attachment
	switch kind {
	case syncdomain.AttachmentTool:
		var response toolResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return syncdomain.Attachment{}, fmt.Errorf("decode tool response: %w", err)
		}
		attachment = syncdomain.Attachment{Kind: kind, Version: response.Version, Tool: &response.Tool}
	case syncdomain.AttachmentSkill:
		var response skillResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return syncdomain.Attachment{}, fmt.Errorf("decode skill response: %w", err)
		}
		attachment = syncdomain.Attachment{Kind: kind, Version: response.Version, Skill: &response.Skill}
	default:
		return syncdomain.Attachment{}, fmt.Errorf("unsupported attachment kind %q", kind)
	}
	if attachment.Key() == "" {
		return syncdomain.Attachment{}, fmt.Errorf("decode %s response: key is required", kind)
	}
	if attachment.Version < 1 {
		return syncdomain.Attachment{}, fmt.Errorf("decode %s response: version must be positive", kind)
	}
	return attachment, nil
}

// IsNotFound reports whether a wrapped LaunchDarkly API error has a 404 status.
func IsNotFound(err error) bool {
	status, ok := responseStatusCode(err)
	return ok && status == http.StatusNotFound
}
