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
	"57844bc3c5aca292ef88b5f949cd791206074b5cfc850ea52ebdd40020ead695",
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
	// SkillUpdate replaces a SKILL.md that an older ldcli sent, or one that
	// is empty.
	SkillUpdate
	// SkillLeave means that someone edited the SKILL.md, or that the skill has
	// none, so setup does not change it without --replace-skill.
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
	// NoFile means that the skill has no SKILL.md, for example a skill that
	// someone made from a PDF.
	NoFile bool
	// Err says why the step is skipped.
	Err error
}

func (s SkillStep) describe() string {
	label := fmt.Sprintf("%s (%s)", ExperimentSkillName, s.AssetID)
	why := "someone edited it"
	if s.NoFile {
		why = "it has no " + skillFilePath
	}
	switch s.Action {
	case SkillKeep:
		return "Keep skill " + label
	case SkillUpdate:
		return fmt.Sprintf("Update skill %s to the instructions of this ldcli version", label)
	case SkillLeave:
		return fmt.Sprintf("Leave skill %s unchanged because %s. Pass --replace-skill to overwrite it", label, why)
	case SkillReplace:
		return fmt.Sprintf("Overwrite skill %s, which is not the ldcli version because %s", label, why)
	case SkillSkip:
		return skillFailure(fmt.Errorf("Skip skill %s: %w", ExperimentSkillName, s.Err))
	default:
		return fmt.Sprintf("Create skill %s for every agent type in the space", ExperimentSkillName)
	}
}

// skillFailure is the text of a failed skill step. A missing permission is
// the most likely cause, so it names the permissions that the step needs.
func skillFailure(err error) string {
	if errorCode(err) != "AccessDeniedException" {
		return err.Error()
	}

	return fmt.Sprintf("%v. The skill step needs aidevops:ListAssets, aidevops:GetAssetFile, "+
		"aidevops:CreateAsset, and aidevops:UpdateAsset", err)
}

func planSkill(ctx context.Context, clients Clients, opts SetupOptions, space SpaceStep) SkillStep {
	if space.Action == SpaceCreate {
		return SkillStep{Action: SkillCreate}
	}
	asset, err := findSkill(ctx, clients, space.ID, ExperimentSkillName)
	if err != nil {
		return SkillStep{Action: SkillSkip, Err: err}
	}
	if asset == nil {
		return SkillStep{Action: SkillCreate}
	}
	step := SkillStep{AssetID: aws.ToString(asset.AssetId), Version: aws.ToInt32(asset.Version)}
	text, found, err := skillText(ctx, clients, space.ID, step.AssetID)
	if err == nil && !found {
		// AWS also sends ResourceNotFoundException for an asset that someone
		// deleted after the list. A deleted skill gets a new one.
		asset, err = findSkill(ctx, clients, space.ID, ExperimentSkillName)
		switch {
		case err != nil:
			return SkillStep{Action: SkillSkip, AssetID: step.AssetID, Err: err}
		case asset == nil:
			return SkillStep{Action: SkillCreate}
		case aws.ToString(asset.AssetId) != step.AssetID:
			return SkillStep{Action: SkillSkip, Err: planChanged("skill %s changed during the plan", ExperimentSkillName)}
		}
	}
	step.NoFile = !found
	switch {
	case err != nil:
		return SkillStep{Action: SkillSkip, AssetID: step.AssetID, Err: err}
	case text == ExperimentSkill:
		step.Action = SkillKeep
	case found && (text == "" || slices.Contains(shippedSkillHashes, skillHash(text))):
		// An empty file has nothing to lose.
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
	return findAsset(ctx, clients, spaceID, skillAssetType, name)
}

// findAsset returns the asset of this type and name in the space, or nil if
// there is none.
func findAsset(ctx context.Context, clients Clients, spaceID, assetType, name string) (*agenttypes.Asset, error) {
	assets, err := listAssets(ctx, clients, spaceID, assetType)
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

// skillText returns the SKILL.md of the skill. found is false if the skill
// has no SKILL.md.
func skillText(ctx context.Context, clients Clients, spaceID, assetID string) (text string, found bool, err error) {
	return assetText(ctx, clients, spaceID, assetID, skillFilePath)
}

// assetText returns the file at path in the asset. found is false if the
// asset has no such file.
func assetText(ctx context.Context, clients Clients, spaceID, assetID, path string) (text string, found bool, err error) {
	file, err := clients.Agent.GetAssetFile(ctx, &devopsagent.GetAssetFileInput{
		AgentSpaceId: aws.String(spaceID),
		AssetId:      aws.String(assetID),
		Path:         aws.String(path),
	})
	var missing *agenttypes.ResourceNotFoundException
	switch {
	case errors.As(err, &missing):
		return "", false, nil
	case err != nil:
		// The caller names the skill.
		return "", false, fmt.Errorf("unable to read %s of asset %s: %w", path, assetID, err)
	case file.File == nil:
		return "", false, nil
	}
	switch body := file.File.Content.(type) {
	case *agenttypes.AssetFileBodyMemberText:
		return body.Value, true, nil
	case *agenttypes.AssetFileBodyMemberBytes:
		return string(body.Value), true, nil
	}

	return "", false, nil
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

// The values of SetupResult.SkillAction.
const (
	SkillCreated     = "created"
	SkillKept        = "kept"
	SkillUpdated     = "updated"
	SkillLeft        = "left"
	SkillOverwritten = "overwritten"
	SkillSkipped     = "skipped"
	SkillFailed      = "failed"
)

// installSkill does the skill step. The skill is optional, so a failure does
// not stop setup. The failure goes in result.SkillError, and the caller
// reports it.
func installSkill(
	ctx context.Context,
	clients Clients,
	step SkillStep,
	result *SetupResult,
	logf func(string, ...any),
) {
	result.SkillAssetID = step.AssetID
	action, err := doSkillStep(ctx, clients, step, result, logf)
	result.SkillAction = action
	if err != nil {
		result.SkillAction = SkillFailed
		// A plan change stops the step before the write, so setup tried
		// nothing.
		if step.Action == SkillSkip || errors.Is(err, ErrPlanChanged) {
			result.SkillAction = SkillSkipped
		}
		result.SkillError = skillFailure(err)
	}
}

func doSkillStep(
	ctx context.Context,
	clients Clients,
	step SkillStep,
	result *SetupResult,
	logf func(string, ...any),
) (string, error) {
	switch step.Action {
	case SkillKeep:
		logf("Kept skill %s (%s)", ExperimentSkillName, step.AssetID)

		return SkillKept, nil
	case SkillLeave:
		logf("Left skill %s (%s) unchanged. Pass --replace-skill to overwrite it", ExperimentSkillName, step.AssetID)

		return SkillLeft, nil
	case SkillSkip:
		return "", fmt.Errorf("setup skipped skill %s: %w", ExperimentSkillName, step.Err)
	}

	// The other steps can take minutes, so check the skill right before the
	// write.
	if err := checkSkill(ctx, clients, result.AgentSpaceID, step); err != nil {
		return "", err
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
			return "", fmt.Errorf("unable to create skill %s: %w", ExperimentSkillName, err)
		}
		result.SkillAssetID = aws.ToString(created.Asset.AssetId)
		logf("Created skill %s (%s)", ExperimentSkillName, result.SkillAssetID)

		return SkillCreated, nil
	}

	// AWS updates only the metadata keys in the request, so the status and
	// the agent types that the customer set stay.
	if _, err := clients.Agent.UpdateAsset(ctx, &devopsagent.UpdateAssetInput{
		AgentSpaceId: aws.String(result.AgentSpaceID),
		AssetId:      aws.String(step.AssetID),
		Content:      content,
		Metadata:     document.NewLazyDocument(map[string]any{"description": experimentSkillDescription}),
	}); err != nil {
		return "", fmt.Errorf("unable to update skill %s (%s): %w", ExperimentSkillName, step.AssetID, err)
	}
	logf("Updated skill %s (%s)", ExperimentSkillName, step.AssetID)
	if step.Action == SkillReplace {
		return SkillOverwritten, nil
	}

	return SkillUpdated, nil
}
