package platform

import "time"

type UserProfile struct {
	ID              string    `json:"id"`
	Username        string    `json:"username"`
	DisplayName     string    `json:"displayName"`
	Email           *string   `json:"email"`
	Bio             string    `json:"bio"`
	AvatarURL       string    `json:"avatarUrl"`
	WebsiteURL      string    `json:"websiteUrl"`
	Location        string    `json:"location"`
	Company         string    `json:"company"`
	Pronouns        string    `json:"pronouns"`
	Locale          string    `json:"locale"`
	CreatedAt       time.Time `json:"createdAt"`
	RepositoryCount int64     `json:"repositoryCount"`
}

type OrganizationView struct {
	ID                          string    `json:"id"`
	Slug                        string    `json:"slug"`
	DisplayName                 string    `json:"displayName"`
	Description                 string    `json:"description"`
	Visibility                  string    `json:"visibility"`
	WebsiteURL                  string    `json:"websiteUrl"`
	ContactEmail                string    `json:"contactEmail"`
	DefaultRepositoryVisibility string    `json:"defaultRepositoryVisibility"`
	Role                        string    `json:"role"`
	MemberCount                 int64     `json:"memberCount"`
	RepositoryCount             int64     `json:"repositoryCount"`
	TeamCount                   int64     `json:"teamCount"`
	CreatedAt                   time.Time `json:"createdAt"`
}

type Team struct {
	ID               string    `json:"id"`
	OrganizationID   string    `json:"organizationId"`
	OrganizationSlug string    `json:"organizationSlug"`
	Slug             string    `json:"slug"`
	DisplayName      string    `json:"displayName"`
	Description      string    `json:"description"`
	ViewerRole       string    `json:"viewerRole"`
	MemberCount      int64     `json:"memberCount"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type TeamMember struct {
	UserID      string    `json:"userId"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joinedAt"`
}

type Dashboard struct {
	Repositories        []Repository       `json:"repositories"`
	Organizations       []OrganizationView `json:"organizations"`
	Notifications       []Notification     `json:"notifications"`
	UnreadNotifications int64              `json:"unreadNotifications"`
}

type SearchResults struct {
	Repositories  []Repository       `json:"repositories"`
	Organizations []OrganizationView `json:"organizations"`
	Users         []UserSearchResult `json:"users"`
}

type UserSearchResult struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

type Notification struct {
	ID        string     `json:"id"`
	Topic     string     `json:"topic"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Href      string     `json:"href"`
	ReadAt    *time.Time `json:"readAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

type NotificationPreferences struct {
	InAppEnabled      bool      `json:"inAppEnabled"`
	EmailEnabled      bool      `json:"emailEnabled"`
	MentionEnabled    bool      `json:"mentionEnabled"`
	TeamEnabled       bool      `json:"teamEnabled"`
	RepositoryEnabled bool      `json:"repositoryEnabled"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type UpdateNotificationPreferencesInput struct {
	InAppEnabled      *bool
	EmailEnabled      *bool
	MentionEnabled    *bool
	TeamEnabled       *bool
	RepositoryEnabled *bool
}

type UpdateProfileInput struct {
	DisplayName *string
	Bio         *string
	AvatarURL   *string
	WebsiteURL  *string
	Location    *string
	Company     *string
	Pronouns    *string
}

type UpdateOrganizationInput struct {
	DisplayName                 *string
	Description                 *string
	Visibility                  *string
	WebsiteURL                  *string
	ContactEmail                *string
	DefaultRepositoryVisibility *string
}

type CreateTeamInput struct {
	Slug        string
	DisplayName string
	Description string
}

type UpdateTeamInput struct {
	DisplayName *string
	Description *string
}

type UpdateRepositorySettingsInput struct {
	DisplayName *string
	Description *string
	Visibility  *string
	HomepageURL *string
}

type NotificationPage struct {
	Items []Notification `json:"items"`
	Total int64          `json:"total"`
}
