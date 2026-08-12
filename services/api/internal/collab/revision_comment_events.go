package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func recordRevisionCommentEvent(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository Repository,
	operation string,
	comment RevisionComment,
) error {
	action := "revision_comment." + operation
	if err := insertAudit(
		ctx, tx, actorID, repository.OrganizationID, repository.ID,
		action, "revision_comment", comment.ID,
	); err != nil {
		return err
	}
	eventComment := comment
	eventComment.ViewerCanUpdate = false
	payload := struct {
		Comment        RevisionComment `json:"comment"`
		OrganizationID string          `json:"organizationId"`
		RepositoryID   string          `json:"repositoryId"`
	}{
		Comment: eventComment, OrganizationID: repository.OrganizationID,
		RepositoryID: repository.ID,
	}
	topic := action + "d"
	eventKey := comment.ID
	if operation != "create" {
		eventKey += ":" + uuidArg()
	}
	if err := insertOutbox(ctx, tx, topic, eventKey, payload); err != nil {
		return fmt.Errorf("record revision comment outbox event: %w", err)
	}
	return nil
}

func translateRevisionCommentError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "22001", "22021", "23514":
			return fmt.Errorf("%s: %w", operation, platform.ErrInvalidInput)
		case "23503":
			return fmt.Errorf("%s: %w", operation, platform.ErrNotFound)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
