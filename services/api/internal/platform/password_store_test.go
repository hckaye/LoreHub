package platform

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestTranslatePasswordConstraintError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		constraint string
		want       error
	}{
		{name: "username", constraint: "users_username_unique", want: ErrUsernameTaken},
		{name: "users email", constraint: "users_email_unique", want: ErrEmailTaken},
		{name: "passwords email", constraint: "user_passwords_email_unique", want: ErrEmailTaken},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := translatePasswordConstraintError("create password user", &pgconn.PgError{
				Code:           "23505",
				ConstraintName: test.constraint,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("constraint %q translated to %v; want %v", test.constraint, err, test.want)
			}
		})
	}
}
