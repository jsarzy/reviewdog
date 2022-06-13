package gerrit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/build/gerrit"

	"github.com/reviewdog/reviewdog"
	"github.com/reviewdog/reviewdog/proto/rdf"
	"github.com/reviewdog/reviewdog/service/commentutil"
	"github.com/reviewdog/reviewdog/service/serviceutil"
)

var _ reviewdog.CommentService = &ChangeReviewCommenter{}

// ChangeReviewCommenter is a comment service for Gerrit Change Review
// API:
// 	https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#set-review
// 	POST /changes/{change-id}/revisions/{revision-id}/review
type ChangeReviewCommenter struct {
	cli        *gerrit.Client
	changeID   string
	revisionID string

	muComments   sync.Mutex
	postComments []*reviewdog.Comment

	// wd is working directory relative to root of repository.
	wd string
}

// NewChangeReviewCommenter returns a new NewChangeReviewCommenter service.
// ChangeReviewCommenter service needs git command in $PATH.
func NewChangeReviewCommenter(cli *gerrit.Client, changeID, revisionID string) (*ChangeReviewCommenter, error) {
	workDir, err := serviceutil.GitRelWorkdir()
	if err != nil {
		return nil, fmt.Errorf("ChangeReviewCommenter needs 'git' command: %w", err)
	}

	return &ChangeReviewCommenter{
		cli:          cli,
		changeID:     changeID,
		revisionID:   revisionID,
		postComments: []*reviewdog.Comment{},
		wd:           workDir,
	}, nil
}

// Post accepts a comment and holds it. Flush method actually posts comments to Gerrit
func (g *ChangeReviewCommenter) Post(_ context.Context, c *reviewdog.Comment) error {
	c.Result.Diagnostic.GetLocation().Path = filepath.Join(g.wd, c.Result.Diagnostic.GetLocation().GetPath())
	g.muComments.Lock()
	defer g.muComments.Unlock()
	g.postComments = append(g.postComments, c)
	return nil
}

// Flush posts comments which has not been posted yet.
func (g *ChangeReviewCommenter) Flush(ctx context.Context) error {
	g.muComments.Lock()
	defer g.muComments.Unlock()

	return g.postAllComments(ctx)
}

func buildCommentRange(s *rdf.Suggestion) gerrit.CommentRange {
	return gerrit.CommentRange{
		StartLine:      int(s.Range.Start.Line),
		StartCharacter: int(s.Range.Start.Column) - 1, // Gerrit uses 0-based indexed columns
		EndLine:        int(s.Range.End.Line),
		EndCharacter:   int(s.Range.End.Column) - 1, // Gerrit uses 0-based indexed columns
	}
}

func buildFixSuggestion(c *reviewdog.Comment, s *rdf.Suggestion) gerrit.FixSuggestionInfo {
	path := c.Result.Diagnostic.GetLocation().GetPath()

	return gerrit.FixSuggestionInfo{
		Description: "suggestion",
		Replacements: []gerrit.FixReplacementInfo{
			{
				Path:        path,
				Replacement: s.Text,
				Range:       buildCommentRange(s),
			},
		},
	}
}

func gerritCommentLine(c *reviewdog.Comment) int {
	if c.Result.FirstSuggestionInDiffContext && len(c.Result.Diagnostic.Suggestions) > 0 {
		// Prefer first suggestion start line
		s := c.Result.Diagnostic.Suggestions[0]
		return int(s.GetRange().GetStart().GetLine())
	}
	return int(c.Result.Diagnostic.GetLocation().GetRange().GetStart().GetLine())
}

func buildRobotComment(c *reviewdog.Comment) gerrit.RobotCommentInput {
	msg := commentutil.GerritComment(c)

	line := gerritCommentLine(c)

	robotComment := gerrit.RobotCommentInput{
		CommentInput: gerrit.CommentInput{
			Line:    line,
			Message: msg,
		},
		RobotID:        "reviewdog 🐶",
		RobotRunID:     os.Getenv("GERRIT_REVIEWDOG_RUN_ID"),
		URL:            os.Getenv("GERRIT_REVIEWDOG_RUN_URL"),
		FixSuggestions: make([]gerrit.FixSuggestionInfo, 0, len(c.Result.Diagnostic.Suggestions)),
	}

	for i, s := range c.Result.Diagnostic.Suggestions {
		fixSuggestion := buildFixSuggestion(c, s)
		robotComment.FixSuggestions = append(robotComment.FixSuggestions, fixSuggestion)
	}

	return robotComment
}

func (g *ChangeReviewCommenter) postAllComments(ctx context.Context) error {
	review := gerrit.ReviewInput{
		RobotComments: map[string][]gerrit.RobotCommentInput{},
	}
	for _, c := range g.postComments {
		if !c.Result.InDiffFile {
			continue
		}

		//TODO(kuba) do we need this?
		if !c.Result.ShouldReport {
			fmt.Println("Comment should not be reported")
			continue
		}

		path := c.Result.Diagnostic.GetLocation().GetPath()
		robotComment := buildRobotComment(c)

		review.RobotComments[path] = append(review.RobotComments[path], robotComment)
	}

	// Here we set g.revisionID, but in Diff we ask for current_revision
	return g.cli.SetReview(ctx, g.changeID, g.revisionID, review)

}
