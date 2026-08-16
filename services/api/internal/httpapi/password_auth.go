package httpapi

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type PasswordAuthStore interface {
	CreatePasswordUser(ctx context.Context, input platform.PasswordUserInput) (platform.User, error)
	PasswordCredential(ctx context.Context, identifier string) (platform.PasswordCredential, error)
	PasswordCredentialForUser(ctx context.Context, userID string) (platform.PasswordCredential, error)
	RecordPasswordFailure(ctx context.Context, userID string) (int, error)
	LockPasswordCredential(ctx context.Context, userID string, until time.Time) error
	ClearPasswordFailures(ctx context.Context, userID string) error
	SetPassword(ctx context.Context, userID string, passwordHash string) error
	RevokeOtherSessions(ctx context.Context, userID string, keepTokenDigest []byte, now time.Time) error
	CreatePasswordReset(ctx context.Context, userID string, tokenDigest []byte, expiresAt time.Time) error
	PasswordResetUser(ctx context.Context, tokenDigest []byte, now time.Time) (string, error)
	ConsumePasswordReset(ctx context.Context, tokenDigest []byte, now time.Time) (string, error)
}

// PasswordResetSender delivers the password reset email for built-in accounts.
type PasswordResetSender interface {
	SendPasswordReset(ctx context.Context, recipient string, locale string, resetURL string) error
}

func WithPasswordAuthentication(store PasswordAuthStore, registration bool) Option {
	return func(api *API) {
		api.passwordAuth = store
		api.passwordRegistration = registration
	}
}

func WithPasswordReset(sender PasswordResetSender) Option {
	return func(api *API) {
		api.passwordResetSender = sender
	}
}

func (api *API) passwordAuthenticationAvailable() bool {
	return api.passwordAuth != nil && api.sessionStore != nil && api.secrets != nil
}

const (
	maxLoginIdentifierLength = 320
	maxEmailLength           = 320
	maxUsernameLength        = 63
)

func (api *API) passwordLogin(writer http.ResponseWriter, request *http.Request) {
	if !api.passwordAuthenticationAvailable() {
		writeProblem(writer, http.StatusServiceUnavailable, "authentication_unavailable",
			"Password authentication is not configured")
		return
	}
	if !api.allowJSONCredentialRequest(writer, request) {
		return
	}
	var input struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	identifier := strings.ToLower(strings.TrimSpace(input.Identifier))
	if identifier == "" || len(identifier) > maxLoginIdentifierLength ||
		input.Password == "" || len(input.Password) > auth.MaxPasswordLength {
		writeProblem(writer, http.StatusUnauthorized, "invalid_credentials",
			"The email address, username, or password is incorrect")
		return
	}
	now := time.Now().UTC()
	api.cleanupAuthentication(request.Context(), now)
	credential, err := api.passwordAuth.PasswordCredential(request.Context(), identifier)
	if errors.Is(err, platform.ErrNotFound) {
		auth.EqualizeVerificationTiming(input.Password)
		writeProblem(writer, http.StatusUnauthorized, "invalid_credentials",
			"The email address, username, or password is incorrect")
		return
	}
	if err != nil {
		api.internalError(writer, request, "look up password credential", err)
		return
	}
	if credential.LockedUntil != nil && credential.LockedUntil.After(now) {
		writeProblem(writer, http.StatusForbidden, "account_locked",
			"The account is temporarily locked after repeated failures")
		return
	}
	if !auth.VerifyPassword(credential.PasswordHash, input.Password) {
		failures, failureErr := api.passwordAuth.RecordPasswordFailure(request.Context(), credential.UserID)
		if failureErr == nil {
			if delay := auth.LockoutDelay(failures); delay > 0 {
				failureErr = api.passwordAuth.LockPasswordCredential(request.Context(), credential.UserID,
					now.Add(delay))
			}
		}
		if failureErr != nil && !errors.Is(failureErr, platform.ErrNotFound) {
			api.logger.Error("record password failure", "error", failureErr)
		}
		writeProblem(writer, http.StatusUnauthorized, "invalid_credentials",
			"The email address, username, or password is incorrect")
		return
	}
	if err := api.passwordAuth.ClearPasswordFailures(request.Context(), credential.UserID); err != nil {
		api.internalError(writer, request, "clear password failures", err)
		return
	}
	api.completePasswordAuthentication(writer, request, credential.UserID, now)
}

func (api *API) passwordRegister(writer http.ResponseWriter, request *http.Request) {
	if !api.passwordAuthenticationAvailable() {
		writeProblem(writer, http.StatusServiceUnavailable, "authentication_unavailable",
			"Password authentication is not configured")
		return
	}
	if !api.passwordRegistration {
		writeProblem(writer, http.StatusForbidden, "registration_disabled",
			"Self-registration is disabled on this installation")
		return
	}
	if !api.allowJSONCredentialRequest(writer, request) {
		return
	}
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Locale   string `json:"locale"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	username := strings.ToLower(strings.TrimSpace(input.Username))
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if !validPasswordUsername(username) {
		writeProblem(writer, http.StatusBadRequest, "invalid_username",
			"Usernames use 2 to 63 lowercase letters, numbers, and hyphens")
		return
	}
	if !validRegistrationEmail(email) {
		writeProblem(writer, http.StatusBadRequest, "invalid_email", "The email address is invalid")
		return
	}
	if err := auth.ValidatePassword(input.Password, username, email); err != nil {
		writeProblem(writer, http.StatusBadRequest, "weak_password",
			"Passwords need at least 12 characters with uppercase and lowercase letters, a number, and a symbol, "+
				"and cannot contain the username or email address")
		return
	}
	passwordHash, err := auth.HashPassword(input.Password)
	if err != nil {
		api.internalError(writer, request, "hash password", err)
		return
	}
	user, err := api.passwordAuth.CreatePasswordUser(request.Context(), platform.PasswordUserInput{
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		Locale:       input.Locale,
	})
	if errors.Is(err, platform.ErrUsernameTaken) {
		writeProblem(writer, http.StatusConflict, "username_taken", "The username is already taken")
		return
	}
	if errors.Is(err, platform.ErrEmailTaken) {
		writeProblem(writer, http.StatusConflict, "email_taken", "The email address is already registered")
		return
	}
	if errors.Is(err, platform.ErrInvalidInput) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "The request contains invalid values")
		return
	}
	if err != nil {
		api.internalError(writer, request, "register password user", err)
		return
	}
	api.completePasswordAuthentication(writer, request, user.ID, time.Now().UTC())
}

func (api *API) changePassword(writer http.ResponseWriter, request *http.Request) {
	if !api.passwordAuthenticationAvailable() {
		writeProblem(writer, http.StatusServiceUnavailable, "authentication_unavailable",
			"Password authentication is not configured")
		return
	}
	session, sessionToken, found, err := api.lookupSession(request)
	if err != nil {
		api.internalError(writer, request, "look up authentication session", err)
		return
	}
	if !found {
		writeProblem(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required")
		return
	}
	if !api.validCSRF(request, session.CSRFDigest) {
		writeProblem(writer, http.StatusForbidden, "csrf_failed", "A valid CSRF token is required")
		return
	}
	user, err := api.store.ActiveUser(request.Context(), session.UserID)
	if err != nil {
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
		return
	}
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	credential, err := api.passwordAuth.PasswordCredentialForUser(request.Context(), user.ID)
	if errors.Is(err, platform.ErrNotFound) {
		writeProblem(writer, http.StatusConflict, "no_password_credential",
			"This account signs in through an external identity provider")
		return
	}
	if err != nil {
		api.internalError(writer, request, "look up password credential", err)
		return
	}
	now := time.Now().UTC()
	if credential.LockedUntil != nil && credential.LockedUntil.After(now) {
		writeProblem(writer, http.StatusForbidden, "account_locked",
			"The account is temporarily locked after repeated failures")
		return
	}
	if input.CurrentPassword == "" || len(input.CurrentPassword) > auth.MaxPasswordLength ||
		!auth.VerifyPassword(credential.PasswordHash, input.CurrentPassword) {
		failures, failureErr := api.passwordAuth.RecordPasswordFailure(request.Context(), user.ID)
		if failureErr == nil {
			if delay := auth.LockoutDelay(failures); delay > 0 {
				failureErr = api.passwordAuth.LockPasswordCredential(request.Context(), user.ID, now.Add(delay))
			}
		}
		if failureErr != nil && !errors.Is(failureErr, platform.ErrNotFound) {
			api.logger.Error("record password failure", "error", failureErr)
		}
		writeProblem(writer, http.StatusUnauthorized, "invalid_credentials", "The current password is incorrect")
		return
	}
	if err := auth.ValidatePassword(input.NewPassword, user.Username, credential.Email); err != nil {
		writeProblem(writer, http.StatusBadRequest, "weak_password",
			"Passwords need at least 12 characters with uppercase and lowercase letters, a number, and a symbol, "+
				"and cannot contain the username or email address")
		return
	}
	passwordHash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		api.internalError(writer, request, "hash password", err)
		return
	}
	if err := api.passwordAuth.SetPassword(request.Context(), user.ID, passwordHash); err != nil {
		api.internalError(writer, request, "update password", err)
		return
	}
	if err := api.passwordAuth.RevokeOtherSessions(
		request.Context(), user.ID, api.secrets.Digest(sessionToken), now,
	); err != nil {
		api.internalError(writer, request, "revoke other sessions", err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, map[string]bool{"updated": true})
}

const passwordResetTTL = time.Hour

func (api *API) passwordResetAvailable() bool {
	return api.passwordAuthenticationAvailable() && api.passwordResetSender != nil
}

func (api *API) passwordResetRequest(writer http.ResponseWriter, request *http.Request) {
	if !api.passwordResetAvailable() {
		writeProblem(writer, http.StatusServiceUnavailable, "reset_unavailable",
			"Password reset email is not configured on this installation")
		return
	}
	if !api.allowJSONCredentialRequest(writer, request) {
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	writer.Header().Set("Cache-Control", "no-store")
	if !validRegistrationEmail(email) {
		writeJSON(writer, http.StatusOK, map[string]bool{"requested": true})
		return
	}
	now := time.Now().UTC()
	api.cleanupAuthentication(request.Context(), now)
	credential, err := api.passwordAuth.PasswordCredential(request.Context(), email)
	if errors.Is(err, platform.ErrNotFound) || err == nil && credential.Email != email {
		writeJSON(writer, http.StatusOK, map[string]bool{"requested": true})
		return
	}
	if err != nil {
		api.internalError(writer, request, "look up password credential", err)
		return
	}
	user, err := api.store.ActiveUser(request.Context(), credential.UserID)
	if err != nil {
		writeJSON(writer, http.StatusOK, map[string]bool{"requested": true})
		return
	}
	token, err := api.secrets.NewSessionToken()
	if err != nil {
		api.internalError(writer, request, "generate password reset token", err)
		return
	}
	if err := api.passwordAuth.CreatePasswordReset(
		request.Context(), user.ID, api.secrets.Digest(token), now.Add(passwordResetTTL),
	); err != nil {
		api.internalError(writer, request, "create password reset", err)
		return
	}
	locale := user.Locale
	if locale != "ja" {
		locale = "en"
	}
	resetURL := api.publicOrigin + "/" + locale + "/auth/reset?token=" + url.QueryEscape(token)
	sender := api.passwordResetSender
	logger := api.logger
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), 30*time.Second)
		defer cancel()
		if err := sender.SendPasswordReset(ctx, credential.Email, locale, resetURL); err != nil {
			logger.Error("send password reset email", "error", err)
		}
	}()
	writeJSON(writer, http.StatusOK, map[string]bool{"requested": true})
}

func (api *API) passwordReset(writer http.ResponseWriter, request *http.Request) {
	if !api.passwordAuthenticationAvailable() {
		writeProblem(writer, http.StatusServiceUnavailable, "authentication_unavailable",
			"Password authentication is not configured")
		return
	}
	if !api.allowJSONCredentialRequest(writer, request) {
		return
	}
	var input struct {
		Token       string `json:"token"`
		NewPassword string `json:"newPassword"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	token := strings.TrimSpace(input.Token)
	if token == "" || len(token) > 512 {
		writeProblem(writer, http.StatusBadRequest, "invalid_reset_token",
			"The password reset link is invalid or has expired")
		return
	}
	now := time.Now().UTC()
	tokenDigest := api.secrets.Digest(token)
	userID, err := api.passwordAuth.PasswordResetUser(request.Context(), tokenDigest, now)
	if errors.Is(err, platform.ErrNotFound) {
		writeProblem(writer, http.StatusBadRequest, "invalid_reset_token",
			"The password reset link is invalid or has expired")
		return
	}
	if err != nil {
		api.internalError(writer, request, "look up password reset", err)
		return
	}
	user, err := api.store.ActiveUser(request.Context(), userID)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_reset_token",
			"The password reset link is invalid or has expired")
		return
	}
	credential, err := api.passwordAuth.PasswordCredentialForUser(request.Context(), userID)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_reset_token",
			"The password reset link is invalid or has expired")
		return
	}
	if err := auth.ValidatePassword(input.NewPassword, user.Username, credential.Email); err != nil {
		writeProblem(writer, http.StatusBadRequest, "weak_password",
			"Passwords need at least 12 characters with uppercase and lowercase letters, a number, and a symbol, "+
				"and cannot contain the username or email address")
		return
	}
	if _, err := api.passwordAuth.ConsumePasswordReset(request.Context(), tokenDigest, now); err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_reset_token",
			"The password reset link is invalid or has expired")
		return
	}
	passwordHash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		api.internalError(writer, request, "hash password", err)
		return
	}
	if err := api.passwordAuth.SetPassword(request.Context(), userID, passwordHash); err != nil {
		api.internalError(writer, request, "update password", err)
		return
	}
	if err := api.passwordAuth.RevokeOtherSessions(request.Context(), userID, nil, now); err != nil {
		api.internalError(writer, request, "revoke sessions after reset", err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, map[string]bool{"reset": true})
}

// completePasswordAuthentication rotates any existing session and issues a new
// one for the given user, mirroring the OIDC callback behavior.
func (api *API) completePasswordAuthentication(
	writer http.ResponseWriter,
	request *http.Request,
	userID string,
	now time.Time,
) {
	if _, err := api.store.ActiveUser(request.Context(), userID); err != nil {
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
		return
	}
	oldCookie, oldCookieErr := request.Cookie(api.cookie.name)
	if oldCookieErr == nil && oldCookie.Value != "" && len(oldCookie.Value) <= 512 {
		if err := api.sessionStore.RevokeSession(request.Context(), api.secrets.Digest(oldCookie.Value), now); err != nil {
			api.internalError(writer, request, "rotate authentication session", err)
			return
		}
	}
	sessionToken, err := api.secrets.NewSessionToken()
	if err != nil {
		api.internalError(writer, request, "generate authentication session", err)
		return
	}
	expiresAt := now.Add(api.sessionTTL)
	if _, err := api.sessionStore.CreateSession(
		request.Context(),
		userID,
		api.secrets.Digest(sessionToken),
		api.secrets.Digest(api.secrets.CSRFToken(sessionToken)),
		expiresAt,
	); err != nil {
		api.internalError(writer, request, "create authentication session", err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	http.SetCookie(writer, api.newSessionCookie(sessionToken, expiresAt))
	writeJSON(writer, http.StatusOK, map[string]bool{"authenticated": true})
}

// allowJSONCredentialRequest rejects requests a cross-site form could produce:
// the body must be declared JSON and any Origin must match the public origin.
func (api *API) allowJSONCredentialRequest(writer http.ResponseWriter, request *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeProblem(writer, http.StatusUnsupportedMediaType, "invalid_content_type",
			"The request must use application/json")
		return false
	}
	if !api.requestOriginAllowed(request) {
		writeProblem(writer, http.StatusForbidden, "invalid_origin", "The request origin is not allowed")
		return false
	}
	return true
}

func validPasswordUsername(username string) bool {
	if len(username) < 2 || len(username) > maxUsernameLength {
		return false
	}
	if username[0] == '-' || username[len(username)-1] == '-' {
		return false
	}
	for index := 0; index < len(username); index++ {
		character := username[index]
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validRegistrationEmail(email string) bool {
	if email == "" || len(email) > maxEmailLength || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email
}
