package platform

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestRepositoryAccessClauseMatchesCanonicalRepositoryAccess(t *testing.T) {
	pool, _ := identityIntegrationStore(t)
	ctx := context.Background()
	organizationID := uuid.NewString()
	teamID := uuid.NewString()
	owner := platformTestUser("access-owner-" + uuid.NewString())
	maintainer := platformTestUser("access-maintainer-" + uuid.NewString())
	direct := platformTestUser("access-direct-" + uuid.NewString())
	revokedDirect := platformTestUser("access-revoked-" + uuid.NewString())
	teamMember := platformTestUser("access-team-" + uuid.NewString())
	suspended := platformTestUser("access-suspended-" + uuid.NewString())
	users := []User{owner, maintainer, direct, revokedDirect, teamMember, suspended}
	for _, user := range users {
		mustIdentityExec(t, pool, `
			INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
		`, user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Repository access organization', 'public', $3)
	`, organizationID, "access-"+uuid.NewString(), owner.ID)
	for _, membership := range []struct {
		userID string
		role   string
	}{
		{userID: owner.ID, role: "owner"},
		{userID: maintainer.ID, role: "maintainer"},
		{userID: teamMember.ID, role: "member"},
	} {
		mustIdentityExec(t, pool, `
			INSERT INTO organization_memberships (organization_id, user_id, role)
			VALUES ($1, $2, $3)
		`, organizationID, membership.userID, membership.role)
	}

	repositories := map[string]string{
		"public":   uuid.NewString(),
		"internal": uuid.NewString(),
		"private":  uuid.NewString(),
	}
	for visibility, repositoryID := range repositories {
		mustIdentityExec(t, pool, `
			INSERT INTO repositories (
				id, organization_id, slug, display_name, visibility,
				lore_repository_id, lore_url, default_branch, created_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7, 'main', $8)
		`, repositoryID, organizationID, visibility+"-"+uuid.NewString(),
			"Access "+visibility, visibility, canonicalTestLoreID(repositoryID),
			"lore://"+repositoryID, owner.ID)
	}

	for _, user := range []User{direct, suspended} {
		for _, visibility := range []string{"internal", "private"} {
			mustIdentityExec(t, pool, `
				INSERT INTO repository_memberships (repository_id, user_id, role)
				VALUES ($1, $2, 'read')
			`, repositories[visibility], user.ID)
		}
	}
	for _, visibility := range []string{"internal", "private"} {
		mustIdentityExec(t, pool, `
			INSERT INTO repository_memberships (repository_id, user_id, role, active)
			VALUES ($1, $2, 'read', false)
		`, repositories[visibility], revokedDirect.ID)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by)
		VALUES ($1, $2, $3, 'Repository access team', $4)
	`, teamID, organizationID, "access-team-"+uuid.NewString(), owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO team_memberships (team_id, user_id) VALUES ($1, $2)
	`, teamID, teamMember.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by)
		VALUES ($1, $2, 'read', $3)
	`, teamID, repositories["private"], owner.ID)
	mustIdentityExec(t, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspended.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		for _, user := range users {
			_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID)
		}
	})

	assertAccess := func(label string, viewerID string, expected ...string) {
		t.Helper()
		rows, err := pool.Query(ctx, `
			SELECT repository.id
			FROM repositories repository
			WHERE repository.organization_id = $1
			  AND `+repositoryAccessClause("repository", "$2")+`
		`, organizationID, viewerID)
		if err != nil {
			t.Fatalf("%s query repository access: %v", label, err)
		}
		defer rows.Close()
		actual := make(map[string]bool)
		for rows.Next() {
			var repositoryID string
			if err := rows.Scan(&repositoryID); err != nil {
				t.Fatalf("%s scan repository access: %v", label, err)
			}
			actual[repositoryID] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("%s iterate repository access: %v", label, err)
		}
		if len(actual) != len(expected) {
			t.Fatalf("%s repository count = %d, want %d: %v", label, len(actual), len(expected), actual)
		}
		for _, visibility := range expected {
			if !actual[repositories[visibility]] {
				t.Errorf("%s omitted %s repository", label, visibility)
			}
		}
	}

	assertAccess("anonymous", "", "public")
	assertAccess("external direct collaborator", direct.ID, "public", "internal", "private")
	assertAccess("inactive direct grants", revokedDirect.ID, "public")
	assertAccess("organization maintainer", maintainer.ID, "public", "internal")
	assertAccess("organization owner", owner.ID, "public", "internal", "private")
	assertAccess("active team grant", teamMember.ID, "public", "internal", "private")
	assertAccess("suspended direct collaborator", suspended.ID)

	mustIdentityExec(t, pool, `UPDATE teams SET active = false WHERE id = $1`, teamID)
	assertAccess("inactive team", teamMember.ID, "public", "internal")
	mustIdentityExec(t, pool, `UPDATE teams SET active = true WHERE id = $1`, teamID)
	mustIdentityExec(t, pool, `UPDATE team_memberships SET active = false WHERE team_id = $1`, teamID)
	assertAccess("inactive team membership", teamMember.ID, "public", "internal")
	mustIdentityExec(t, pool, `UPDATE team_memberships SET active = true WHERE team_id = $1`, teamID)
	mustIdentityExec(t, pool, `
		UPDATE organization_memberships SET active = false
		WHERE organization_id = $1 AND user_id = $2
	`, organizationID, teamMember.ID)
	assertAccess("inactive organization membership", teamMember.ID, "public")

	mustIdentityExec(t, pool, `
		UPDATE repositories SET lifecycle_state = 'pending' WHERE id = $1
	`, repositories["public"])
	assertAccess("inactive repository lifecycle", "")
	mustIdentityExec(t, pool, `
		UPDATE repositories SET lifecycle_state = 'active' WHERE id = $1
	`, repositories["public"])
	mustIdentityExec(t, pool, `UPDATE organizations SET active = false WHERE id = $1`, organizationID)
	assertAccess("inactive organization anonymous", "")
	assertAccess("inactive organization owner", owner.ID)
}
