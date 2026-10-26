package awsdevops

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

// shippedSkillHashes holds the SHA-256 of each SKILL.md that an ldcli release
// sent. A skill with one of these texts has no customer edits, so setup
// updates it without --replace-skill. When you change SKILL.md, add the hash
// of the new text. Never remove a hash.
var shippedSkillHashes = []string{
	"a5a063f2f3739ca73c10602e05f18d36f5d0e29c372be025e7e6c71d9846c4eb",
}

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

func skillHash(text string) string {
	sum := sha256.Sum256([]byte(text))

	return hex.EncodeToString(sum[:])
}

// SkillAction is the planned skill action.
type SkillAction int

const (
	SkillCreate SkillAction = iota
	SkillKeep
	// SkillUpdate replaces a SKILL.md that an older ldcli sent.
	SkillUpdate
	// SkillLeave means that someone edited the SKILL.md, so setup does not
	// replace it without --replace-skill.
	SkillLeave
	SkillReplace
	// SkillSkip means that setup could not read the skills of the space. The
	// skill is optional, so the run continues without it.
	SkillSkip
)

// SkillStep is the planned experimentation skill step.
type SkillStep struct {
	Action  SkillAction
	AssetID string
	// Version is the asset version at plan time. A write stops if it changed.
	Version int32
	// Reason says why the step is skipped.
	Reason string
}

func (s SkillStep) describe() string {
	label := fmt.Sprintf("%s (%s)", ExperimentSkillName, s.AssetID)
	switch s.Action {
	case SkillKeep:
		return "Keep skill " + label
	case SkillUpdate:
		return fmt.Sprintf("Update skill %s to the instructions of this ldcli version", label)
	case SkillLeave:
		return fmt.Sprintf("Leave skill %s unchanged because someone edited it. "+
			"Pass --replace-skill to overwrite it", label)
	case SkillReplace:
		return fmt.Sprintf("Overwrite the edited instructions of skill %s", label)
	case SkillSkip:
		return fmt.Sprintf("Skip skill %s: %s", ExperimentSkillName, s.Reason)
	default:
		return fmt.Sprintf("Create skill %s for every agent type in the space", ExperimentSkillName)
	}
}

func planSkill(ctx context.Context, clients Clients, opts SetupOptions, space SpaceStep) SkillStep {
	if space.Action == SpaceCreate {
		return SkillStep{Action: SkillCreate}
	}
	asset, err := findSkill(ctx, clients, space.ID, ExperimentSkillName)
	if err != nil {
		return SkillStep{Action: SkillSkip, Reason: errorCode(err)}
	}
	if asset == nil {
		return SkillStep{Action: SkillCreate}
	}
	step := SkillStep{AssetID: aws.ToString(asset.AssetId), Version: aws.ToInt32(asset.Version)}
	text, err := skillText(ctx, clients, space.ID, step.AssetID)
	switch {
	case err != nil:
		return SkillStep{Action: SkillSkip, AssetID: step.AssetID, Reason: errorCode(err)}
	case text == ExperimentSkill:
		step.Action = SkillKeep
	case slices.Contains(shippedSkillHashes, skillHash(text)):
		step.Action = SkillUpdate
	case opts.ReplaceSkill:
		step.Action = SkillReplace
	default:
		step.Action = SkillLeave
	}

	return step
}

// findSkill returns the skill with this name in the space, or nil if there is
// none. AWS keeps skill names unique in a space.
func findSkill(ctx context.Context, clients Clients, spaceID, name string) (*agenttypes.Asset, error) {
	assets, err := listAssets(ctx, clients, spaceID, skillAssetType)
	if err != nil {
		return nil, err
	}
	for _, asset := range assets {
		if assetName(asset) == name {
			return &asset, nil
		}
	}

	return nil, nil
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

// skillText returns the SKILL.md of the skill, or "" if it has none.
func skillText(ctx context.Context, clients Clients, spaceID, assetID string) (string, error) {
	file, err := clients.Agent.GetAssetFile(ctx, &devopsagent.GetAssetFileInput{
		AgentSpaceId: aws.String(spaceID),
		AssetId:      aws.String(assetID),
		Path:         aws.String(skillFilePath),
	})
	var missing *agenttypes.ResourceNotFoundException
	switch {
	case errors.As(err, &missing):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("unable to read skill %s (%s): %w", ExperimentSkillName, assetID, err)
	case file.File == nil:
		return "", nil
	}
	text, _ := file.File.Content.(*agenttypes.AssetFileBodyMemberText)
	if text == nil {
		return "", nil
	}

	return text.Value, nil
}

// checkSkill compares the space with a step that writes. A change to the
// version means that someone edited the skill after the plan.
func checkSkill(ctx context.Context, clients Clients, spaceID string, step SkillStep) error {
	asset, err := findSkill(ctx, clients, spaceID, ExperimentSkillName)
	switch {
	case err != nil:
		return err
	case step.Action == SkillCreate && asset != nil:
		return planChanged("skill %s appeared after the plan", ExperimentSkillName)
	case step.Action == SkillCreate:
		return nil
	case asset == nil || aws.ToString(asset.AssetId) != step.AssetID:
		return planChanged("skill %s disappeared after the plan", ExperimentSkillName)
	case aws.ToInt32(asset.Version) != step.Version:
		return planChanged("skill %s changed after the plan", ExperimentSkillName)
	}

	return nil
}

// installSkill does the skill step. The skill is optional, so a failure does
// not stop setup. It goes in result.SkillError, and the caller reports it.
func installSkill(
	ctx context.Context,
	clients Clients,
	step SkillStep,
	result *SetupResult,
	logf func(string, ...any),
) {
	result.SkillAssetID = step.AssetID
	if err := doSkillStep(ctx, clients, step, result, logf); err != nil {
		result.SkillError = err.Error()
		logf("Warning: %v", err)
	}
}

func doSkillStep(
	ctx context.Context,
	clients Clients,
	step SkillStep,
	result *SetupResult,
	logf func(string, ...any),
) error {
	switch step.Action {
	case SkillKeep:
		logf("Kept skill %s (%s)", ExperimentSkillName, step.AssetID)

		return nil
	case SkillLeave:
		logf("Skill %s (%s) has edits, so ldcli did not change it", ExperimentSkillName, step.AssetID)
		logf("Pass --replace-skill to overwrite it")

		return nil
	case SkillSkip:
		return fmt.Errorf("skipped skill %s: %s", ExperimentSkillName, step.Reason)
	}

	// The other steps can take minutes, so check the skill right before the
	// write.
	if err := checkSkill(ctx, clients, result.AgentSpaceID, step); err != nil {
		return err
	}
	content := &agenttypes.AssetContentMemberFile{Value: agenttypes.AssetFileContent{
		Path: aws.String(skillFilePath),
		Body: &agenttypes.AssetFileBodyMemberText{Value: ExperimentSkill},
	}}
	if step.Action == SkillCreate {
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

		return nil
	}

	// AWS updates only the metadata keys in the request, so the status and
	// the agent types that the customer set stay.
	if _, err := clients.Agent.UpdateAsset(ctx, &devopsagent.UpdateAssetInput{
		AgentSpaceId: aws.String(result.AgentSpaceID),
		AssetId:      aws.String(step.AssetID),
		Content:      content,
		Metadata:     document.NewLazyDocument(map[string]any{"description": experimentSkillDescription}),
	}); err != nil {
		return fmt.Errorf("unable to update skill %s (%s): %w", ExperimentSkillName, step.AssetID, err)
	}
	logf("Updated skill %s (%s)", ExperimentSkillName, step.AssetID)

	return nil
}
