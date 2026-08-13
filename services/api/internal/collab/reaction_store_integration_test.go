package collab

import (
	"context"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestReactionMutationsAreUniqueAndAggregatedPostgres(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "write")
	issueID := seedRepositoryIssueListItem(
		t, ctx, fixture, "Reaction issue", "Issue body", "open", fixture.alice.ID,
	)
	mergeRequestID := seedRepositoryPullRequestListItem(
		t, ctx, fixture, "Reaction pull request", "Pull request body", "open", false,
		"feature/reactions", "main", fixture.alice.ID,
	)
	issueCommentID := uuidNew()
	mergeRequestCommentID := uuidNew()
	mustExec(t, ctx, pool, `
		INSERT INTO issue_comments (id, issue_id, author_id, body)
		VALUES ($1, $2, $3, 'Issue comment')
	`, issueCommentID, issueID, fixture.alice.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO merge_request_comments (id, merge_request_id, author_id, body)
		VALUES ($1, $2, $3, 'Pull request comment')
	`, mergeRequestCommentID, mergeRequestID, fixture.alice.ID)

	issueLike := ReactionInput{
		SubjectKind: reactionIssue, SubjectID: issueID, Reaction: "+1",
	}
	assertReactionCount(t, mustPutReaction(t, store, fixture.alice, fixture.repoID, issueLike), "+1", 1, true)
	assertReactionCount(t, mustPutReaction(t, store, fixture.alice, fixture.repoID, issueLike), "+1", 1, true)
	assertReactionCount(t, mustPutReaction(t, store, fixture.bob, fixture.repoID, issueLike), "+1", 2, true)

	issueHeart := ReactionInput{SubjectKind: reactionIssue, SubjectID: issueID, Reaction: "heart"}
	assertReactionCount(t, mustPutReaction(t, store, fixture.alice, fixture.repoID, issueHeart), "heart", 1, true)

	issue, err := store.GetIssueWithReactions(ctx, fixture.repoID, 1, fixture.alice.Username)
	if err != nil {
		t.Fatal(err)
	}
	assertReactionCount(t, issue.Reactions, "+1", 2, true)
	assertReactionCount(t, issue.Reactions, "heart", 1, true)

	issueForBob, err := store.GetIssueWithReactions(ctx, fixture.repoID, 1, fixture.bob.Username)
	if err != nil {
		t.Fatal(err)
	}
	assertReactionCount(t, issueForBob.Reactions, "+1", 2, true)
	assertReactionCount(t, issueForBob.Reactions, "heart", 1, false)

	commentLike := ReactionInput{
		SubjectKind: reactionIssueComment, SubjectID: issueCommentID, Reaction: "laugh",
	}
	mustPutReaction(t, store, fixture.alice, fixture.repoID, commentLike)
	mustPutReaction(t, store, fixture.bob, fixture.repoID, commentLike)
	comments, err := store.ListIssueCommentsWithReactions(
		ctx, fixture.repoID, 1, Page{Limit: 10}, fixture.alice.Username,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments.Items) != 1 {
		t.Fatalf("issue comments = %#v", comments)
	}
	assertReactionCount(t, comments.Items[0].Reactions, "laugh", 2, true)

	pullRequestLike := ReactionInput{
		SubjectKind: reactionMergeRequest, SubjectID: mergeRequestID, Reaction: "rocket",
	}
	mustPutReaction(t, store, fixture.alice, fixture.repoID, pullRequestLike)
	pullRequest, err := store.GetMergeRequestWithReactions(
		ctx, fixture.repoID, 1, fixture.bob.Username,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertReactionCount(t, pullRequest.Reactions, "rocket", 1, false)

	pullRequestCommentLike := ReactionInput{
		SubjectKind: reactionMergeRequestComment, SubjectID: mergeRequestCommentID, Reaction: "eyes",
	}
	mustPutReaction(t, store, fixture.bob, fixture.repoID, pullRequestCommentLike)
	pullRequestComments, err := store.ListMergeRequestCommentsWithReactions(
		ctx, fixture.repoID, 1, Page{Limit: 10}, fixture.bob.Username,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(pullRequestComments.Items) != 1 {
		t.Fatalf("pull request comments = %#v", pullRequestComments)
	}
	assertReactionCount(t, pullRequestComments.Items[0].Reactions, "eyes", 1, true)

	removed := mustDeleteReaction(t, store, fixture.alice, fixture.repoID, issueLike)
	assertReactionCount(t, removed, "+1", 1, false)
	removedAgain := mustDeleteReaction(t, store, fixture.alice, fixture.repoID, issueLike)
	assertReactionCount(t, removedAgain, "+1", 1, false)
}

func mustPutReaction(
	t *testing.T,
	store *store,
	actor platform.User,
	repositoryID string,
	input ReactionInput,
) []Reaction {
	t.Helper()
	mutation, err := store.PutReaction(context.Background(), actor, repositoryID, input)
	if err != nil {
		t.Fatalf("put reaction: %v", err)
	}
	return mutation.Reactions
}

func mustDeleteReaction(
	t *testing.T,
	store *store,
	actor platform.User,
	repositoryID string,
	input ReactionInput,
) []Reaction {
	t.Helper()
	mutation, err := store.DeleteReaction(context.Background(), actor, repositoryID, input)
	if err != nil {
		t.Fatalf("delete reaction: %v", err)
	}
	return mutation.Reactions
}

func assertReactionCount(t *testing.T, reactions []Reaction, name string, count int64, viewerReacted bool) {
	t.Helper()
	for _, reaction := range reactions {
		if reaction.Reaction == name {
			if reaction.Count != count || reaction.ViewerReacted != viewerReacted {
				t.Fatalf("reaction %q = %#v, want count=%d viewerReacted=%t", name, reaction, count, viewerReacted)
			}
			return
		}
	}
	t.Fatalf("reaction %q not found in %#v", name, reactions)
}
