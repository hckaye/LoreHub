package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (store *Store) Dashboard(ctx context.Context, actor User) (Dashboard, error) {
	repositories, err := store.accessibleRepositories(ctx, actor.ID, 50)
	if err != nil {
		return Dashboard{}, err
	}
	organizations, err := store.listOrganizationViews(ctx, actor.ID, 50)
	if err != nil {
		return Dashboard{}, err
	}
	notifications, err := store.ListNotifications(ctx, actor, false, 6)
	if err != nil {
		return Dashboard{}, err
	}
	unread, err := store.UnreadNotificationCount(ctx, actor)
	if err != nil {
		return Dashboard{}, err
	}
	return Dashboard{
		Repositories:        repositories,
		Organizations:       organizations,
		Notifications:       notifications.Items,
		UnreadNotifications: unread,
	}, nil
}

func (store *Store) UserProfile(ctx context.Context, viewer *User, username string) (UserProfile, error) {
	viewerID := ""
	if viewer != nil {
		viewerID = viewer.ID
	}
	var profile UserProfile
	var email string
	err := store.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.display_name, COALESCE(u.email, ''), u.bio,
		       u.avatar_url, u.website_url, u.location, u.company, u.pronouns, u.locale,
		       u.created_at, COUNT(DISTINCT r.id)
		FROM users u
		LEFT JOIN organization_memberships owned
		  ON owned.user_id = u.id AND owned.role IN ('owner', 'maintainer') AND owned.active
		LEFT JOIN repositories r ON r.organization_id = owned.organization_id
		  AND `+repositoryAccessClause("r", "$2")+`
		WHERE lower(u.username) = lower($1) AND u.status = 'active'
		GROUP BY u.id
	`, username, viewerID).Scan(
		&profile.ID,
		&profile.Username,
		&profile.DisplayName,
		&email,
		&profile.Bio,
		&profile.AvatarURL,
		&profile.WebsiteURL,
		&profile.Location,
		&profile.Company,
		&profile.Pronouns,
		&profile.Locale,
		&profile.CreatedAt,
		&profile.RepositoryCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserProfile{}, ErrNotFound
	}
	if err != nil {
		return UserProfile{}, fmt.Errorf("get user profile: %w", err)
	}
	if email != "" && viewer != nil && viewer.ID == profile.ID {
		profile.Email = &email
	}
	return profile, nil
}

func (store *Store) UserRepositories(
	ctx context.Context,
	viewer *User,
	username string,
) ([]Repository, error) {
	viewerID := ""
	if viewer != nil {
		viewerID = viewer.ID
	}
	rows, err := store.pool.Query(ctx, repositorySelect+`
		JOIN organization_memberships owner_membership
		  ON owner_membership.organization_id = r.organization_id AND owner_membership.active
		JOIN users owner ON owner.id = owner_membership.user_id
		WHERE lower(owner.username) = lower($1)
		  AND owner.status = 'active'
		  AND owner_membership.role IN ('owner', 'maintainer')
		  AND `+repositoryAccessClause("r", "$2")+`
		GROUP BY r.id, o.slug
		ORDER BY r.updated_at DESC
		LIMIT 100
	`, username, viewerID)
	if err != nil {
		return nil, fmt.Errorf("list user repositories: %w", err)
	}
	defer rows.Close()
	return scanRepositories(rows)
}

func (store *Store) UpdateProfile(ctx context.Context, actor User, input UpdateProfileInput) (UserProfile, error) {
	sets := []string{"updated_at = now()"}
	args := []any{actor.ID}
	appendProfileUpdate(&sets, &args, "display_name", input.DisplayName)
	appendProfileUpdate(&sets, &args, "bio", input.Bio)
	appendProfileUpdate(&sets, &args, "avatar_url", input.AvatarURL)
	appendProfileUpdate(&sets, &args, "website_url", input.WebsiteURL)
	appendProfileUpdate(&sets, &args, "location", input.Location)
	appendProfileUpdate(&sets, &args, "company", input.Company)
	appendProfileUpdate(&sets, &args, "pronouns", input.Pronouns)
	if len(sets) == 1 {
		return store.UserProfile(ctx, &actor, actor.Username)
	}
	for index := 1; index < len(args); index++ {
		sets[index] = strings.Replace(sets[index], "?", fmt.Sprintf("$%d", index+1), 1)
	}
	query := fmt.Sprintf("UPDATE users SET %s WHERE id = $1 AND status = 'active'", strings.Join(sets, ", "))
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return UserProfile{}, fmt.Errorf("begin profile update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := transaction.Exec(ctx, query, args...); err != nil {
		return UserProfile{}, fmt.Errorf("update profile: %w", err)
	}
	if err := insertAudit(ctx, transaction, actor.ID, "", "", "profile.update", "user", actor.ID); err != nil {
		return UserProfile{}, err
	}
	if err := insertOutbox(ctx, transaction, "profile.updated", actor.ID+":"+uuid.NewString(), input); err != nil {
		return UserProfile{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return UserProfile{}, fmt.Errorf("commit profile update: %w", err)
	}
	return store.UserProfile(ctx, &actor, actor.Username)
}

func appendProfileUpdate(sets *[]string, args *[]any, column string, value *string) {
	if value == nil {
		return
	}
	*sets = append(*sets, column+" = ?")
	*args = append(*args, strings.TrimSpace(*value))
}

func (store *Store) accessibleRepositories(ctx context.Context, userID string, limit int) ([]Repository, error) {
	rows, err := store.pool.Query(ctx, repositorySelect+`
		WHERE 1 = 1
		  AND `+repositoryAccessClause("r", "$1")+`
		GROUP BY r.id, o.slug
		ORDER BY r.updated_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list accessible repositories: %w", err)
	}
	defer rows.Close()
	return scanRepositories(rows)
}

func (store *Store) listOrganizationViews(
	ctx context.Context,
	userID string,
	limit int,
) ([]OrganizationView, error) {
	query := `
		SELECT o.id, o.slug, o.display_name, o.description, o.visibility,
		       o.website_url, o.contact_email, o.default_repository_visibility,
		       COALESCE(viewer.role, ''), COUNT(DISTINCT members.user_id),
		       COUNT(DISTINCT r.id) FILTER (WHERE ` + repositoryAccessClause("r", "$1") + `),
		       COUNT(DISTINCT t.id), o.created_at
		FROM organizations o
		LEFT JOIN organization_memberships viewer
		  ON viewer.organization_id = o.id AND viewer.user_id = $1
		 AND viewer.active
		 AND EXISTS (
		     SELECT 1 FROM users viewer_user
		     WHERE viewer_user.id = viewer.user_id AND viewer_user.status = 'active'
		 )
		LEFT JOIN organization_memberships members
		  ON members.organization_id = o.id AND members.active
		LEFT JOIN users member_user ON member_user.id = members.user_id AND member_user.status = 'active'
		LEFT JOIN repositories r ON r.organization_id = o.id
		  AND r.lifecycle_state = 'active'
		LEFT JOIN teams t ON t.organization_id = o.id AND t.active
		WHERE o.active AND (o.visibility = 'public' OR viewer.user_id IS NOT NULL)
		GROUP BY o.id, viewer.role
		ORDER BY o.updated_at DESC
		LIMIT $2
	`
	rows, err := store.pool.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	defer rows.Close()
	return scanOrganizationViews(rows)
}

func scanOrganizationViews(rows pgx.Rows) ([]OrganizationView, error) {
	views := make([]OrganizationView, 0)
	for rows.Next() {
		var view OrganizationView
		if err := rows.Scan(
			&view.ID, &view.Slug, &view.DisplayName, &view.Description, &view.Visibility,
			&view.WebsiteURL, &view.ContactEmail, &view.DefaultRepositoryVisibility,
			&view.Role, &view.MemberCount, &view.RepositoryCount, &view.TeamCount, &view.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan organization: %w", err)
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate organizations: %w", err)
	}
	return views, nil
}
