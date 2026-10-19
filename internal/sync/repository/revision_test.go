package repository

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

const testCommit = "3f9c2ae8d1b7c4e5f6a7b8c9d0e1f2a3b4c5d6e7"

func revisionReader(git *fakeGit, env map[string]string) gitRevisionReader {
	return gitRevisionReader{git: git, getenv: func(name string) string { return env[name] }}
}

func gitWithSubject(subject string) *fakeGit {
	return &fakeGit{outputs: map[string]gitResult{
		"rev-parse HEAD":          {output: testCommit},
		"log -1 --format=%s HEAD": {output: subject},
	}}
}

func TestRevisionReadsCommitOfHead(t *testing.T) {
	revision := revisionReader(gitWithSubject("Fix the prompt"), nil).Revision("/repo")

	assert.Equal(t, Revision{Commit: testCommit}, revision)
}

func TestRevisionPullRequestRules(t *testing.T) {
	pullRequestEvent := map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_REF": "refs/pull/142/merge"}
	tests := map[string]struct {
		subject  string
		env      map[string]string
		expected int
	}{
		"GitHub pull request event wins over the subject": {
			subject: "Fix the prompt (#7)", env: pullRequestEvent, expected: 142,
		},
		"push event uses the subject": {
			subject:  "Fix the prompt (#7)",
			env:      map[string]string{"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/pull/142/merge"},
			expected: 7,
		},
		"pull request event with another ref uses the subject": {
			subject:  "Fix the prompt (#7)",
			env:      map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_REF": "refs/heads/main"},
			expected: 7,
		},
		"squash merge subject": {subject: "Fix the prompt (#58)", expected: 58},
		"squash merge wins over merge subject": {
			subject: "Merge pull request #3 from acme/fix (#9)", expected: 9,
		},
		"merge commit subject":                {subject: "Merge pull request #12 from acme/fix", expected: 12},
		"number in the middle of the subject": {subject: "Fix (#5) in the prompt", expected: 0},
		"zero is not a pull request":          {subject: "Fix the prompt (#0)", expected: 0},
		"no pull request":                     {subject: "Fix the prompt", expected: 0},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			revision := revisionReader(gitWithSubject(test.subject), test.env).Revision("/repo")

			assert.Equal(t, testCommit, revision.Commit)
			assert.Equal(t, test.expected, revision.PullRequest)
		})
	}
}

func TestRevisionOmitsCommitWhenGitFails(t *testing.T) {
	git := &fakeGit{outputs: map[string]gitResult{
		"rev-parse HEAD":          {stderr: "fatal: ambiguous argument 'HEAD'", err: errors.New("exit status 128")},
		"log -1 --format=%s HEAD": {output: "Fix the prompt (#7)"},
	}}

	assert.Equal(t, Revision{}, revisionReader(git, nil).Revision("/repo"))

	pullRequestEvent := map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_REF": "refs/pull/142/merge"}
	assert.Equal(t, Revision{PullRequest: 142}, revisionReader(git, pullRequestEvent).Revision("/repo"))
}

func TestRevisionOmitsPullRequestWhenSubjectReadFails(t *testing.T) {
	git := &fakeGit{outputs: map[string]gitResult{
		"rev-parse HEAD":          {output: testCommit},
		"log -1 --format=%s HEAD": {output: "Fix the prompt (#7)", err: errors.New("exit status 128")},
	}}

	assert.Equal(t, Revision{Commit: testCommit}, revisionReader(git, nil).Revision("/repo"))
}
