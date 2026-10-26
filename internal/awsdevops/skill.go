package awsdevops

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent/document"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
)

// ExperimentSkill is the SKILL.md of the experimentation skill. Its front
// matter gives the name and the description that setup sends as metadata.
//
//go:embed skills/experiment-orchestration/SKILL.md
var ExperimentSkill string

var (
	ExperimentSkillName        = frontMatterField(ExperimentSkill, "name")
	experimentSkillDescription = frontMatterField(ExperimentSkill, "description")
)

const (
	skillAssetType = "skill"
	skillFilePath  = "SKILL.md"
)

// frontMatterField reads one key from the front matter of a SKILL.md.
func frontMatterField(text, key string) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return ""
		}
		if value, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.Trim(strings.TrimSpace(value), `"`)
		}
	}

	return ""
}

// SkillAction is the planned skill action.
type SkillAction int

const (
	SkillCreate SkillAction = iota
	SkillKeep
	// SkillLeave means that a skill with the name has other instructions. Skills
	// have no tags, so setup cannot tell an edit by the customer from an older
	// ldcli version, and it does not replace them without --replace-skill.
	SkillLeave
	SkillReplace
)

// SkillStep is the planned experimentation skill step.
type SkillStep struct {
	Action  SkillAction
	AssetID string
}

func (s SkillStep) describe() string {
	label := fmt.Sprintf("%s (%s)", ExperimentSkillName, s.AssetID)
	switch s.Action {
	case SkillKeep:
		return "Keep skill " + label
	case SkillLeave:
		return fmt.Sprintf("Leave skill %s unchanged because its instructions differ. "+
			"Pass --replace-skill to replace them", label)
	case SkillReplace:
		return "Replace the instructions of skill " + label
	default:
		return "Create skill " + ExperimentSkillName
	}
}

func planSkill(ctx context.Context, clients Clients, opts SetupOptions, space SpaceStep) (SkillStep, error) {
	if space.Action == SpaceCreate {
		return SkillStep{Action: SkillCreate}, nil
	}
	id, err := findSkill(ctx, clients, space.ID, ExperimentSkillName)
	if err != nil || id == "" {
		return SkillStep{Action: SkillCreate}, err
	}
	same, err := skillMatches(ctx, clients, space.ID, id)
	switch {
	case err != nil:
		return SkillStep{}, err
	case same:
		return SkillStep{Action: SkillKeep, AssetID: id}, nil
	case opts.ReplaceSkill:
		return SkillStep{Action: SkillReplace, AssetID: id}, nil
	default:
		return SkillStep{Action: SkillLeave, AssetID: id}, nil
	}
}

// findSkill returns the asset ID of the skill with this name in the space, or
// "" if there is none. AWS keeps skill names unique in a space.
func findSkill(ctx context.Context, clients Clients, spaceID, name string) (string, error) {
	assets, err := listAssets(ctx, clients, spaceID, skillAssetType)
	if err != nil {
		return "", err
	}
	for _, asset := range assets {
		if assetName(asset) == name {
			return aws.ToString(asset.AssetId), nil
		}
	}

	return "", nil
}

// assetName reads the name from the metadata of an asset. It decodes the JSON
// of the document, because UnmarshalSmithyDocument fails on a document from
// NewLazyDocument.
func assetName(asset agenttypes.Asset) string {
	if asset.Metadata == nil {
		return ""
	}
	encoded, err := asset.Metadata.MarshalSmithyDocument()
	if err != nil {
		return ""
	}
	var metadata struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(encoded, &metadata); err != nil {
		return ""
	}

	return metadata.Name
}

// skillMatches reports whether the SKILL.md of the skill is ours.
func skillMatches(ctx context.Context, clients Clients, spaceID, assetID string) (bool, error) {
	file, err := clients.Agent.GetAssetFile(ctx, &devopsagent.GetAssetFileInput{
		AgentSpaceId: aws.String(spaceID),
		AssetId:      aws.String(assetID),
		Path:         aws.String(skillFilePath),
	})
	var missing *agenttypes.ResourceNotFoundException
	switch {
	case errors.As(err, &missing):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("unable to read skill %s (%s): %w", ExperimentSkillName, assetID, err)
	}
	text, ok := file.File.Content.(*agenttypes.AssetFileBodyMemberText)

	return ok && text.Value == ExperimentSkill, nil
}

// checkSkill compares the space with the skill step.
func checkSkill(ctx context.Context, clients Clients, spaceID string, step SkillStep) error {
	id, err := findSkill(ctx, clients, spaceID, ExperimentSkillName)
	switch {
	case err != nil:
		return err
	case step.Action == SkillCreate && id != "":
		return planChanged("skill %s appeared after the plan", ExperimentSkillName)
	case step.Action != SkillCreate && id != step.AssetID:
		return planChanged("skill %s disappeared after the plan", ExperimentSkillName)
	}

	return nil
}

func installSkill(
	ctx context.Context,
	clients Clients,
	step SkillStep,
	result *SetupResult,
	logf func(string, ...any),
) error {
	// The other steps can take minutes, so check the skill again.
	if err := checkSkill(ctx, clients, result.AgentSpaceID, step); err != nil {
		return err
	}
	content := &agenttypes.AssetContentMemberFile{Value: agenttypes.AssetFileContent{
		Path: aws.String(skillFilePath),
		Body: &agenttypes.AssetFileBodyMemberText{Value: ExperimentSkill},
	}}
	switch step.Action {
	case SkillKeep:
		result.SkillAssetID = step.AssetID
		logf("Kept skill %s (%s)", ExperimentSkillName, step.AssetID)
	case SkillLeave:
		result.SkillAssetID = step.AssetID
		logf("Skill %s (%s) has other instructions, so ldcli did not change it", ExperimentSkillName, step.AssetID)
		logf("Pass --replace-skill to replace them")
	case SkillReplace:
		// AWS updates only the metadata keys in the request, so the status
		// and the agent types that the customer set stay.
		if _, err := clients.Agent.UpdateAsset(ctx, &devopsagent.UpdateAssetInput{
			AgentSpaceId: aws.String(result.AgentSpaceID),
			AssetId:      aws.String(step.AssetID),
			Content:      content,
			Metadata:     document.NewLazyDocument(map[string]any{"description": experimentSkillDescription}),
		}); err != nil {
			return fmt.Errorf("unable to replace the instructions of skill %s (%s): %w", ExperimentSkillName, step.AssetID, err)
		}
		result.SkillAssetID = step.AssetID
		logf("Replaced the instructions of skill %s (%s)", ExperimentSkillName, step.AssetID)
	default:
		created, err := clients.Agent.CreateAsset(ctx, &devopsagent.CreateAssetInput{
			AgentSpaceId: aws.String(result.AgentSpaceID),
			AssetType:    aws.String(skillAssetType),
			Content:      content,
			Metadata: document.NewLazyDocument(map[string]any{
				"name":        ExperimentSkillName,
				"description": experimentSkillDescription,
				"agent_types": []string{"GENERIC"},
			}),
		})
		if err != nil {
			return fmt.Errorf("unable to create skill %s: %w", ExperimentSkillName, err)
		}
		result.SkillAssetID = aws.ToString(created.Asset.AssetId)
		logf("Created skill %s (%s)", ExperimentSkillName, result.SkillAssetID)
	}

	return nil
}
