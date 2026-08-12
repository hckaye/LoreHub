package discussions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestPostgresDiscussionOutsideAccessTeamGrantAndPublicRepository(t *testing.T) {
	fixture := openDiscussionFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)

	if _, err := store.Create(ctx, fixture.outsider, fixture.repo, CreateInput{
		CategorySlug: "general", Title: "Outside private discussion",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("outside private create error = %v, want forbidden", err)
	}

	teamID := uuid.NewString()
	mustDiscussionExec(t, fixture.pool, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by)
		VALUES ($1, $2, $3, 'External team', $4)
	`, teamID, fixture.orgID, "external-"+teamID[:8], fixture.owner.ID)
	mustDiscussionExec(t, fixture.pool, `
		INSERT INTO team_memberships (team_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, teamID, fixture.outsider.ID)
	mustDiscussionExec(t, fixture.pool, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by)
		VALUES ($1, $2, 'write', $3)
	`, teamID, fixture.repoID, fixture.owner.ID)
	teamDiscussion, err := store.Create(ctx, fixture.outsider, fixture.repo, CreateInput{
		CategorySlug: "general", Title: "Team grant discussion",
	})
	if err != nil || teamDiscussion.Author.ID != fixture.outsider.ID {
		t.Fatalf("team grant discussion = %+v, error = %v", teamDiscussion, err)
	}

	publicFixture := openDiscussionFixture(t)
	publicStore := NewStore(publicFixture.pool)
	if _, err := publicFixture.pool.Exec(ctx, `
		UPDATE repositories SET visibility = 'public' WHERE id = $1
	`, publicFixture.repoID); err != nil {
		t.Fatal(err)
	}
	publicDiscussion, err := publicStore.Create(ctx, publicFixture.outsider, publicFixture.repo, CreateInput{
		CategorySlug: "general", Title: "Public discussion",
	})
	if err != nil {
		t.Fatalf("public outsider create: %v", err)
	}
	if _, err := publicStore.CreateComment(
		ctx, publicFixture.outsider, publicFixture.repo, publicDiscussion.Number, nil, "Public reply",
	); err != nil {
		t.Fatalf("public outsider comment: %v", err)
	}
	if _, err := publicStore.SetVote(
		ctx, publicFixture.outsider, publicFixture.repo, publicDiscussion.Number, true,
	); err != nil {
		t.Fatalf("public outsider vote: %v", err)
	}
}

func TestPostgresDiscussionCrossRepositoryCommentAndAnswerBoundary(t *testing.T) {
	fixture := openDiscussionFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	other := seedDiscussionRepository(t, fixture)

	first, err := store.Create(ctx, fixture.owner, fixture.repo, CreateInput{
		CategorySlug: "questions", Title: "First repository question",
	})
	if err != nil {
		t.Fatalf("first discussion: %v", err)
	}
	firstComment, err := store.CreateComment(
		ctx, fixture.owner, fixture.repo, first.Number, nil, "First repository answer",
	)
	if err != nil {
		t.Fatalf("first comment: %v", err)
	}
	second, err := store.Create(ctx, fixture.owner, other, CreateInput{
		CategorySlug: "questions", Title: "Second repository question",
	})
	if err != nil {
		t.Fatalf("second discussion: %v", err)
	}
	if _, err := store.CreateComment(
		ctx, fixture.owner, other, second.Number, &firstComment.ID, "Cross repository reply",
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository parent error = %v, want not found", err)
	}
	if _, err := store.SetAnswer(
		ctx, fixture.owner, other, second.Number, firstComment.ID, true,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository answer error = %v, want not found", err)
	}
	loaded, err := store.Get(ctx, other.ID, second.Number, fixture.owner.ID, 1, 100)
	if err != nil || loaded.Answered || len(loaded.Comments) != 0 {
		t.Fatalf("second discussion after cross-boundary attempts = %+v, error = %v", loaded, err)
	}
}

func TestPostgresDiscussionCategoryUpdateClearsAnswersAndEmptyDelete(t *testing.T) {
	fixture := openDiscussionFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	question, err := store.Create(ctx, fixture.owner, fixture.repo, CreateInput{
		CategorySlug: "questions", Title: "Category format question",
	})
	if err != nil {
		t.Fatalf("create category question: %v", err)
	}
	comment, err := store.CreateComment(
		ctx, fixture.owner, fixture.repo, question.Number, nil, "Accepted answer",
	)
	if err != nil {
		t.Fatalf("create category answer: %v", err)
	}
	if _, err := store.SetAnswer(ctx, fixture.owner, fixture.repo, question.Number, comment.ID, true); err != nil {
		t.Fatalf("set category answer: %v", err)
	}
	updated, err := store.UpdateCategory(ctx, fixture.owner, fixture.repo, "questions", CategoryInput{
		Slug: "questions", Name: "Questions", Description: "General discussions", Format: "discussion",
	})
	if err != nil || updated.Format != "discussion" {
		t.Fatalf("updated category = %+v, error = %v", updated, err)
	}
	loaded, err := store.Get(ctx, fixture.repoID, question.Number, fixture.owner.ID, 1, 100)
	if err != nil || loaded.Answered {
		t.Fatalf("answer after category format update = %+v, error = %v", loaded, err)
	}

	empty, err := store.CreateCategory(ctx, fixture.owner, fixture.repo, CategoryInput{
		Slug: "empty", Name: "Empty", Description: "", Format: "discussion",
	})
	if err != nil {
		t.Fatalf("create empty category: %v", err)
	}
	if err := store.DeleteCategory(ctx, fixture.owner, fixture.repo, empty.Slug); err != nil {
		t.Fatalf("delete empty category: %v", err)
	}
	if err := store.DeleteCategory(ctx, fixture.owner, fixture.repo, empty.Slug); !errors.Is(
		err, platform.ErrNotFound,
	) {
		t.Fatalf("delete missing category error = %v, want not found", err)
	}
}

func TestPostgresDiscussionInactiveOrganizationRejectsMutations(t *testing.T) {
	fixture := openDiscussionFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	if _, err := fixture.pool.Exec(ctx, `
		UPDATE organizations SET active = false WHERE id = $1
	`, fixture.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, fixture.owner, fixture.repo, CreateInput{
		CategorySlug: "general", Title: "Inactive organization discussion",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("inactive organization create error = %v, want forbidden", err)
	}
	if _, err := store.CreateCategory(ctx, fixture.owner, fixture.repo, CategoryInput{
		Slug: "inactive", Name: "Inactive", Format: "discussion",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("inactive organization category error = %v, want forbidden", err)
	}
}

func seedDiscussionRepository(t *testing.T, fixture discussionFixture) RepositoryRef {
	t.Helper()
	repositoryID := uuid.NewString()
	mustDiscussionExec(t, fixture.pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Other', '', 'private', $4, $5, 'main', $6)
	`, repositoryID, fixture.orgID, "other-"+repositoryID[:8], compactDiscussionUUID(repositoryID),
		"https://lore.invalid/"+repositoryID, fixture.owner.ID)
	mustDiscussionExec(t, fixture.pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	return RepositoryRef{ID: repositoryID, OrganizationID: fixture.orgID}
}
