package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
)

type notificationRecipient struct {
	ID           string
	Locale       string
	InAppEnabled bool
	EmailEnabled bool
}

func (store *Store) notificationRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	scope notificationScope,
	event notificationEvent,
) ([]notificationRecipient, error) {
	if event.Topic == "repository.invitation.created" {
		return repositoryInvitationNotificationRecipients(ctx, transaction, event)
	}
	if strings.HasPrefix(event.Topic, "merge_request_review_request.") {
		return reviewRequestNotificationRecipients(ctx, transaction, event)
	}
	if scope.Kind == "team" || strings.HasPrefix(event.Topic, "team.") {
		return store.teamNotificationRecipients(ctx, transaction, scope.TeamID)
	}
	if scope.Kind == "organization" || scope.RepositoryID == "" {
		return store.organizationNotificationRecipients(ctx, transaction, scope.OrganizationID)
	}
	return store.repositoryNotificationRecipients(ctx, transaction, scope)
}

func (store *Store) teamNotificationRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	teamID string,
) ([]notificationRecipient, error) {
	rows, err := transaction.Query(ctx, `
		SELECT DISTINCT tm.user_id::text,
		       CASE WHEN recipient.locale = 'ja' THEN 'ja' ELSE 'en' END,
		       COALESCE(preferences.in_app_enabled, true),
		       COALESCE(preferences.email_enabled, false)
		FROM team_memberships tm
		JOIN teams t ON t.id = tm.team_id AND t.active
		JOIN organizations o ON o.id = t.organization_id AND o.active
		JOIN organization_memberships om
		  ON om.organization_id = o.id AND om.user_id = tm.user_id AND om.active
		JOIN users recipient ON recipient.id = tm.user_id AND recipient.status = 'active'
		LEFT JOIN notification_preferences preferences ON preferences.user_id = tm.user_id
		WHERE tm.team_id = $1
		  AND tm.active
		  AND COALESCE(preferences.team_enabled, true)
		  AND (
		      COALESCE(preferences.in_app_enabled, true)
		      OR COALESCE(preferences.email_enabled, false)
		  )
	`, teamID)
	if err != nil {
		return nil, fmt.Errorf("list team notification recipients: %w", err)
	}
	return scanNotificationRecipients(rows)
}

func (store *Store) organizationNotificationRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	organizationID string,
) ([]notificationRecipient, error) {
	rows, err := transaction.Query(ctx, `
		SELECT DISTINCT members.user_id::text,
		       CASE WHEN recipient.locale = 'ja' THEN 'ja' ELSE 'en' END,
		       COALESCE(preferences.in_app_enabled, true),
		       COALESCE(preferences.email_enabled, false)
		FROM organizations o
		JOIN organization_memberships members ON members.organization_id = o.id
		JOIN users recipient ON recipient.id = members.user_id AND recipient.status = 'active'
		LEFT JOIN notification_preferences preferences ON preferences.user_id = members.user_id
		WHERE o.id = $1 AND o.active
		  AND members.active
		  AND (
		      COALESCE(preferences.in_app_enabled, true)
		      OR COALESCE(preferences.email_enabled, false)
		  )
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization notification recipients: %w", err)
	}
	return scanNotificationRecipients(rows)
}

func (store *Store) repositoryNotificationRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	scope notificationScope,
) ([]notificationRecipient, error) {
	rows, err := transaction.Query(ctx, `
		WITH eligible AS (
			SELECT members.user_id
			FROM organization_memberships members
			JOIN organizations o ON o.id = members.organization_id AND o.active
			JOIN users recipient ON recipient.id = members.user_id AND recipient.status = 'active'
			WHERE members.organization_id = $1
			  AND members.active
			  AND ($3 = 'internal' OR ($3 = 'private' AND members.role = 'owner'))
			UNION
			SELECT repository_members.user_id
			FROM repository_memberships repository_members
			JOIN repositories r ON r.id = repository_members.repository_id
			  AND r.organization_id = $1 AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			JOIN users recipient ON recipient.id = repository_members.user_id
			WHERE repository_members.repository_id = $2
			  AND repository_members.active
			  AND recipient.status = 'active'
			  AND $3 IN ('public', 'internal', 'private')
			UNION
			SELECT team_members.user_id
			FROM team_memberships team_members
			JOIN team_repository_roles team_repositories
			  ON team_repositories.team_id = team_members.team_id AND team_repositories.active
			JOIN teams t ON t.id = team_members.team_id AND t.active AND t.organization_id = $1
			JOIN organizations o ON o.id = t.organization_id AND o.active
			JOIN organization_memberships team_org_member
			  ON team_org_member.organization_id = o.id
			 AND team_org_member.user_id = team_members.user_id
			 AND team_org_member.active
			JOIN users recipient ON recipient.id = team_members.user_id
			WHERE team_repositories.repository_id = $2
			  AND team_members.active
			  AND recipient.status = 'active'
			  AND $3 IN ('public', 'internal', 'private')
			UNION
			SELECT watches.user_id
			FROM repository_watches watches
			JOIN repositories r ON r.id = watches.repository_id
			  AND r.organization_id = $1 AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			JOIN users recipient ON recipient.id = watches.user_id AND recipient.status = 'active'
			WHERE watches.repository_id = $2
		)
		SELECT DISTINCT eligible.user_id::text,
		       CASE WHEN recipient.locale = 'ja' THEN 'ja' ELSE 'en' END,
		       COALESCE(preferences.in_app_enabled, true),
		       COALESCE(preferences.email_enabled, false)
		FROM eligible
		JOIN users recipient ON recipient.id = eligible.user_id AND recipient.status = 'active'
		LEFT JOIN notification_preferences preferences ON preferences.user_id = eligible.user_id
		WHERE COALESCE(preferences.repository_enabled, true)
		  AND (
		      COALESCE(preferences.in_app_enabled, true)
		      OR COALESCE(preferences.email_enabled, false)
		  )
	`, scope.OrganizationID, scope.RepositoryID, scope.Visibility)
	if err != nil {
		return nil, fmt.Errorf("list notification recipients: %w", err)
	}
	return scanNotificationRecipients(rows)
}

func reviewRequestNotificationRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	event notificationEvent,
) ([]notificationRecipient, error) {
	eventID, valid := notificationEventID(event)
	if !valid {
		return nil, nil
	}
	rows, err := transaction.Query(ctx, `
		SELECT DISTINCT recipient.id::text,
		       CASE WHEN recipient.locale = 'ja' THEN 'ja' ELSE 'en' END,
		       COALESCE(preferences.in_app_enabled, true),
		       COALESCE(preferences.email_enabled, false)
		FROM merge_request_review_requests request
		JOIN users recipient
		  ON recipient.id = request.reviewer_user_id AND recipient.status = 'active'
		LEFT JOIN notification_preferences preferences ON preferences.user_id = recipient.id
		WHERE request.id = $1 AND request.removed_at IS NULL
		  AND (
		      COALESCE(preferences.in_app_enabled, true)
		      OR COALESCE(preferences.email_enabled, false)
		  )
		UNION
		SELECT DISTINCT recipient.id::text,
		       CASE WHEN recipient.locale = 'ja' THEN 'ja' ELSE 'en' END,
		       COALESCE(preferences.in_app_enabled, true),
		       COALESCE(preferences.email_enabled, false)
		FROM merge_request_review_requests request
		JOIN teams team ON team.id = request.reviewer_team_id AND team.active
		JOIN team_memberships membership ON membership.team_id = team.id AND membership.active
		JOIN organization_memberships organization_member
		  ON organization_member.organization_id = request.organization_id
		 AND organization_member.user_id = membership.user_id AND organization_member.active
		JOIN users recipient ON recipient.id = membership.user_id AND recipient.status = 'active'
		LEFT JOIN notification_preferences preferences ON preferences.user_id = recipient.id
		WHERE request.id = $1 AND request.removed_at IS NULL
		  AND COALESCE(preferences.team_enabled, true)
		  AND (
		      COALESCE(preferences.in_app_enabled, true)
		      OR COALESCE(preferences.email_enabled, false)
		  )
	`, eventID)
	if err != nil {
		return nil, fmt.Errorf("list review request notification recipients: %w", err)
	}
	return scanNotificationRecipients(rows)
}

func repositoryInvitationNotificationRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	event notificationEvent,
) ([]notificationRecipient, error) {
	eventID, valid := notificationEventID(event)
	if !valid {
		return nil, nil
	}
	rows, err := transaction.Query(ctx, `
		SELECT invitee.id::text,
		       CASE WHEN invitee.locale = 'ja' THEN 'ja' ELSE 'en' END,
		       COALESCE(preferences.in_app_enabled, true),
		       COALESCE(preferences.email_enabled, false)
		FROM repository_invitations invitation
		JOIN users invitee ON invitee.id = invitation.invitee_user_id AND invitee.status = 'active'
		LEFT JOIN notification_preferences preferences ON preferences.user_id = invitee.id
		WHERE invitation.id = $1
		  AND invitation.status = 'pending'
		  AND invitation.expires_at > now()
		  AND COALESCE(preferences.repository_enabled, true)
		  AND (
		      COALESCE(preferences.in_app_enabled, true)
		      OR COALESCE(preferences.email_enabled, false)
		  )
	`, eventID)
	if err != nil {
		return nil, fmt.Errorf("list repository invitation notification recipient: %w", err)
	}
	return scanNotificationRecipients(rows)
}

func scanNotificationRecipients(rows pgx.Rows) ([]notificationRecipient, error) {
	defer rows.Close()
	recipients := make([]notificationRecipient, 0)
	for rows.Next() {
		var recipient notificationRecipient
		if err := rows.Scan(
			&recipient.ID,
			&recipient.Locale,
			&recipient.InAppEnabled,
			&recipient.EmailEnabled,
		); err != nil {
			return nil, fmt.Errorf("scan notification recipient: %w", err)
		}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification recipients: %w", err)
	}
	return recipients, nil
}

type notificationText struct {
	title string
	body  string
	href  string
}

func notificationMessage(event notificationEvent, scope notificationScope, locale string) notificationText {
	var fields map[string]any
	_ = json.Unmarshal(event.Payload, &fields)
	title := scope.Title
	titleKey := "titleEn"
	bodyKey := "bodyEn"
	if locale == "ja" {
		titleKey = "titleJa"
		bodyKey = "bodyJa"
	}
	if value, ok := fields[titleKey].(string); ok && strings.TrimSpace(value) != "" {
		title = value
	} else if value, ok := fields["title"].(string); ok && strings.TrimSpace(value) != "" {
		title = value
	}
	if strings.TrimSpace(title) == "" {
		title = strings.ReplaceAll(event.Topic, ".", " ")
	}
	body, _ := fields[bodyKey].(string)
	if strings.TrimSpace(body) == "" {
		body, _ = fields["body"].(string)
	}
	if comment, ok := fields["comment"].(map[string]any); ok {
		if commentBody, ok := comment["body"].(string); ok {
			body = commentBody
		}
	}
	if len([]rune(body)) > 500 {
		body = string([]rune(body)[:500])
	}
	return notificationText{title: title, body: body, href: scope.Href}
}

func notificationHref(scope notificationScope) string {
	if scope.RepositorySlug != "" && scope.Revision != "" {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug +
			"/commit?revision=" + url.QueryEscape(scope.Revision)
	}
	if scope.RepositorySlug != "" && scope.DiscussionNumber != nil {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug +
			"/discussions/" + fmt.Sprint(*scope.DiscussionNumber)
	}
	if scope.RepositorySlug != "" && scope.IssueNumber != nil {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug +
			"/issues/" + fmt.Sprint(*scope.IssueNumber)
	}
	if scope.RepositorySlug != "" && scope.MergeRequestNumber != nil {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug +
			"/pulls/" + fmt.Sprint(*scope.MergeRequestNumber)
	}
	if scope.RepositorySlug != "" {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug
	}
	if scope.OrganizationSlug != "" {
		return "/organizations/" + scope.OrganizationSlug
	}
	return "/notifications"
}
