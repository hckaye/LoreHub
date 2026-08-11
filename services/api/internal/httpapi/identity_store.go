package httpapi

import (
	"context"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type IdentityStore interface {
	Dashboard(context.Context, platform.User) (platform.Dashboard, error)
	Search(context.Context, *platform.User, string, string, int) (platform.SearchResults, error)
	UserProfile(context.Context, *platform.User, string) (platform.UserProfile, error)
	UserRepositories(context.Context, *platform.User, string) ([]platform.Repository, error)
	UpdateProfile(context.Context, platform.User, platform.UpdateProfileInput) (platform.UserProfile, error)
	CreateTeam(context.Context, platform.User, string, platform.SetTeamInput) (platform.Team, error)
	UpdateTeam(context.Context, platform.User, string, string, platform.SetTeamInput) (platform.Team, error)
	ListNotifications(context.Context, platform.User, bool, int) (platform.NotificationPage, error)
	UnreadNotificationCount(context.Context, platform.User) (int64, error)
	MarkNotificationRead(context.Context, platform.User, string) error
	MarkAllNotificationsRead(context.Context, platform.User) error
	NotificationPreferences(context.Context, platform.User) (platform.NotificationPreferences, error)
	UpdateNotificationPreferences(
		context.Context, platform.User, platform.UpdateNotificationPreferencesInput,
	) (platform.NotificationPreferences, error)
	Organization(context.Context, *platform.User, string) (platform.OrganizationView, error)
	OrganizationRepositories(context.Context, *platform.User, string) ([]platform.Repository, error)
	OrganizationAuditLog(
		context.Context, platform.User, string, string, string, int,
	) (platform.AuditLogPage, error)
	UpdateOrganization(
		context.Context, platform.User, string, platform.UpdateOrganizationInput,
	) (platform.OrganizationView, error)
	Teams(context.Context, *platform.User, string) ([]platform.Team, error)
	Team(context.Context, *platform.User, string, string) (platform.Team, error)
	TeamMembers(context.Context, *platform.User, string, string) ([]platform.TeamMember, error)
	AddTeamMember(context.Context, platform.User, string, string, string, string) (platform.TeamMember, error)
	RemoveTeamMember(context.Context, platform.User, string, string, string) error
	UpdateRepositorySettings(context.Context, platform.User, string, string,
		platform.UpdateRepositorySettingsInput) (platform.Repository, error)
	RepositoryForSettings(context.Context, platform.User, string, string) (platform.Repository, error)
}
