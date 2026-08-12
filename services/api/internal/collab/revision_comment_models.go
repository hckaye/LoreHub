package collab

import (
	"context"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type RevisionCommentAuthor struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

type RevisionComment struct {
	ID              string                `json:"id"`
	Revision        string                `json:"revision"`
	Author          RevisionCommentAuthor `json:"author"`
	Body            string                `json:"body"`
	CreatedAt       time.Time             `json:"createdAt"`
	EditedAt        *time.Time            `json:"editedAt"`
	ViewerCanUpdate bool                  `json:"viewerCanUpdate"`
}

type RevisionCommentPage struct {
	Items      []RevisionComment `json:"items"`
	Page       int               `json:"page"`
	PerPage    int               `json:"perPage"`
	TotalCount int64             `json:"totalCount"`
	HasNext    bool              `json:"hasNext"`
}

type RevisionCommentStore interface {
	ListRevisionComments(
		context.Context, *platform.User, Repository, string, int, int,
	) (RevisionCommentPage, error)
	CreateRevisionComment(
		context.Context, platform.User, Repository, string, string,
	) (RevisionComment, error)
	UpdateRevisionComment(
		context.Context, platform.User, Repository, string, string, string,
	) (RevisionComment, error)
	DeleteRevisionComment(
		context.Context, platform.User, Repository, string, string,
	) error
}
