package local

import (
	"fmt"
	"path/filepath"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// This file tells an authored source from a generated, vendored, or minified
// one, which sync does not edit.

// publicationProblem is the problem of a source that sync cannot write.
func publicationProblem(file, message, suggestion string, cause error) error {
	return syncdomain.WrapProblem(
		syncdomain.NewProblem(syncdomain.ErrorSourceNotWritable, message, suggestion).WithFile(file, 0, 0),
		cause,
	)
}

// generatedSource reports whether a path or its content marks a file that a
// tool produced: a vendored or minified path, or a generated-code header.
func generatedSource(file string, data []byte) bool {
	// A case-insensitive volume serves `Vendor/` and `.Min.js` too, as the
	// managed-directory check allows for.
	path := strings.ToLower(filepath.ToSlash(file))
	if strings.Contains("/"+path+"/", "/vendor/") || strings.HasSuffix(path, ".min.js") {
		return true
	}
	prefix := strings.ToLower(string(data))
	if len(prefix) > 512 {
		prefix = prefix[:512]
	}
	return strings.Contains(prefix, "code generated") && strings.Contains(prefix, "do not edit")
}

// generatedTarget reports whether a source whose resolved path is target is
// generated, vendored, or minified. A link can point into such a tree, so the
// resolved path counts as well as the name in the descriptor, on every read.
func generatedTarget(repositoryRoot, file, target string, data []byte) (bool, error) {
	// A root or a target that does not resolve hides whether the source is
	// writable, so the check fails closed.
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return false, fmt.Errorf("resolve repository root: %w", err)
	}
	resolved, err := filepath.Rel(root, target)
	if err != nil {
		return false, fmt.Errorf("resolve source file %q: %w", file, err)
	}
	return generatedSource(file, data) || generatedSource(filepath.ToSlash(resolved), data), nil
}

// rejectGeneratedTarget reports a source that sync must write but that is
// generated, vendored, or minified.
func rejectGeneratedTarget(repositoryRoot, file, target string, data []byte) error {
	generated, err := generatedTarget(repositoryRoot, file, target, data)
	if err != nil {
		return err
	}
	if generated {
		return generatedProblem(file)
	}
	return nil
}

// generatedProblem is the problem of a source that sync must write but that
// a tool produced.
func generatedProblem(file string) error {
	return publicationProblem(file, "source is generated, vendored, or minified", "Link the authored source file.", nil)
}
