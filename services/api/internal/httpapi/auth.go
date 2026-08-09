package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type Option func(*API)

type AuthOptions struct {
	LoginProvider  auth.LoginProvider
	LoginStore     auth.LoginTransactionStore
	SessionStore   auth.SessionStore
	CleanupStore   auth.CleanupStore
	Secrets        *auth.SecretCodec
	PublicOrigin   string
	SessionTTL     time.Duration
	TransactionTTL time.Duration
	SessionCookie  SessionCookieOptions
}

type SessionCookieOptions struct {
	Name             string
	LoginBindingName string
	Path             string
	Domain           string
	Secure           bool
}

type sessionCookieConfig struct {
	name        string
	bindingName string
	path        string
	domain      string
	secure      bool
}

const loginBindingCookiePath = "/auth"

func WithAuthentication(options AuthOptions) Option {
	return func(api *API) {
		api.loginProvider = options.LoginProvider
		api.loginStore = options.LoginStore
		api.sessionStore = options.SessionStore
		api.cleanupStore = options.CleanupStore
		api.secrets = options.Secrets
		api.publicOrigin = strings.TrimRight(options.PublicOrigin, "/")
		api.sessionTTL = options.SessionTTL
		if api.sessionTTL <= 0 {
			api.sessionTTL = 30 * 24 * time.Hour
		}
		api.transactionTTL = options.TransactionTTL
		if api.transactionTTL <= 0 {
			api.transactionTTL = 10 * time.Minute
		}
		api.cookie = sessionCookieConfig{
			name:        options.SessionCookie.Name,
			bindingName: options.SessionCookie.LoginBindingName,
			path:        options.SessionCookie.Path,
			domain:      options.SessionCookie.Domain,
			secure:      options.SessionCookie.Secure,
		}
		if api.cookie.name == "" {
			api.cookie.name = "lorehub_session"
		}
		if api.cookie.bindingName == "" {
			api.cookie.bindingName = "lorehub_login_binding"
		}
		if api.cookie.path == "" {
			api.cookie.path = "/"
		}
	}
}

func (api *API) login(writer http.ResponseWriter, request *http.Request) {
	returnTo, ok := safeRelativeReturnTo(request.URL.Query().Get("return_to"))
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_return_to", "The return location is invalid")
		return
	}
	prompt, ok := loginPrompt(request.URL.Query())
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_auth_request", "The login request is invalid")
		return
	}
	if !api.interactiveAuthenticationAvailable() {
		writeProblem(writer, http.StatusServiceUnavailable, "authentication_unavailable",
			"Interactive authentication is not configured")
		return
	}
	now := time.Now().UTC()
	api.cleanupAuthentication(request.Context(), now)
	state, err := api.secrets.NewState()
	if err != nil {
		api.internalError(writer, request, "generate login transaction", err)
		return
	}
	codeVerifier := api.secrets.CodeVerifier(state)
	nonce := api.secrets.Nonce(state)
	transaction := auth.LoginTransaction{
		StateDigest:        api.secrets.Digest(state),
		CodeVerifierDigest: api.secrets.Digest(codeVerifier),
		NonceDigest:        api.secrets.Digest(nonce),
		ReturnTo:           returnTo,
		CreatedAt:          now,
		ExpiresAt:          now.Add(api.transactionTTL),
	}
	if err := api.loginStore.CreateLoginTransaction(request.Context(), transaction); err != nil {
		api.internalError(writer, request, "create login transaction", err)
		return
	}
	loginURL := api.loginProvider.AuthorizationURL(
		state, api.secrets.CodeChallenge(codeVerifier), nonce, prompt,
	)
	if err := validateProviderURL(loginURL); err != nil {
		api.internalError(writer, request, "prepare OIDC redirect", err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	http.SetCookie(writer, api.newLoginBindingCookie(state, now.Add(api.transactionTTL)))
	http.Redirect(writer, request, loginURL, http.StatusFound)
}

func (api *API) callback(writer http.ResponseWriter, request *http.Request) {
	if !api.interactiveAuthenticationAvailable() {
		writeProblem(writer, http.StatusServiceUnavailable, "authentication_unavailable",
			"Interactive authentication is not configured")
		return
	}
	state := request.URL.Query().Get("state")
	if state == "" || len(state) > 512 {
		writeProblem(writer, http.StatusBadRequest, "authentication_failed", "The authentication response is invalid")
		return
	}
	bindingCookie, bindingErr := request.Cookie(api.cookie.bindingName)
	if bindingErr != nil || bindingCookie.Value == "" || len(bindingCookie.Value) != len(state) ||
		subtle.ConstantTimeCompare([]byte(bindingCookie.Value), []byte(state)) != 1 {
		writeProblem(writer, http.StatusBadRequest, "authentication_failed", "The authentication response is invalid")
		return
	}
	api.clearLoginBindingCookie(writer)
	now := time.Now().UTC()
	transaction, err := api.loginStore.ConsumeLoginTransaction(request.Context(), api.secrets.Digest(state), now)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidTransaction) {
			writeProblem(writer, http.StatusBadRequest, "authentication_failed", "The authentication response is invalid")
			return
		}
		api.internalError(writer, request, "consume login transaction", err)
		return
	}
	codeVerifier := api.secrets.CodeVerifier(state)
	nonce := api.secrets.Nonce(state)
	if !api.secrets.Matches(codeVerifier, transaction.CodeVerifierDigest) ||
		!api.secrets.Matches(nonce, transaction.NonceDigest) {
		writeProblem(writer, http.StatusBadRequest, "authentication_failed", "The authentication response is invalid")
		return
	}
	if request.URL.Query().Get("error") != "" || request.URL.Query().Get("code") == "" {
		writeProblem(writer, http.StatusBadRequest, "authentication_failed", "The authentication was not completed")
		return
	}
	principal, err := api.loginProvider.Exchange(request.Context(), request.URL.Query().Get("code"), codeVerifier, nonce)
	if err != nil {
		api.logger.Error("complete OIDC login", "error", err)
		writeProblem(writer, http.StatusUnauthorized, "authentication_failed", "The authentication could not be completed")
		return
	}
	user, err := api.store.EnsureUser(request.Context(), principal)
	if err != nil {
		api.internalError(writer, request, "provision authenticated user", err)
		return
	}
	oldCookie, oldCookieErr := request.Cookie(api.cookie.name)
	if oldCookieErr == nil && oldCookie.Value != "" {
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
	_, err = api.sessionStore.CreateSession(
		request.Context(),
		user.ID,
		api.secrets.Digest(sessionToken),
		api.secrets.Digest(api.secrets.CSRFToken(sessionToken)),
		expiresAt,
	)
	if err != nil {
		api.internalError(writer, request, "create authentication session", err)
		return
	}
	returnTo, ok := safeRelativeReturnTo(transaction.ReturnTo)
	if !ok {
		returnTo = "/"
	}
	writer.Header().Set("Cache-Control", "no-store")
	http.SetCookie(writer, api.newSessionCookie(sessionToken, expiresAt))
	http.Redirect(writer, request, returnTo, http.StatusSeeOther)
}

func (api *API) logout(writer http.ResponseWriter, request *http.Request) {
	returnTo, ok := safeRelativeReturnTo(request.URL.Query().Get("return_to"))
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_return_to", "The return location is invalid")
		return
	}
	if api.sessionStore == nil || api.secrets == nil {
		api.clearSessionCookie(writer)
		http.Redirect(writer, request, returnTo, http.StatusSeeOther)
		return
	}
	session, sessionToken, found, err := api.lookupSession(request)
	if err != nil {
		api.internalError(writer, request, "look up authentication session", err)
		return
	}
	if !found {
		api.clearSessionCookie(writer)
		http.Redirect(writer, request, returnTo, http.StatusSeeOther)
		return
	}
	if !api.validCSRF(request, session.CSRFDigest) {
		writeProblem(writer, http.StatusForbidden, "csrf_failed", "A valid CSRF token is required")
		return
	}
	if err := api.sessionStore.RevokeSession(
		request.Context(), api.secrets.Digest(sessionToken), time.Now().UTC(),
	); err != nil {
		api.internalError(writer, request, "revoke authentication session", err)
		return
	}
	api.clearSessionCookie(writer)
	writer.Header().Set("Cache-Control", "no-store")
	http.Redirect(writer, request, returnTo, http.StatusSeeOther)
}

func (api *API) session(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	session, sessionToken, found, err := api.lookupSession(request)
	if err != nil {
		api.internalError(writer, request, "look up authentication session", err)
		return
	}
	if !found {
		if sessionToken != "" {
			api.clearSessionCookie(writer)
		}
		writeJSON(writer, http.StatusOK, anonymousSessionResponse())
		return
	}
	writeJSON(writer, http.StatusOK, authenticatedSessionResponse(session, api.secrets.CSRFToken(sessionToken)))
}

func (api *API) lookupSession(request *http.Request) (auth.Session, string, bool, error) {
	if api.sessionStore == nil || api.secrets == nil {
		return auth.Session{}, "", false, nil
	}
	cookie, err := request.Cookie(api.cookie.name)
	if errors.Is(err, http.ErrNoCookie) || cookie == nil || cookie.Value == "" {
		return auth.Session{}, "", false, nil
	}
	if err != nil {
		return auth.Session{}, "", false, nil
	}
	if len(cookie.Value) > 512 {
		return auth.Session{}, cookie.Value, false, nil
	}
	session, err := api.sessionStore.LookupSession(request.Context(), api.secrets.Digest(cookie.Value), time.Now().UTC())
	if errors.Is(err, auth.ErrInvalidSession) {
		return auth.Session{}, cookie.Value, false, nil
	}
	if err != nil {
		return auth.Session{}, cookie.Value, false, err
	}
	return session, cookie.Value, true, nil
}

func (api *API) validCSRF(request *http.Request, expectedDigest []byte) bool {
	csrfToken := request.Header.Get("X-CSRF-Token")
	if csrfToken == "" || len(csrfToken) > 512 || !api.secrets.Matches(csrfToken, expectedDigest) {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	expected, err := url.Parse(api.publicOrigin)
	if err != nil || expected.Scheme == "" || expected.Host == "" {
		return false
	}
	actual, err := url.Parse(origin)
	if err != nil || actual.Scheme == "" || actual.Host == "" || actual.Path != "" || actual.RawQuery != "" ||
		actual.Fragment != "" {
		return false
	}
	return strings.EqualFold(actual.Scheme, expected.Scheme) && strings.EqualFold(actual.Host, expected.Host)
}

func (api *API) interactiveAuthenticationAvailable() bool {
	return api.loginProvider != nil && api.loginStore != nil && api.sessionStore != nil && api.secrets != nil
}

func (api *API) cleanupAuthentication(ctx context.Context, now time.Time) {
	if api.cleanupStore == nil {
		return
	}
	if err := api.cleanupStore.CleanupExpiredAuthentication(ctx, now); err != nil {
		api.logger.Warn("clean expired authentication data", "error", err)
	}
}

func (api *API) newSessionCookie(token string, expiresAt time.Time) *http.Cookie {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	return &http.Cookie{
		Name:     api.cookie.name,
		Value:    token,
		Path:     api.cookie.path,
		Domain:   api.cookie.domain,
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   api.cookie.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (api *API) clearSessionCookie(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{
		Name:     api.cookie.name,
		Value:    "",
		Path:     api.cookie.path,
		Domain:   api.cookie.domain,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   api.cookie.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (api *API) newLoginBindingCookie(state string, expiresAt time.Time) *http.Cookie {
	maxAge := int(api.transactionTTL / time.Second)
	if maxAge < 1 {
		maxAge = 1
	}
	return &http.Cookie{
		Name:     api.cookie.bindingName,
		Value:    state,
		Path:     loginBindingCookiePath,
		Domain:   api.cookie.domain,
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   api.cookie.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (api *API) clearLoginBindingCookie(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{
		Name:     api.cookie.bindingName,
		Value:    "",
		Path:     loginBindingCookiePath,
		Domain:   api.cookie.domain,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   api.cookie.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

type authSessionResponse struct {
	Authenticated bool                 `json:"authenticated"`
	User          *authUserResponse    `json:"user"`
	Session       *authSessionMetadata `json:"session"`
	CSRFToken     string               `json:"csrfToken"`
}

type authUserResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Locale      string `json:"locale"`
}

type authSessionMetadata struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

func anonymousSessionResponse() authSessionResponse {
	return authSessionResponse{Authenticated: false, User: nil, Session: nil, CSRFToken: ""}
}

func authenticatedSessionResponse(session auth.Session, csrfToken string) authSessionResponse {
	return authSessionResponse{
		Authenticated: true,
		User: &authUserResponse{
			ID:          session.UserID,
			Username:    session.Username,
			DisplayName: session.DisplayName,
			Email:       session.Email,
			Locale:      session.Locale,
		},
		Session: &authSessionMetadata{
			ID:         session.ID,
			CreatedAt:  session.CreatedAt,
			ExpiresAt:  session.ExpiresAt,
			LastSeenAt: session.LastSeenAt,
		},
		CSRFToken: csrfToken,
	}
}

func userFromSession(session auth.Session) platform.User {
	return platform.User{
		ID:          session.UserID,
		Username:    session.Username,
		DisplayName: session.DisplayName,
		Email:       session.Email,
		Locale:      session.Locale,
	}
}

func safeRelativeReturnTo(value string) (string, bool) {
	if value == "" {
		return "/", true
	}
	if len(value) > 2048 || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\r\n") {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil ||
		!strings.HasPrefix(parsed.Path, "/") {
		return "", false
	}
	decodedPath, err := url.PathUnescape(parsed.Path)
	if err != nil || strings.HasPrefix(decodedPath, "//") || strings.Contains(decodedPath, "\\") {
		return "", false
	}
	return value, true
}

func validateProviderURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("OIDC provider returned an invalid authorization URL")
	}
	return nil
}

func loginPrompt(query url.Values) (string, bool) {
	promptValues, promptPresent := query["prompt"]
	if promptPresent && (len(promptValues) != 1 || promptValues[0] != auth.RegistrationPrompt) {
		return "", false
	}
	kcActionValues, kcActionPresent := query["kc_action"]
	if kcActionPresent && (len(kcActionValues) != 1 || kcActionValues[0] != "register") {
		return "", false
	}
	if promptPresent || kcActionPresent {
		return auth.RegistrationPrompt, true
	}
	return "", true
}

func stateChangingMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
