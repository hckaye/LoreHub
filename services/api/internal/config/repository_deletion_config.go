package config

import (
	"errors"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

func validateRepositoryDeletionConfig(settings Config, command string) error {
	if command != "serve" {
		return nil
	}
	if err := loreclient.ValidateServiceSubject(settings.LoreRepositoryLifecycleSubject); err != nil {
		return errors.New("LOREHUB_LORE_REPOSITORY_LIFECYCLE_SUBJECT is invalid")
	}
	if settings.RepositoryDeletionRetention < time.Hour ||
		settings.RepositoryDeletionRetention > 365*24*time.Hour {
		return errors.New("LOREHUB_REPOSITORY_DELETION_RETENTION must be between one hour and one year")
	}
	if settings.RepositoryDeletionPollPeriod <= 0 || settings.RepositoryDeletionPollPeriod > time.Minute {
		return errors.New("LOREHUB_REPOSITORY_DELETION_POLL_PERIOD must be no longer than one minute")
	}
	if settings.RepositoryDeletionTimeout <= 0 || settings.RepositoryDeletionTimeout > 10*time.Minute {
		return errors.New("LOREHUB_REPOSITORY_DELETION_TIMEOUT must be no longer than ten minutes")
	}
	if settings.RepositoryDeletionLeaseDuration < settings.RepositoryDeletionTimeout+30*time.Second ||
		settings.RepositoryDeletionLeaseDuration > 15*time.Minute {
		return errors.New("LOREHUB_REPOSITORY_DELETION_LEASE_DURATION must cover timeout and completion")
	}
	return nil
}
