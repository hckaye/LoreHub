// Package discussions implements repository conversations backed by PostgreSQL.
package discussions

import (
	"context"
	"errors"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var ErrInvalidInput = errors.New("invalid discussion input")

type RepositoryRef struct {
	ID             string
	OrganizationID string
}

type Category struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Format          string    `json:"format"`
	DiscussionCount int64     `json:"discussionCount"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Author struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

type Summary struct {
	ID                string    `json:"id"`
	Number            int64     `json:"number"`
	Title             string    `json:"title"`
	State             string    `json:"state"`
	Locked            bool      `json:"locked"`
	Pinned            bool      `json:"pinned"`
	Answered          bool      `json:"answered"`
	Category          Category  `json:"category"`
	Author            Author    `json:"author"`
	CommentCount      int64     `json:"commentCount"`
	VoteCount         int64     `json:"voteCount"`
	ViewerHasVoted    bool      `json:"viewerHasVoted"`
	ViewerCanVote     bool      `json:"viewerCanVote"`
	ViewerCanEdit     bool      `json:"viewerCanEdit"`
	ViewerCanModerate bool      `json:"viewerCanModerate"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type Comment struct {
	ID                  string     `json:"id"`
	ParentID            *string    `json:"parentId"`
	Author              Author     `json:"author"`
	Body                string     `json:"body"`
	Answer              bool       `json:"answer"`
	ViewerCanEdit       bool       `json:"viewerCanEdit"`
	ViewerCanDelete     bool       `json:"viewerCanDelete"`
	ViewerCanMarkAnswer bool       `json:"viewerCanMarkAnswer"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	EditedAt            *time.Time `json:"editedAt"`
}

type Discussion struct {
	Summary
	Body             string    `json:"body"`
	Comments         []Comment `json:"comments"`
	CommentPage      int       `json:"commentPage"`
	CommentsPerPage  int       `json:"commentsPerPage"`
	TotalComments    int64     `json:"totalComments"`
	ViewerCanComment bool      `json:"viewerCanComment"`
}

type Page struct {
	Discussions     []Summary `json:"discussions"`
	TotalCount      int64     `json:"totalCount"`
	Page            int       `json:"page"`
	PerPage         int       `json:"perPage"`
	ViewerCanCreate bool      `json:"viewerCanCreate"`
}

type ListFilter struct {
	Category string
	State    string
	Query    string
	Sort     string
	Page     int
	PerPage  int
}

type CreateInput struct {
	CategorySlug string
	Title        string
	Body         string
}

type UpdateInput struct {
	CategorySlug *string
	Title        *string
	Body         *string
	State        *string
	Locked       *bool
	Pinned       *bool
}

type CategoryInput struct {
	Slug        string
	Name        string
	Description string
	Format      string
}

type Store interface {
	ListCategories(context.Context, string) ([]Category, error)
	CreateCategory(context.Context, platform.User, RepositoryRef, CategoryInput) (Category, error)
	UpdateCategory(context.Context, platform.User, RepositoryRef, string, CategoryInput) (Category, error)
	DeleteCategory(context.Context, platform.User, RepositoryRef, string) error
	List(context.Context, string, string, ListFilter) (Page, error)
	Get(context.Context, string, int64, string, int, int) (Discussion, error)
	Create(context.Context, platform.User, RepositoryRef, CreateInput) (Discussion, error)
	Update(context.Context, platform.User, RepositoryRef, int64, UpdateInput) (Discussion, error)
	Delete(context.Context, platform.User, RepositoryRef, int64) error
	CreateComment(context.Context, platform.User, RepositoryRef, int64, *string, string) (Comment, error)
	UpdateComment(context.Context, platform.User, RepositoryRef, int64, string, string) (Comment, error)
	DeleteComment(context.Context, platform.User, RepositoryRef, int64, string) error
	SetAnswer(context.Context, platform.User, RepositoryRef, int64, string, bool) (Discussion, error)
	SetVote(context.Context, platform.User, RepositoryRef, int64, bool) (Summary, error)
}
