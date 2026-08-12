package platform

import (
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const repositorySelect = `
		SELECT r.id, r.organization_id, o.slug, r.slug, r.display_name, r.description,
		       r.visibility, r.lore_repository_id, r.lore_url, r.default_branch,
		       r.homepage_url, r.allow_issues, r.allow_merge_requests,
		       COALESCE((
		           SELECT jsonb_agg(topic.topic ORDER BY topic.topic)
		           FROM repository_topics topic WHERE topic.repository_id = r.id
		       ), '[]'::jsonb),
		       COUNT(DISTINCT i.id) FILTER (WHERE i.state = 'open'),
		       COUNT(DISTINCT mr.id) FILTER (WHERE mr.state = 'open'),
		       r.archived_at, r.lifecycle_state, COALESCE(r.provisioning_error, ''), r.updated_at
	FROM repositories r
	JOIN organizations o ON o.id = r.organization_id AND o.active
	LEFT JOIN issues i ON i.repository_id = r.id
	LEFT JOIN merge_requests mr ON mr.repository_id = r.id
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRepository(row rowScanner) (Repository, error) {
	var repository Repository
	var topicsJSON []byte
	err := row.Scan(
		&repository.ID,
		&repository.OrganizationID,
		&repository.Owner,
		&repository.Slug,
		&repository.DisplayName,
		&repository.Description,
		&repository.Visibility,
		&repository.LoreRepositoryID,
		&repository.LoreURL,
		&repository.DefaultBranch,
		&repository.HomepageURL,
		&repository.AllowIssues,
		&repository.AllowMergeRequests,
		&topicsJSON,
		&repository.IssueCount,
		&repository.MergeRequestCount,
		&repository.ArchivedAt,
		&repository.LifecycleState,
		&repository.ProvisioningError,
		&repository.UpdatedAt,
	)
	if err == nil {
		err = json.Unmarshal(topicsJSON, &repository.Topics)
	}
	return repository, err
}

func scanRepositories(rows pgx.Rows) ([]Repository, error) {
	repositories := make([]Repository, 0)
	for rows.Next() {
		repository, err := scanRepository(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repository: %w", err)
		}
		repositories = append(repositories, repository)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repositories: %w", err)
	}
	return repositories, nil
}
