package local

import (
	"cmp"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// The managed directory has this layout:
//
//	.launchdarkly/<project>/configs/<config>/<variation>.prompt.md
//	.launchdarkly/<project>/tools/<tool>.json
//	.launchdarkly/<project>/skills/<skill>.md
const (
	configsDir = "configs"
	toolsDir   = "tools"
	skillsDir  = "skills"

	variationFileSuffix = ".prompt.md"
	toolFileSuffix      = ".json"
	skillFileSuffix     = ".md"
)

// ParseManagedPath returns the resource that a managed file stores. The path
// is relative to the repository root and uses forward slashes. ParseManagedPath
// returns false for every other path.
func ParseManagedPath(file string) (syncdomain.ResourceID, bool) {
	parts := strings.Split(file, "/")
	if len(parts) < 4 || parts[0] != syncdomain.RootDir {
		return syncdomain.ResourceID{}, false
	}

	var id syncdomain.ResourceID
	var stem string
	switch {
	case len(parts) == 5 && parts[2] == configsDir && strings.HasSuffix(parts[4], variationFileSuffix):
		stem = strings.TrimSuffix(parts[4], variationFileSuffix)
		if syncdomain.ValidateKey(parts[3]) != nil {
			return syncdomain.ResourceID{}, false
		}
		id = syncdomain.VariationID(parts[1], parts[3], stem)
	case len(parts) == 4 && parts[2] == toolsDir && strings.HasSuffix(parts[3], toolFileSuffix):
		stem = strings.TrimSuffix(parts[3], toolFileSuffix)
		id = syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: parts[1], LookupKey: stem}
	case len(parts) == 4 && parts[2] == skillsDir && strings.HasSuffix(parts[3], skillFileSuffix):
		stem = strings.TrimSuffix(parts[3], skillFileSuffix)
		id = syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: parts[1], LookupKey: stem}
	default:
		return syncdomain.ResourceID{}, false
	}

	if syncdomain.ValidateKey(parts[1]) != nil || syncdomain.ValidateKey(stem) != nil {
		return syncdomain.ResourceID{}, false
	}
	return id, true
}

// variationPath returns the path of a variation file relative to the managed
// directory. It makes sure that each key is a safe path segment.
func variationPath(projectKey, configKey, variationKey string) (string, error) {
	if err := cmp.Or(
		validateKey("project", projectKey),
		validateKey("config", configKey),
		validateKey("variation", variationKey),
	); err != nil {
		return "", err
	}
	return path.Join(projectKey, configsDir, configKey, variationKey+variationFileSuffix), nil
}

// attachmentPath returns the path of a tool or skill file relative to the
// managed directory. It makes sure that each key is a safe path segment.
func attachmentPath(projectKey string, kind syncdomain.AttachmentKind, key string) (string, error) {
	if err := cmp.Or(validateKey("project", projectKey), validateKey(string(kind), key)); err != nil {
		return "", err
	}
	switch kind {
	case syncdomain.AttachmentTool:
		return path.Join(projectKey, toolsDir, key+toolFileSuffix), nil
	case syncdomain.AttachmentSkill:
		return path.Join(projectKey, skillsDir, key+skillFileSuffix), nil
	default:
		return "", fmt.Errorf("unsupported attachment kind %q", kind)
	}
}

// validateKey makes sure that key is a safe path segment. The name is the kind
// of key, for example "project", and appears in the error.
func validateKey(name, key string) error {
	if err := syncdomain.ValidateKey(key); err != nil {
		return fmt.Errorf("invalid %s key %q: %w", name, key, err)
	}
	return nil
}

// isWithin reports whether target is root or a path below root.
func isWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
