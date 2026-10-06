package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/jwt"
	sbmail "github.com/shellhaki/sparkbase/server/internal/mail"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

const (
	otpTTL         = 15 * time.Minute
	maxOTPAttempts = 5
)

type authUser struct {
	ID               string         `json:"id"`
	Email            string         `json:"email"`
	Data             map[string]any `json:"data"`
	AppData          map[string]any `json:"appData"`
	EmailConfirmedAt *time.Time     `json:"emailConfirmedAt"`
	LastSignInAt     *time.Time     `json:"lastSignInAt"`
	BannedAt         *time.Time     `json:"bannedAt"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

const authUserCols = `id, email, data, app_data, email_confirmed_at, last_sign_in_at, banned_at, created_at, updated_at`

func scanAuthUser(row pgx.Row) (*authUser, error) {
	var u authUser
	err := row.Scan(&u.ID, &u.Email, &u.Data, &u.AppData, &u.EmailConfirmedAt, &u.LastSignInAt, &u.BannedAt, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("user")
	}
	if err != nil {
		return nil, queryError(err)
	}
	return &u, nil
}

type authSettings struct {
	AllowSignups    bool     `json:"allowSignups"`
	SiteURL         string   `json:"siteUrl"`
	RedirectURLs    []string `json:"redirectUrls"`
	AccessTokenTTL  int      `json:"accessTokenTtl"`
	RefreshTokenTTL int      `json:"refreshTokenTtl"`
}

func loadAuthSettings(ctx context.Context, pool *pgxpool.Pool) (*authSettings, error) {
	rows, err := pool.Query(ctx, `select key, value from sb_settings`)
	if err != nil {
		return nil, queryError(err)
	}
	defer rows.Close()
	st := &authSettings{AllowSignups: true, AccessTokenTTL: 3600, RefreshTokenTTL: 30 * 24 * 3600, RedirectURLs: []string{}}
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		switch k {
		case "allow_signups":
			_ = json.Unmarshal(v, &st.AllowSignups)
		case "site_url":
			_ = json.Unmarshal(v, &st.SiteURL)
		case "redirect_urls":
			_ = json.Unmarshal(v, &st.RedirectURLs)
		case "access_token_ttl":
			_ = json.Unmarshal(v, &st.AccessTokenTTL)
		case "refresh_token_ttl":
			_ = json.Unmarshal(v, &st.RefreshTokenTTL)
		}
	}
	return st, rows.Err()
}

// allowedRedirect reports whether a magic-link target is the site URL or
// starts with an allowlisted URL. Unlisted targets would leak tokens.
func (st *authSettings) allowedRedirect(target string) bool {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return false
	}
	for _, allowed := range append([]string{st.SiteURL}, st.RedirectURLs...) {
		if allowed != "" && (target == allowed || strings.HasPrefix(target, strings.TrimRight(allowed, "/")+"/") || strings.HasPrefix(target, allowed+"?")) {
			return true
		}
	}
	return false
}

func sixDigits() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// authCtx is one request against the project's auth service.
type authCtx struct {
	res    *store.Resource
	pool   *pgxpool.Pool
	secret []byte
	issuer string
}

func (s *Server) authService(r *http.Request, pc *pctx) (*authCtx, error) {
	res, err := s.service(r, pc, "auth", "")
	if err != nil {
		return nil, err
	}
	pool, err := s.Tenants.PG(r.Context(), res)
	if err != nil {
		return nil, err
	}
	cred, err := s.Store.CurrentCredential(r.Context(), res.ID)
	if err != nil {
		return nil, err
	}
	c, err := s.OpenCredential(cred)
	if err != nil {
		return nil, err
	}
	return &authCtx{res: res, pool: pool, secret: []byte(c.Extra), issuer: s.Cfg.ProjectURL(res.ProjectSlug) + "/auth"}, nil
}

// issueSession issues an access token and a refresh token in a token family.
func (s *Server) issueSession(ctx context.Context, a *authCtx, tx pgx.Tx, u *authUser, family string, st *authSettings) (map[string]any, error) {
	if family == "" {
		family = newUUID()
	}
	refresh := sbcrypto.Token()
	if _, err := tx.Exec(ctx, `
		insert into sb_refresh_tokens (user_id, token_hash, family, expires_at)
		values ($1, $2, $3, now() + make_interval(secs => $4))`, u.ID, sbcrypto.SHA256Hex(refresh), family, st.RefreshTokenTTL); err != nil {
		return nil, queryError(err)
	}
	now := time.Now()
	exp := now.Add(time.Duration(st.AccessTokenTTL) * time.Second)
	access, err := jwt.Sign(map[string]any{
		"iss": a.issuer, "sub": u.ID, "aud": "authenticated", "role": "authenticated",
		"email": u.Email, "session_id": family, "iat": now.Unix(), "exp": exp.Unix(),
		"user_metadata": u.Data, "app_metadata": u.AppData,
	}, a.secret)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"access_token": access, "token_type": "bearer", "expires_in": st.AccessTokenTTL,
		"expires_at": exp.Unix(), "refresh_token": refresh, "user": u,
	}, nil
}

func newUUID() string {
	b := sbcrypto.RandomBytes(16)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// endUser verifies the end-user access token on the request.
func (s *Server) endUser(r *http.Request, pc *pctx, a *authCtx) (map[string]any, error) {
	if pc.UserToken == "" {
		return nil, httpx.Unauthorized("send the user's access token as Authorization: Bearer <token>")
	}
	claims, err := jwt.Verify(pc.UserToken, a.secret, time.Now())
	if errors.Is(err, jwt.ErrExpired) {
		return nil, httpx.Unauthorized("the access token has expired; refresh the session")
	}
	if err != nil || claims["iss"] != a.issuer {
		return nil, httpx.Unauthorized("invalid access token")
	}
	return claims, nil
}

// sendOTP creates a one-time link token and code and emails them.
func (s *Server) sendOTP(ctx context.Context, a *authCtx, st *authSettings, email, redirectTo string, data map[string]any, send bool) (token, code, link string, err error) {
	token, code = sbcrypto.Token(), sixDigits()
	var redirect *string
	if redirectTo != "" {
		redirect = &redirectTo
	}
	var dataJSON []byte
	if data != nil {
		dataJSON, _ = json.Marshal(data)
	}
	if _, err := a.pool.Exec(ctx, `
		insert into sb_otps (email, token_hash, code_hash, redirect_to, data, expires_at)
		values ($1, $2, $3, $4, $5, now() + make_interval(secs => $6))`,
		email, sbcrypto.SHA256Hex(token), sbcrypto.SHA256Hex(email+":"+code), redirect, dataJSON, otpTTL.Seconds()); err != nil {
		return "", "", "", queryError(err)
	}
	if redirectTo != "" {
		sep := "?"
		if strings.Contains(redirectTo, "?") {
			sep = "&"
		}
		link = redirectTo + sep + "token=" + url.QueryEscape(token)
	}
	if !send {
		return token, code, link, nil
	}
	text := "Your sign-in code is " + code + ".\n\n"
	if link != "" {
		text += "Or open this link to sign in:\n\n" + link + "\n\n"
	}
	text += "It works once and expires in 15 minutes. If you did not ask for it, ignore this email.\n"
	err = s.Mail.Send(ctx, sbmail.Message{To: email, Subject: "Your sign-in code: " + code, Text: text, Link: link + " code=" + code})
	return token, code, link, err
}

func (s *Server) authServiceRoutes() {
	s.papi("POST /p/{ref}/auth/signup", accessPublishable, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Email    string         `json:"email"`
			Password string         `json:"password"`
			Data     map[string]any `json:"data"`
		}
		if err := httpx.Decode(r, &in, 64<<10); err != nil {
			return err
		}
		if err := s.ipLimit(w, r, "authsignup:"+pc.Project.ID, 20, 10*time.Minute); err != nil {
			return err
		}
		email, ok := NormalizeEmail(in.Email)
		if !ok {
			return httpx.Invalid("enter a valid email address")
		}
		if !sbcrypto.ValidPassword(in.Password) {
			return errWeakPassword
		}
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		st, err := loadAuthSettings(ctx, a.pool)
		if err != nil {
			return err
		}
		if !st.AllowSignups {
			return httpx.Forbidden("sign-ups are closed for this app")
		}
		tx, err := a.pool.Begin(ctx)
		if err != nil {
			return queryError(err)
		}
		defer tx.Rollback(context.Background())
		if max := a.res.Limits.Get("max_users"); max > 0 {
			var n int64
			if err := tx.QueryRow(ctx, `select count(*) from sb_users`).Scan(&n); err != nil {
				return queryError(err)
			}
			if n >= max {
				return httpx.Quota("this app reached its user limit")
			}
		}
		if in.Data == nil {
			in.Data = map[string]any{}
		}
		u, err := scanAuthUser(tx.QueryRow(ctx, `
			insert into sb_users (email, password_hash, data, last_sign_in_at) values ($1, $2, $3, now())
			on conflict (email) do nothing returning `+authUserCols, email, sbcrypto.HashPassword(in.Password), in.Data))
		var e *httpx.Error
		if errors.As(err, &e) && e.Code == "not_found" {
			return httpx.Conflict("a user with this email exists; sign in instead")
		}
		if err != nil {
			return err
		}
		sess, err := s.issueSession(ctx, a, tx, u, "", st)
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return queryError(err)
		}
		return httpx.WriteJSON(w, 201, map[string]any{"session": sess, "user": u})
	})

	// Password recovery emails a one-time link and code. Verifying it signs
	// the user in, and the app then sets a new password with PUT /auth/user.
	s.papi("POST /p/{ref}/auth/recover", accessPublishable, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Email      string `json:"email"`
			RedirectTo string `json:"redirectTo"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		email, ok := NormalizeEmail(in.Email)
		if !ok {
			return httpx.Invalid("enter a valid email address")
		}
		if err := s.ipLimit(w, r, "authotp:"+pc.Project.ID, 30, 10*time.Minute); err != nil {
			return err
		}
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		st, err := loadAuthSettings(ctx, a.pool)
		if err != nil {
			return err
		}
		if in.RedirectTo == "" {
			in.RedirectTo = st.SiteURL
		} else if !st.allowedRedirect(in.RedirectTo) {
			return httpx.Invalid("redirectTo is not the site URL or in the redirect allowlist; add it in the auth settings")
		}
		if res, err := s.Limiter.Allow(ctx, "authotp:email:"+a.res.ID+":"+sbcrypto.SHA256Hex(email), 5, 15*time.Minute); err == nil && !res.Allowed {
			return httpx.OK(w, map[string]any{"sent": true})
		}
		var exists bool
		if err := a.pool.QueryRow(ctx, `select exists(select 1 from sb_users where email = $1 and banned_at is null)`, email).Scan(&exists); err != nil {
			return queryError(err)
		}
		if exists {
			if _, _, _, err := s.sendOTP(ctx, a, st, email, in.RedirectTo, nil, true); err != nil {
				return err
			}
		}
		return httpx.OK(w, map[string]any{"sent": true})
	})

	s.papi("POST /p/{ref}/auth/otp", accessPublishable, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Email      string         `json:"email"`
			RedirectTo string         `json:"redirectTo"`
			Data       map[string]any `json:"data"`
			CreateUser *bool          `json:"createUser"`
		}
		if err := httpx.Decode(r, &in, 64<<10); err != nil {
			return err
		}
		email, ok := NormalizeEmail(in.Email)
		if !ok {
			return httpx.Invalid("enter a valid email address")
		}
		if err := s.ipLimit(w, r, "authotp:"+pc.Project.ID, 30, 10*time.Minute); err != nil {
			return err
		}
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		st, err := loadAuthSettings(ctx, a.pool)
		if err != nil {
			return err
		}
		if in.RedirectTo == "" {
			in.RedirectTo = st.SiteURL
		} else if !st.allowedRedirect(in.RedirectTo) {
			return httpx.Invalid("redirectTo is not the site URL or in the redirect allowlist; add it in the auth settings")
		}
		// Per-email cap; over it, answer the same without sending.
		if res, err := s.Limiter.Allow(ctx, "authotp:email:"+a.res.ID+":"+sbcrypto.SHA256Hex(email), 5, 15*time.Minute); err == nil && !res.Allowed {
			return httpx.OK(w, map[string]any{"sent": true})
		}
		var exists bool
		if err := a.pool.QueryRow(ctx, `select exists(select 1 from sb_users where email = $1 and banned_at is null)`, email).Scan(&exists); err != nil {
			return queryError(err)
		}
		create := in.CreateUser == nil || *in.CreateUser
		if !exists && (!create || !st.AllowSignups) {
			return httpx.OK(w, map[string]any{"sent": true})
		}
		if _, _, _, err := s.sendOTP(ctx, a, st, email, in.RedirectTo, in.Data, true); err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"sent": true})
	})

	s.papi("POST /p/{ref}/auth/verify", accessPublishable, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Token string `json:"token"`
			Email string `json:"email"`
			Code  string `json:"code"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if err := s.ipLimit(w, r, "authverify:"+pc.Project.ID, 60, 10*time.Minute); err != nil {
			return err
		}
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		st, err := loadAuthSettings(ctx, a.pool)
		if err != nil {
			return err
		}
		tx, err := a.pool.Begin(ctx)
		if err != nil {
			return queryError(err)
		}
		defer tx.Rollback(context.Background())

		var otpID, email string
		var data map[string]any
		invalid := httpx.Unauthorized("the code or link is invalid or has expired; request a new one")
		switch {
		case in.Token != "":
			err = tx.QueryRow(ctx, `
				update sb_otps set used_at = now()
				where token_hash = $1 and used_at is null and expires_at > now() and attempts < $2
				returning id, email, data`, sbcrypto.SHA256Hex(in.Token), maxOTPAttempts).Scan(&otpID, &email, &data)
			if errors.Is(err, pgx.ErrNoRows) {
				return invalid
			}
		case in.Email != "" && in.Code != "":
			e, ok := NormalizeEmail(in.Email)
			if !ok {
				return invalid
			}
			var codeHash string
			var attempts int
			err = tx.QueryRow(ctx, `
				select id, code_hash, attempts, data from sb_otps
				where email = $1 and used_at is null and expires_at > now()
				order by created_at desc limit 1 for update`, e).Scan(&otpID, &codeHash, &attempts, &data)
			if errors.Is(err, pgx.ErrNoRows) {
				return invalid
			}
			if err != nil {
				return queryError(err)
			}
			if attempts >= maxOTPAttempts {
				return invalid
			}
			if subtle.ConstantTimeCompare([]byte(codeHash), []byte(sbcrypto.SHA256Hex(e+":"+strings.TrimSpace(in.Code)))) != 1 {
				// Count the miss and commit it, so repeated guesses lock the code.
				if _, err := tx.Exec(ctx, `update sb_otps set attempts = attempts + 1 where id = $1`, otpID); err != nil {
					return queryError(err)
				}
				if err := tx.Commit(ctx); err != nil {
					return queryError(err)
				}
				return invalid
			}
			if _, err := tx.Exec(ctx, `update sb_otps set used_at = now() where id = $1`, otpID); err != nil {
				return queryError(err)
			}
			email = e
		default:
			return httpx.Invalid("send a token, or an email and code")
		}
		if err != nil {
			return queryError(err)
		}

		u, err := scanAuthUser(tx.QueryRow(ctx, `select `+authUserCols+` from sb_users where email = $1`, email))
		var notFound *httpx.Error
		if errors.As(err, &notFound) && notFound.Code == "not_found" {
			if !st.AllowSignups {
				return httpx.Forbidden("sign-ups are closed for this app")
			}
			if max := a.res.Limits.Get("max_users"); max > 0 {
				var n int64
				if err := tx.QueryRow(ctx, `select count(*) from sb_users`).Scan(&n); err != nil {
					return queryError(err)
				}
				if n >= max {
					return httpx.Quota("this app reached its user limit")
				}
			}
			if data == nil {
				data = map[string]any{}
			}
			u, err = scanAuthUser(tx.QueryRow(ctx, `
				insert into sb_users (email, data, email_confirmed_at) values ($1, $2, now())
				on conflict (email) do update set updated_at = now()
				returning `+authUserCols, email, data))
		}
		if err != nil {
			return err
		}
		if u.BannedAt != nil {
			return httpx.Forbidden("this user is banned")
		}
		u, err = scanAuthUser(tx.QueryRow(ctx, `
			update sb_users set last_sign_in_at = now(), email_confirmed_at = coalesce(email_confirmed_at, now())
			where id = $1 returning `+authUserCols, u.ID))
		if err != nil {
			return err
		}
		sess, err := s.issueSession(ctx, a, tx, u, "", st)
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"session": sess, "user": u})
	})

	s.papi("POST /p/{ref}/auth/token", accessPublishable, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		grant := r.URL.Query().Get("grant_type")
		if grant == "password" {
			return s.passwordGrant(w, r, pc)
		}
		if grant != "refresh_token" {
			return httpx.Invalid("grant_type must be password or refresh_token")
		}
		var in struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if err := s.ipLimit(w, r, "authrefresh:"+pc.Project.ID, 300, 10*time.Minute); err != nil {
			return err
		}
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		st, err := loadAuthSettings(ctx, a.pool)
		if err != nil {
			return err
		}
		tx, err := a.pool.Begin(ctx)
		if err != nil {
			return queryError(err)
		}
		defer tx.Rollback(context.Background())
		var id int64
		var userID, family string
		var revoked bool
		var expires time.Time
		err = tx.QueryRow(ctx, `select id, user_id, family, revoked, expires_at from sb_refresh_tokens where token_hash = $1 for update`,
			sbcrypto.SHA256Hex(in.RefreshToken)).Scan(&id, &userID, &family, &revoked, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.Unauthorized("invalid refresh token")
		}
		if err != nil {
			return queryError(err)
		}
		if revoked {
			// A used token came back: assume it was stolen and end the family.
			if _, err := tx.Exec(ctx, `update sb_refresh_tokens set revoked = true where family = $1`, family); err != nil {
				return queryError(err)
			}
			if err := tx.Commit(ctx); err != nil {
				return queryError(err)
			}
			return httpx.Unauthorized("this refresh token was already used; sign in again")
		}
		if time.Now().After(expires) {
			return httpx.Unauthorized("the session has expired; sign in again")
		}
		if _, err := tx.Exec(ctx, `update sb_refresh_tokens set revoked = true where id = $1`, id); err != nil {
			return queryError(err)
		}
		u, err := scanAuthUser(tx.QueryRow(ctx, `select `+authUserCols+` from sb_users where id = $1`, userID))
		if err != nil {
			return httpx.Unauthorized("the user no longer exists")
		}
		if u.BannedAt != nil {
			return httpx.Forbidden("this user is banned")
		}
		sess, err := s.issueSession(ctx, a, tx, u, family, st)
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"session": sess, "user": u})
	})

	s.papi("POST /p/{ref}/auth/logout", accessPublishable, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		claims, err := s.endUser(r, pc, a)
		if err != nil {
			return err
		}
		ctx := r.Context()
		if r.URL.Query().Get("scope") == "global" {
			_, err = a.pool.Exec(ctx, `update sb_refresh_tokens set revoked = true where user_id = $1`, claims["sub"])
		} else {
			_, err = a.pool.Exec(ctx, `update sb_refresh_tokens set revoked = true where family = $1`, claims["session_id"])
		}
		if err != nil {
			return queryError(err)
		}
		return httpx.NoContent(w)
	})

	s.papi("GET /p/{ref}/auth/user", accessPublishable, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		claims, err := s.endUser(r, pc, a)
		if err != nil {
			return err
		}
		u, err := scanAuthUser(a.pool.QueryRow(r.Context(), `select `+authUserCols+` from sb_users where id = $1`, claims["sub"]))
		if err != nil {
			return httpx.Unauthorized("the user no longer exists")
		}
		return httpx.OK(w, map[string]any{"user": u})
	})

	s.papi("PUT /p/{ref}/auth/user", accessPublishable, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Data     map[string]any `json:"data"`
			Password *string        `json:"password"`
		}
		if err := httpx.Decode(r, &in, 64<<10); err != nil {
			return err
		}
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		claims, err := s.endUser(r, pc, a)
		if err != nil {
			return err
		}
		if in.Data == nil {
			in.Data = map[string]any{}
		}
		var hash *string
		if in.Password != nil {
			if !sbcrypto.ValidPassword(*in.Password) {
				return errWeakPassword
			}
			h := sbcrypto.HashPassword(*in.Password)
			hash = &h
		}
		u, err := scanAuthUser(a.pool.QueryRow(r.Context(), `
			update sb_users set data = data || $2, password_hash = coalesce($3, password_hash), updated_at = now()
			where id = $1 returning `+authUserCols, claims["sub"], in.Data, hash))
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"user": u})
	})

	// Admin routes (secret key)

	s.papi("GET /p/{ref}/auth/admin/users", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		q := r.URL.Query()
		limit, offset := queryInt(r, "limit", 50, 1, 1000), queryInt(r, "offset", 0, 0, 1<<30)
		rows, err := a.pool.Query(r.Context(), `
			select `+authUserCols+` from sb_users
			where ($1 = '' or email ilike '%' || $1 || '%')
			order by created_at desc limit $2 offset $3`, q.Get("email"), limit, offset)
		if err != nil {
			return queryError(err)
		}
		defer rows.Close()
		users := []*authUser{}
		for rows.Next() {
			u, err := scanAuthUser(rows)
			if err != nil {
				return err
			}
			users = append(users, u)
		}
		var total int64
		_ = a.pool.QueryRow(r.Context(), `select count(*) from sb_users`).Scan(&total)
		return httpx.OK(w, map[string]any{"users": users, "total": total})
	})

	s.papi("POST /p/{ref}/auth/admin/users", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Email    string         `json:"email"`
			Password string         `json:"password"`
			Data     map[string]any `json:"data"`
			AppData  map[string]any `json:"appData"`
		}
		if err := httpx.Decode(r, &in, 64<<10); err != nil {
			return err
		}
		email, ok := NormalizeEmail(in.Email)
		if !ok {
			return httpx.Invalid("enter a valid email address")
		}
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		if max := a.res.Limits.Get("max_users"); max > 0 {
			var n int64
			if err := a.pool.QueryRow(ctx, `select count(*) from sb_users`).Scan(&n); err != nil {
				return queryError(err)
			}
			if n >= max {
				return httpx.Quota("this app reached its user limit")
			}
		}
		if in.Data == nil {
			in.Data = map[string]any{}
		}
		if in.AppData == nil {
			in.AppData = map[string]any{}
		}
		var hash *string
		if in.Password != "" {
			if !sbcrypto.ValidPassword(in.Password) {
				return errWeakPassword
			}
			h := sbcrypto.HashPassword(in.Password)
			hash = &h
		}
		u, err := scanAuthUser(a.pool.QueryRow(ctx, `
			insert into sb_users (email, data, app_data, password_hash, email_confirmed_at) values ($1, $2, $3, $4, now())
			on conflict (email) do nothing returning `+authUserCols, email, in.Data, in.AppData, hash))
		var e *httpx.Error
		if errors.As(err, &e) && e.Code == "not_found" {
			return httpx.Conflict("a user with this email exists")
		}
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, 201, map[string]any{"user": u})
	})

	s.papi("GET /p/{ref}/auth/admin/users/{id}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		u, err := scanAuthUser(a.pool.QueryRow(r.Context(), `select `+authUserCols+` from sb_users where id::text = $1`, r.PathValue("id")))
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"user": u})
	})

	s.papi("PATCH /p/{ref}/auth/admin/users/{id}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Data    map[string]any `json:"data"`
			AppData map[string]any `json:"appData"`
			Banned  *bool          `json:"banned"`
		}
		if err := httpx.Decode(r, &in, 64<<10); err != nil {
			return err
		}
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		u, err := scanAuthUser(a.pool.QueryRow(ctx, `
			update sb_users set
			  data = coalesce($2, data), app_data = coalesce($3, app_data),
			  banned_at = case when $4::boolean is null then banned_at when $4 then coalesce(banned_at, now()) else null end,
			  updated_at = now()
			where id::text = $1 returning `+authUserCols, r.PathValue("id"), in.Data, in.AppData, in.Banned))
		if err != nil {
			return err
		}
		if in.Banned != nil && *in.Banned {
			_, _ = a.pool.Exec(ctx, `update sb_refresh_tokens set revoked = true where user_id = $1`, u.ID)
		}
		return httpx.OK(w, map[string]any{"user": u})
	})

	s.papi("DELETE /p/{ref}/auth/admin/users/{id}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		tag, err := a.pool.Exec(r.Context(), `delete from sb_users where id::text = $1`, r.PathValue("id"))
		if err != nil {
			return queryError(err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("user")
		}
		return httpx.NoContent(w)
	})

	s.papi("POST /p/{ref}/auth/admin/generate-link", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Email      string `json:"email"`
			RedirectTo string `json:"redirectTo"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		email, ok := NormalizeEmail(in.Email)
		if !ok {
			return httpx.Invalid("enter a valid email address")
		}
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		st, err := loadAuthSettings(ctx, a.pool)
		if err != nil {
			return err
		}
		if in.RedirectTo == "" {
			in.RedirectTo = st.SiteURL
		} else if !st.allowedRedirect(in.RedirectTo) {
			return httpx.Invalid("redirectTo is not the site URL or in the redirect allowlist")
		}
		token, code, link, err := s.sendOTP(ctx, a, st, email, in.RedirectTo, nil, false)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"token": token, "code": code, "link": link, "expiresIn": int(otpTTL.Seconds())})
	})

	s.papi("GET /p/{ref}/auth/admin/settings", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		st, err := loadAuthSettings(r.Context(), a.pool)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"settings": st})
	})

	s.papi("PATCH /p/{ref}/auth/admin/settings", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			AllowSignups    *bool     `json:"allowSignups"`
			SiteURL         *string   `json:"siteUrl"`
			RedirectURLs    *[]string `json:"redirectUrls"`
			AccessTokenTTL  *int      `json:"accessTokenTtl"`
			RefreshTokenTTL *int      `json:"refreshTokenTtl"`
		}
		if err := httpx.Decode(r, &in, 64<<10); err != nil {
			return err
		}
		set := map[string]any{}
		validURL := func(s string) bool {
			u, err := url.Parse(s)
			return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
		}
		if in.AllowSignups != nil {
			set["allow_signups"] = *in.AllowSignups
		}
		if in.SiteURL != nil {
			if *in.SiteURL != "" && !validURL(*in.SiteURL) {
				return httpx.Invalid("siteUrl must be an http or https URL")
			}
			set["site_url"] = *in.SiteURL
		}
		if in.RedirectURLs != nil {
			if len(*in.RedirectURLs) > 50 {
				return httpx.Invalid("at most 50 redirect URLs")
			}
			for _, u := range *in.RedirectURLs {
				if !validURL(u) {
					return httpx.Invalid("redirect URL " + u + " must be an http or https URL")
				}
			}
			set["redirect_urls"] = *in.RedirectURLs
		}
		if in.AccessTokenTTL != nil {
			if *in.AccessTokenTTL < 60 || *in.AccessTokenTTL > 86400 {
				return httpx.Invalid("accessTokenTtl must be 60 to 86400 seconds")
			}
			set["access_token_ttl"] = *in.AccessTokenTTL
		}
		if in.RefreshTokenTTL != nil {
			if *in.RefreshTokenTTL < 3600 || *in.RefreshTokenTTL > 365*86400 {
				return httpx.Invalid("refreshTokenTtl must be 3600 to 31536000 seconds")
			}
			set["refresh_token_ttl"] = *in.RefreshTokenTTL
		}
		a, err := s.authService(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		for k, v := range set {
			b, _ := json.Marshal(v)
			if _, err := a.pool.Exec(ctx, `insert into sb_settings (key, value) values ($1, $2) on conflict (key) do update set value = excluded.value`, k, string(b)); err != nil {
				return queryError(err)
			}
		}
		st, err := loadAuthSettings(ctx, a.pool)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"settings": st})
	})
}

// passwordGrant signs an end user in with email and password.
func (s *Server) passwordGrant(w http.ResponseWriter, r *http.Request, pc *pctx) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &in, 0); err != nil {
		return err
	}
	if err := s.ipLimit(w, r, "authlogin:"+pc.Project.ID, 60, 10*time.Minute); err != nil {
		return err
	}
	a, err := s.authService(r, pc)
	if err != nil {
		return err
	}
	ctx := r.Context()
	wrong := httpx.Unauthorized("wrong email or password")
	email, ok := NormalizeEmail(in.Email)
	if !ok {
		sbcrypto.DummyVerify(in.Password)
		return wrong
	}
	if res, err := s.Limiter.Allow(ctx, "authlogin:email:"+a.res.ID+":"+sbcrypto.SHA256Hex(email), 10, 15*time.Minute); err == nil && !res.Allowed {
		return httpx.E(429, "rate_limited", "too many sign-in attempts for this email; try again later")
	}
	st, err := loadAuthSettings(ctx, a.pool)
	if err != nil {
		return err
	}
	var hash *string
	var id string
	err = a.pool.QueryRow(ctx, `select id, password_hash from sb_users where email = $1`, email).Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && hash == nil) {
		sbcrypto.DummyVerify(in.Password)
		return wrong
	}
	if err != nil {
		return queryError(err)
	}
	if !sbcrypto.VerifyPassword(in.Password, *hash) {
		return wrong
	}
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return queryError(err)
	}
	defer tx.Rollback(context.Background())
	u, err := scanAuthUser(tx.QueryRow(ctx, `update sb_users set last_sign_in_at = now() where id = $1 returning `+authUserCols, id))
	if err != nil {
		return err
	}
	if u.BannedAt != nil {
		return httpx.Forbidden("this user is banned")
	}
	sess, err := s.issueSession(ctx, a, tx, u, "", st)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return queryError(err)
	}
	return httpx.OK(w, map[string]any{"session": sess, "user": u})
}
