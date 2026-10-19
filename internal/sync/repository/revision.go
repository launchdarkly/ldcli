package repository

import (
	"os"
	"regexp"
	"strconv"
)

// Revision is the commit that sync runs at, and the pull request of that
// commit. An empty Commit or a PullRequest of 0 means that sync does not know
// the value.
type Revision struct {
	// Commit is the full SHA of HEAD.
	Commit      string
	PullRequest int
}

// RevisionReader finds the revision of a repository. Tests replace it, so that
// they do not run Git.
type RevisionReader interface {
	Revision(root string) Revision
}

// NewRevisionReader returns a RevisionReader that runs Git and reads the
// environment of the process.
func NewRevisionReader() RevisionReader {
	return gitRevisionReader{git: execGit{}, getenv: os.Getenv}
}

type gitRevisionReader struct {
	git    gitRunner
	getenv func(string) string
}

var (
	// pullRequestRef is the ref that GitHub Actions checks out for a pull
	// request event.
	pullRequestRef = regexp.MustCompile(`^refs/pull/([0-9]+)/merge$`)
	// squashSubject is the subject that GitHub gives a squash merge.
	squashSubject = regexp.MustCompile(`\(#([0-9]+)\)\s*$`)
	// mergeSubject is the subject that GitHub gives a merge commit.
	mergeSubject = regexp.MustCompile(`^Merge pull request #([0-9]+)`)
)

// Revision returns the commit of HEAD in root and its pull request. If Git
// fails, the result has no commit. The first rule that matches gives the pull
// request:
//
//  1. GITHUB_EVENT_NAME is "pull_request" and GITHUB_REF is
//     "refs/pull/<n>/merge".
//  2. The subject of HEAD ends with "(#<n>)".
//  3. The subject of HEAD starts with "Merge pull request #<n>".
func (reader gitRevisionReader) Revision(root string) Revision {
	var revision Revision
	if commit, _, err := reader.git.output(root, "rev-parse", "HEAD"); err == nil {
		revision.Commit = commit
	}

	if reader.getenv("GITHUB_EVENT_NAME") == "pull_request" {
		if number := matchNumber(pullRequestRef, reader.getenv("GITHUB_REF")); number != 0 {
			revision.PullRequest = number
			return revision
		}
	}
	if revision.Commit == "" {
		return revision
	}
	subject, _, err := reader.git.output(root, "log", "-1", "--format=%s", "HEAD")
	if err != nil {
		return revision
	}
	if number := matchNumber(squashSubject, subject); number != 0 {
		revision.PullRequest = number
	} else {
		revision.PullRequest = matchNumber(mergeSubject, subject)
	}
	return revision
}

// matchNumber returns the positive number in the first group of pattern, or 0.
func matchNumber(pattern *regexp.Regexp, value string) int {
	match := pattern.FindStringSubmatch(value)
	if match == nil {
		return 0
	}
	number, err := strconv.Atoi(match[1])
	if err != nil || number <= 0 {
		return 0
	}
	return number
}
