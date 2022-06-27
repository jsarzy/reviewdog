package gerrit

import (
	"context"
	"fmt"
	"os/exec"

	"golang.org/x/build/gerrit"

	"github.com/reviewdog/reviewdog"
	"github.com/reviewdog/reviewdog/service/serviceutil"
)

const (
	stripDiffResult = 1
)

var _ reviewdog.DiffService = &ChangeDiff{}

// ChangeDiff is a diff service for Gerrit changes.
type ChangeDiff struct {
	cli        *gerrit.Client
	changeID   string
	revisionID string

	// wd is working directory relative to root of repository.
	wd string
}

// NewChangeDiff returns a new ChangeDiff service,
// it needs git command in $PATH.
func NewChangeDiff(cli *gerrit.Client, changeID, revisionID string) (*ChangeDiff, error) {
	workDir, err := serviceutil.GitRelWorkdir()
	if err != nil {
		return nil, fmt.Errorf("ChangeDiff needs 'git' command: %w", err)
	}
	return &ChangeDiff{
		cli:        cli,
		changeID:   changeID,
		revisionID: revisionID,
		wd:         workDir,
	}, nil
}

// Diff returns a diff of MergeRequest. It runs `git diff` locally instead of
// diff_url of GitLab Merge Request because diff of diff_url is not suited for
// comment API in a sense that diff of diff_url is equivalent to
// `git diff --no-renames`, we want diff which is equivalent to
// `git diff --find-renames`.
func (g *ChangeDiff) Diff(ctx context.Context) ([]byte, error) {
	return g.gitDiff(ctx)
}

func (g *ChangeDiff) gitDiff(ctx context.Context) ([]byte, error) {
	bytes, err := exec.Command("git", "diff", "--find-renames", g.revisionID+string("~"), g.revisionID).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run git diff: %w", err)
	}

	return bytes, nil
}

// Strip returns 1 as a strip of git diff.
func (g *ChangeDiff) Strip() int {
	return stripDiffResult
}
