package api

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/httpx"
	sbmail "github.com/shellhaki/sparkbase/server/internal/mail"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

const (
	resetTTL        = time.Hour
	maxLoginFails   = 10
	loginLockPeriod = 15 * time.Minute
)

// NormalizeEmail validates and lowercases an address.
func NormalizeEmail(s string) (string, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) > 254 {
		return "", false
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") && !strings.HasSuffix(s, "@localhost") {
		return "", false
	}
	return s, true
}

var errWeakPassword = httpx.Invalid("password must be 8 to 256 characters")

// IssueSetupToken creates a fresh one-time setup token while the install has
// no users. It returns "" once setup is closed.
func (s *Server) IssueSetupToken(ctx context.Context) (string, error) {
	n, err := s.Store.CountUsers(ctx)
	if err != nil || n > 0 {
		return "", err
	}
	if err := s.Store.DeleteAuthTokens(ctx, "setup"); err != nil {
		return "", err
	}
	tok := sbcrypto.Token()
	_, err = s.Store.CreateAuthToken(ctx, store.NewAuthToken{Kind: "setup", TokenHash: sbcrypto.SHA256Hex(tok), ExpiresAt: time.Now().Add(7 * 24 * time.Hour)})
	return tok, err
}

func trimName(name *string) *string {
	if name == nil {
		return nil
	}
	n := strings.TrimSpace(*name)
	if n == "" {
		return nil
	}
	if len(n) > 100 {
		n = n[:100]
	}
	return &n
}

// CreatePasswordResetLink makes a one-time reset link for a user.
func (s *Server) CreatePasswordResetLink(ctx context.Context, u *store.User) (string, error) {
	raw := sbcrypto.Token()
	if _, err := s.Store.CreateAuthToken(ctx, store.NewAuthToken{
		Kind: "password_reset", UserID: &u.ID, Email: &u.Email,
		TokenHash: sbcrypto.SHA256Hex(raw), ExpiresAt: time.Now().Add(resetTTL),
	}); err != nil {
		return "", err
	}
	return s.Cfg.DashboardURL + "/reset-password?token=" + raw, nil
}

func (s *Server) accountRoutes() {
	s.handle("GET /v1/setup", func(w http.ResponseWriter, r *http.Request) error {
		n, err := s.Store.CountUsers(r.Context())
		if err != nil {
			return err
		}
		st, err := s.Settings(r.Context())
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"open": n == 0, "signupMode": st.SignupMode, "smtpConfigured": s.Mail.Configured()})
	})

	s.handle("POST /v1/setup", func(w http.ResponseWriter, r *http.Request) error {
		if err := s.ipLimit(w, r, "setup", 10, 10*time.Minute); err != nil {
			return err
		}
		var in struct {
			Token    string  `json:"token"`
			Email    string  `json:"email"`
			Password string  `json:"password"`
			Name     *string `json:"name"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		email, ok := NormalizeEmail(in.Email)
		if !ok {
			return httpx.Invalid("enter a valid email address")
		}
		if !sbcrypto.ValidPassword(in.Password) {
			return errWeakPassword
		}
		hash := sbcrypto.HashPassword(in.Password)
		var u *store.User
		err := s.Store.Tx(ctx, func(tx *store.Store) error {
			if err := tx.XactLock(ctx, 727275); err != nil {
				return err
			}
			n, err := tx.CountUsers(ctx)
			if err != nil {
				return err
			}
			if n > 0 {
				return httpx.E(409, "setup_closed", "setup was already completed; sign in instead")
			}
			if _, err := tx.ConsumeAuthToken(ctx, "setup", sbcrypto.SHA256Hex(strings.TrimSpace(in.Token))); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return httpx.Unauthorized("the setup token is wrong; find it in the server logs")
				}
				return err
			}
			u, err = tx.CreateUser(ctx, email, hash, trimName(in.Name), "admin")
			return err
		})
		if err != nil {
			return err
		}
		s.Audit(ctx, u.ID, "", "setup.complete", nil)
		if err := s.startSession(ctx, w, u); err != nil {
			return err
		}
		return httpx.WriteJSON(w, 201, map[string]any{"user": u})
	})

	s.handle("POST /v1/auth/login", func(w http.ResponseWriter, r *http.Request) error {
		if err := s.ipLimit(w, r, "login", 30, 10*time.Minute); err != nil {
			return err
		}
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		wrong := httpx.Unauthorized("wrong email or password")
		email, ok := NormalizeEmail(in.Email)
		if !ok {
			sbcrypto.DummyVerify(in.Password)
			return wrong
		}
		u, err := s.Store.GetUserByEmail(ctx, email)
		if errors.Is(err, store.ErrNotFound) {
			sbcrypto.DummyVerify(in.Password)
			return wrong
		}
		if err != nil {
			return err
		}
		if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
			sbcrypto.DummyVerify(in.Password)
			return httpx.E(429, "account_locked", "too many wrong passwords; try again in a few minutes or reset your password")
		}
		if !sbcrypto.VerifyPassword(in.Password, u.PasswordHash) {
			_ = s.Store.RecordFailedLogin(ctx, u.ID, maxLoginFails, loginLockPeriod)
			return wrong
		}
		if u.DisabledAt != nil {
			return httpx.Unauthorized("this account is disabled")
		}
		if err := s.startSession(ctx, w, u); err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"user": u})
	})

	s.handle("POST /v1/auth/signup", func(w http.ResponseWriter, r *http.Request) error {
		if err := s.ipLimit(w, r, "signup", 10, 10*time.Minute); err != nil {
			return err
		}
		var in struct {
			Email    string  `json:"email"`
			Password string  `json:"password"`
			Name     *string `json:"name"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		st, err := s.Settings(ctx)
		if err != nil {
			return err
		}
		if st.SignupMode != "open" {
			return httpx.Forbidden("sign-ups are invite only on this install; ask an admin for an invite")
		}
		email, ok := NormalizeEmail(in.Email)
		if !ok {
			return httpx.Invalid("enter a valid email address")
		}
		if !sbcrypto.ValidPassword(in.Password) {
			return errWeakPassword
		}
		u, err := s.Store.CreateUser(ctx, email, sbcrypto.HashPassword(in.Password), trimName(in.Name), "member")
		if errors.Is(err, store.ErrConflict) {
			return httpx.Conflict("an account with this email exists; sign in instead")
		}
		if err != nil {
			return err
		}
		s.Audit(ctx, u.ID, "", "user.signup", nil)
		if err := s.startSession(ctx, w, u); err != nil {
			return err
		}
		return httpx.WriteJSON(w, 201, map[string]any{"user": u})
	})

	s.handle("POST /v1/auth/password/reset", func(w http.ResponseWriter, r *http.Request) error {
		if err := s.ipLimit(w, r, "reset", 10, 10*time.Minute); err != nil {
			return err
		}
		var in struct {
			Email string `json:"email"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		// Same answer whether or not the account exists.
		sent := map[string]any{"sent": true, "emailEnabled": s.Mail.Configured()}
		email, ok := NormalizeEmail(in.Email)
		if !ok {
			return httpx.Invalid("enter a valid email address")
		}
		if res, err := s.Limiter.Allow(ctx, "reset:email:"+sbcrypto.SHA256Hex(email), 3, 15*time.Minute); err == nil && !res.Allowed {
			return httpx.OK(w, sent)
		}
		u, err := s.Store.GetUserByEmail(ctx, email)
		if err != nil || u.DisabledAt != nil {
			return httpx.OK(w, sent)
		}
		link, err := s.CreatePasswordResetLink(ctx, u)
		if err != nil {
			return err
		}
		if err := s.Mail.Send(ctx, sbmail.Message{
			To: u.Email, Subject: "Reset your Sparkbase password", Link: link,
			Text: "Open this link to choose a new password:\n\n" + link + "\n\nIt works once and expires in one hour. If you did not ask for it, ignore this email.\n",
		}); err != nil {
			s.Log.Warn("reset email failed", "err", err.Error())
		}
		return httpx.OK(w, sent)
	})

	s.handle("POST /v1/auth/password/reset/confirm", func(w http.ResponseWriter, r *http.Request) error {
		if err := s.ipLimit(w, r, "reset", 20, 10*time.Minute); err != nil {
			return err
		}
		var in struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if !sbcrypto.ValidPassword(in.Password) {
			return errWeakPassword
		}
		ctx := r.Context()
		tok, err := s.Store.ConsumeAuthToken(ctx, "password_reset", sbcrypto.SHA256Hex(strings.TrimSpace(in.Token)))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.Unauthorized("this reset link is invalid, used or expired; request a new one")
		}
		if err != nil {
			return err
		}
		u, err := s.Store.GetUser(ctx, *tok.UserID)
		if err != nil {
			return err
		}
		if u.DisabledAt != nil {
			return httpx.Unauthorized("this account is disabled")
		}
		if err := s.Store.SetUserPassword(ctx, u.ID, sbcrypto.HashPassword(in.Password)); err != nil {
			return err
		}
		// A reset signs out every other session.
		_ = s.Store.DeleteUserSessions(ctx, u.ID)
		_ = s.Store.ConsumeUserTokens(ctx, u.ID, "password_reset")
		s.Audit(ctx, u.ID, "", "user.password_reset", nil)
		if err := s.startSession(ctx, w, u); err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"user": u})
	})

	s.handle("GET /v1/auth/invites/{token}", func(w http.ResponseWriter, r *http.Request) error {
		if err := s.ipLimit(w, r, "invite", 30, 10*time.Minute); err != nil {
			return err
		}
		inv, err := s.Store.ValidAuthToken(r.Context(), "invite", sbcrypto.SHA256Hex(r.PathValue("token")))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("invite")
		}
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"email": inv.Email, "role": inv.Role, "expiresAt": inv.ExpiresAt})
	})

	s.handle("POST /v1/auth/invites/accept", func(w http.ResponseWriter, r *http.Request) error {
		if err := s.ipLimit(w, r, "invite", 30, 10*time.Minute); err != nil {
			return err
		}
		var in struct {
			Token    string  `json:"token"`
			Password string  `json:"password"`
			Name     *string `json:"name"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if !sbcrypto.ValidPassword(in.Password) {
			return errWeakPassword
		}
		ctx := r.Context()
		hash := sbcrypto.HashPassword(in.Password)
		var u *store.User
		err := s.Store.Tx(ctx, func(tx *store.Store) error {
			inv, err := tx.ConsumeAuthToken(ctx, "invite", sbcrypto.SHA256Hex(strings.TrimSpace(in.Token)))
			if errors.Is(err, store.ErrNotFound) {
				return httpx.Unauthorized("this invite is invalid, used or expired; ask the admin for a new one")
			}
			if err != nil {
				return err
			}
			role := "member"
			if inv.Role != nil {
				role = *inv.Role
			}
			u, err = tx.CreateUser(ctx, *inv.Email, hash, trimName(in.Name), role)
			if errors.Is(err, store.ErrConflict) {
				return httpx.Conflict("an account with this email already exists; sign in instead")
			}
			return err
		})
		if err != nil {
			return err
		}
		s.Audit(ctx, u.ID, "", "invite.accept", map[string]any{"role": u.Role})
		if err := s.startSession(ctx, w, u); err != nil {
			return err
		}
		return httpx.WriteJSON(w, 201, map[string]any{"user": u})
	})

	s.handle("POST /v1/auth/logout", func(w http.ResponseWriter, r *http.Request) error {
		if c, err := r.Cookie(sessionCookie); err == nil {
			_ = s.Store.DeleteSession(r.Context(), sbcrypto.SHA256Hex(c.Value))
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
		return httpx.NoContent(w)
	})

	s.user("GET /v1/me", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		return httpx.OK(w, map[string]any{"user": p.User})
	})

	s.user("PATCH /v1/me", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			Name            *string `json:"name"`
			CurrentPassword string  `json:"currentPassword"`
			NewPassword     string  `json:"newPassword"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		if in.Name != nil {
			if err := s.Store.UpdateUserProfile(ctx, p.User.ID, trimName(in.Name)); err != nil {
				return err
			}
		}
		if in.NewPassword != "" {
			if !sbcrypto.VerifyPassword(in.CurrentPassword, p.User.PasswordHash) {
				return httpx.E(403, "wrong_password", "the current password is wrong")
			}
			if !sbcrypto.ValidPassword(in.NewPassword) {
				return errWeakPassword
			}
			if err := s.Store.SetUserPassword(ctx, p.User.ID, sbcrypto.HashPassword(in.NewPassword)); err != nil {
				return err
			}
			s.Audit(ctx, p.User.ID, "", "user.password_change", nil)
		}
		u, err := s.Store.GetUser(ctx, p.User.ID)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"user": u})
	})

	s.user("GET /v1/api-keys", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		keys, err := s.Store.ListAPIKeys(r.Context(), p.User.ID)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"keys": keys})
	})

	s.user("POST /v1/api-keys", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			Name          string  `json:"name"`
			ProjectID     *string `json:"projectId"`
			ExpiresInDays *int    `json:"expiresInDays"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || len(in.Name) > 60 {
			return httpx.Invalid("name must be 1 to 60 characters")
		}
		if p.Key != nil && p.Key.ProjectID != nil {
			return httpx.Forbidden("project-scoped keys cannot create keys")
		}
		if in.ProjectID != nil {
			proj, err := s.Store.GetProject(ctx, *in.ProjectID)
			if err != nil || !p.canAccess(proj) {
				return httpx.NotFound("project")
			}
		}
		var expires *time.Time
		if in.ExpiresInDays != nil {
			if *in.ExpiresInDays < 1 || *in.ExpiresInDays > 3650 {
				return httpx.Invalid("expiresInDays must be between 1 and 3650")
			}
			t := time.Now().Add(time.Duration(*in.ExpiresInDays) * 24 * time.Hour)
			expires = &t
		}
		full, prefix := sbcrypto.NewKey(sbcrypto.PrefixAccount)
		k, err := s.Store.CreateAPIKey(ctx, p.User.ID, in.ProjectID, in.Name, prefix, sbcrypto.SHA256Hex(full), expires)
		if err != nil {
			return err
		}
		s.Audit(ctx, p.User.ID, "", "api_key.create", map[string]any{"key": k.ID})
		return httpx.WriteJSON(w, 201, map[string]any{"key": k, "secret": full})
	})

	s.user("DELETE /v1/api-keys/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		ok, err := s.Store.RevokeAPIKey(r.Context(), p.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if !ok {
			return httpx.NotFound("API key")
		}
		s.Audit(r.Context(), p.User.ID, "", "api_key.revoke", map[string]any{"key": r.PathValue("id")})
		return httpx.NoContent(w)
	})

	s.user("GET /v1/notifications", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		list, err := s.Store.ListNotifications(r.Context(), p.User.ID, queryInt(r, "limit", 50, 1, 200))
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"notifications": list})
	})

	s.user("POST /v1/notifications/read", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		if err := s.Store.MarkNotificationsRead(r.Context(), p.User.ID); err != nil {
			return err
		}
		return httpx.NoContent(w)
	})
}
