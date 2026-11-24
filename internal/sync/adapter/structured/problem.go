package structured

import syncdomain "github.com/launchdarkly/ldcli/internal/sync"

// nameDocument puts the path of the document on a problem that has none, so
// that a caller that does not annotate the problem still reports the file.
func nameDocument(err error, path string) error {
	if problem, ok := syncdomain.ProblemFrom(err); ok && problem.File == "" {
		_ = problem.WithFile(path, 0, 0)
	}
	return err
}
