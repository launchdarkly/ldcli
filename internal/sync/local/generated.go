package local

import (
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
	path := filepath.ToSlash(file)
	if strings.Contains("/"+path+"/", "/vendor/") || strings.HasSuffix(path, ".min.js") {
		return true
	}
	prefix := strings.ToLower(string(data))
	if len(prefix) > 512 {
		prefix = prefix[:512]
	}
	return strings.Contains(prefix, "code generated") && strings.Contains(prefix, "do not edit")
}

// rejectGeneratedTarget reports a source whose resolved path is generated,
// vendored, or minified. A link can point into such a tree, so the resolved
// path counts as well as the name in the descriptor, on every read.
func rejectGeneratedTarget(repositoryRoot, file, target string, data []byte) error {
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return nil
	}
	resolved, err := filepath.Rel(root, target)
	if err != nil {
		return nil
	}
	if generatedSource(filepath.ToSlash(resolved), data) {
		return publicationProblem(file, "source is generated, vendored, or minified", "Link the authored source file.", nil)
	}
	return nil
}
