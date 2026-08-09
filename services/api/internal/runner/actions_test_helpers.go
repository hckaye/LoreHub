package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func countRepositoryRows(ctx context.Context, pool *pgxpool.Pool, table string, repositoryID string) (int, error) {
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE repository_id = $1", repositoryID).
		Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func countActionEvents(ctx context.Context, pool *pgxpool.Pool, repositoryID string) (int, error) {
	var auditCount, outboxCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_events WHERE repository_id = $1 AND action LIKE 'actions.%'",
		repositoryID).Scan(&auditCount); err != nil {
		return 0, err
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbox_events WHERE topic LIKE 'actions.%'").
		Scan(&outboxCount); err != nil {
		return 0, err
	}
	return auditCount + outboxCount, nil
}

type actionsFixture struct {
	pool           *pgxpool.Pool
	userID         string
	organizationID string
	repositoryID   string
	owner          string
	repositorySlug string
}

func newActionsFixture(t *testing.T, pool *pgxpool.Pool) actionsFixture {
	t.Helper()
	fixture := actionsFixture{
		pool:           pool,
		userID:         uuid.NewString(),
		organizationID: uuid.NewString(),
		repositoryID:   uuid.NewString(),
		owner:          "actions-test-" + strings.ToLower(uuid.NewString()[:8]),
		repositorySlug: "runtime-" + strings.ToLower(uuid.NewString()[:8]),
	}
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, 'Actions Test')
	`, fixture.userID, "actions-user-"+strings.ToLower(uuid.NewString()[:8]))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO organizations (id, slug, display_name, created_by)
		VALUES ($1, $2, 'Actions Test', $3)
	`, fixture.organizationID, fixture.owner, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'owner')
	`, fixture.organizationID, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Runtime', $5, $6, 'main', $4)
	`, fixture.repositoryID, fixture.organizationID, fixture.repositorySlug, fixture.userID,
		strings.ReplaceAll(fixture.repositoryID, "-", ""), "lore://fixture/"+fixture.repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'admin')
	`, fixture.repositoryID, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, fixture.repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture actionsFixture) cleanup(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, "DELETE FROM organizations WHERE id = $1", fixture.organizationID); err != nil {
		t.Error(err)
	}
	if _, err := fixture.pool.Exec(ctx, "DELETE FROM users WHERE id = $1", fixture.userID); err != nil {
		t.Error(err)
	}
}

func workflowDefinition(path string, name string, branches []string, dispatch bool) WorkflowDefinition {
	config := map[string]any{}
	if branches != nil {
		config["push"] = map[string]any{"branches": branches}
	}
	if dispatch {
		config["workflow_dispatch"] = map[string]any{}
	}
	encoded, _ := json.Marshal(config)
	return WorkflowDefinition{
		Path: path, Name: name, Enabled: true, State: "active", Push: triggerFromBranches(branches),
		WorkflowDispatch: dispatch, TriggerConfig: encoded,
	}
}

func workflowDispatchDefinition(path string, name string) WorkflowDefinition {
	inputs := map[string]WorkflowDispatchInput{
		"channel": {
			Description: "Release channel",
			Type:        "choice",
			Default:     stringPointer("stable"),
			Options:     []string{"stable", "beta"},
		},
	}
	config := map[string]any{"workflow_dispatch": WorkflowDispatchConfig{Inputs: inputs}}
	encoded, _ := json.Marshal(config)
	return WorkflowDefinition{
		Path: path, Name: name, Enabled: true, State: "active", WorkflowDispatch: true,
		DispatchInputs: inputs, TriggerConfig: encoded,
	}
}

func stringPointer(value string) *string {
	return &value
}

func workflowWithTriggerConfig(path string, name string, config map[string]any) WorkflowDefinition {
	encoded, _ := json.Marshal(config)
	definition := WorkflowDefinition{
		Path: path, Name: name, Enabled: true, State: "active", TriggerConfig: encoded,
	}
	if value, ok := config["pull_request"].(*PullRequestTrigger); ok {
		definition.PullRequest = value
	}
	if value, ok := config["repository_dispatch"].(*RepositoryDispatchTrigger); ok {
		definition.RepositoryDispatch = value
	}
	if value, ok := config["schedule"].([]ScheduleTrigger); ok {
		definition.Schedules = value
	}
	return definition
}

func triggerFromBranches(branches []string) *PushTrigger {
	if branches == nil {
		return nil
	}
	return &PushTrigger{Branches: branches}
}
