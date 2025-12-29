package backend

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type authCtxKey string

const userIDKey authCtxKey = "userID"

// Access token lifetime
const accessTokenTTL = 15 * time.Minute

// Refresh token lifetime
const refreshTokenTTL = 7 * 24 * time.Hour

// GenerateJWT creates an HS256 JWT with configurable expiry.
func GenerateJWTWithExpiry(userID, email, displayName string, ttl time.Duration) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", errors.New("JWT_SECRET not configured")
	}
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub":          userID,
		"email":        email,
		"display_name": displayName,
		"iat":          now.Unix(),
		"exp":          now.Add(ttl).Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(secret))
}

func GenerateJWT(userID, email, displayName string) (string, error) {
	return GenerateJWTWithExpiry(userID, email, displayName, 24*time.Hour)
}

// parseToken validates and returns MapClaims.
func parseToken(tokenStr string) (jwt.MapClaims, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, errors.New("JWT_SECRET not configured")
	}
	t, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	if !t.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := t.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}
	return claims, nil
}

// GetUserIDFromRequest extracts a token from Authorization header, cookie, or ?token= and returns the "sub".
func GetUserIDFromRequest(r *http.Request) (string, error) {
	// Authorization header
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			token := strings.TrimPrefix(auth, "Bearer ")
			if claims, err := parseToken(token); err == nil {
				if sub, ok := claims["sub"].(string); ok {
					return sub, nil
				}
			} else {
				return "", err
			}
		}
	}
	// cookie
	if c, err := r.Cookie("access_token"); err == nil && c.Value != "" {
		if claims, err := parseToken(c.Value); err == nil {
			if sub, ok := claims["sub"].(string); ok {
				return sub, nil
			}
		} else {
			return "", err
		}
	}
	// query param
	if q := r.URL.Query().Get("token"); q != "" {
		if claims, err := parseToken(q); err == nil {
			if sub, ok := claims["sub"].(string); ok {
				return sub, nil
			}
		} else {
			return "", err
		}
	}
	return "", errors.New("missing or invalid token")
}

// RequireAuth wraps a handler and enforces a valid token; it injects user id into the request context.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, err := GetUserIDFromRequest(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, uid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetUserIDFromCtx returns the authenticated user id from context (or empty string).
func GetUserIDFromCtx(ctx context.Context) string {
	if v := ctx.Value(userIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// helpers for refresh token generation / hashing
func generateRandomToken(nbytes int) (string, error) {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// createRefreshToken inserts a refresh token row and returns the raw token to set in cookie.
func createRefreshToken(pool *pgxpool.Pool, userID string) (string, error) {
	token, err := generateRandomToken(32)
	if err != nil {
		return "", err
	}
	hash := hashToken(token)
	expires := time.Now().Add(refreshTokenTTL)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, hash, expires)
	if err != nil {
		return "", err
	}
	return token, nil
}

// rotateRefreshToken finds a token row by hash, ensures valid, and rotates it (update token_hash/expires). Returns new raw token.
func rotateRefreshToken(pool *pgxpool.Pool, oldToken string) (string, string, error) {
	oldHash := hashToken(oldToken)
	var id string
	var userID string
	var revoked bool
	var expires time.Time
	err := pool.QueryRow(context.Background(), `
		SELECT id::text, user_id::text, revoked, expires_at
		FROM refresh_tokens
		WHERE token_hash = $1
		LIMIT 1
	`, oldHash).Scan(&id, &userID, &revoked, &expires)
	if err != nil {
		return "", "", fmt.Errorf("invalid refresh token")
	}
	if revoked || time.Now().After(expires) {
		return "", "", fmt.Errorf("refresh token revoked or expired")
	}
	// rotate: generate new token and update row
	newTok, err := generateRandomToken(32)
	if err != nil {
		return "", "", err
	}
	newHash := hashToken(newTok)
	newExpires := time.Now().Add(refreshTokenTTL)
	_, err = pool.Exec(context.Background(), `
		UPDATE refresh_tokens SET token_hash=$1, expires_at=$2, created_at=now(), revoked=false WHERE id=$3
	`, newHash, newExpires, id)
	if err != nil {
		return "", "", err
	}
	return newTok, userID, nil
}

// revokeRefreshToken revokes token row matching provided raw token.
func revokeRefreshToken(pool *pgxpool.Pool, token string) error {
	if token == "" {
		return nil
	}
	h := hashToken(token)
	_, err := pool.Exec(context.Background(), `
		UPDATE refresh_tokens SET revoked = true WHERE token_hash=$1
	`, h)
	return err
}

// LoginHandler returns an http.Handler that performs email/password login and issues and httpOnly cookie + returns token.
func LoginHandler(pool *pgxpool.Pool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		var id string
		var pwHash string
		var display sql.NullString
		var email string
		row := pool.QueryRow(r.Context(), `SELECT id, password_hash, display_name, email FROM users WHERE email = $1`, req.Email)
		if err := row.Scan(&id, &pwHash, &display, &email); err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(pwHash), []byte(req.Password)); err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		token, err := GenerateJWTWithExpiry(id, email, display.String, accessTokenTTL)
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}

		// setting httpOnly cookie (secure when TLS)
		accessCookie := &http.Cookie{
			Name:     "access_token",
			Value:    token,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Path:     "/",
			Expires:  time.Now().Add(accessTokenTTL),
		}
		if r.TLS != nil {
			accessCookie.Secure = true
		}
		http.SetCookie(w, accessCookie)

		// creating refresh token row & cookie
		refreshRaw, err := createRefreshToken(pool, id)
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		refreshCookie := &http.Cookie{
			Name:     "refresh_token",
			Value:    refreshRaw,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Path:     "/",
			Expires:  time.Now().Add(refreshTokenTTL),
		}
		if r.TLS != nil {
			refreshCookie.Secure = true
		}
		http.SetCookie(w, refreshCookie)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token": token,
			"user": map[string]interface{}{
				"id":           id,
				"email":        email,
				"display_name": display.String,
			},
		})
	})
}

// RefreshHandler rotates refresh token and issues a new access token.
func RefreshHandler(pool *pgxpool.Pool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// reading the cookie
		cookieHeader := r.Header.Get("Cookie")
		var candidates []string
		if cookieHeader != "" {
			for _, part := range strings.Split(cookieHeader, ";") {
				p := strings.TrimSpace(part)
				if strings.HasPrefix(p, "refresh_token=") {
					candidates = append(candidates, strings.TrimPrefix(p, "refresh_token="))
				}
			}
		}
		if c, err := r.Cookie("refresh_token"); err == nil && c.Value != "" {
			candidates = append([]string{c.Value}, candidates...)
		}

		if len(candidates) == 0 {
			log.Printf("refresh: missing cookie (no candidates)")
			http.Error(w, "missing refresh token", http.StatusUnauthorized)
			return
		}

		var newRaw, userID string
		var rotateErr error
		for _, cand := range candidates {
			log.Printf("refresh: trying candidate (len=%d) from %s", len(cand), r.RemoteAddr)
			newRaw, userID, rotateErr = rotateRefreshToken(pool, cand)
			if rotateErr == nil {
				log.Printf("refresh: rotation succeeded for user %s", userID)
				break
			}
			log.Printf("refresh: candidate rotation failed: %v", rotateErr)
		}
		if rotateErr != nil {
			log.Printf("refresh: all candidates failed")
			http.Error(w, "invalid refresh token", http.StatusUnauthorized)
			return
		}
		// optionally fetching user email/display for claims
		var email string
		var display sql.NullString
		_ = pool.QueryRow(r.Context(), `SELECT email, display_name FROM users WHERE id = $1`, userID).Scan(&email, &display)

		// generating the new access token
		accessTok, err := GenerateJWTWithExpiry(userID, email, display.String, accessTokenTTL)
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		// setting access cookie
		accessCookie := &http.Cookie{
			Name:     "access_token",
			Value:    accessTok,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Path:     "/",
			Expires:  time.Now().Add(accessTokenTTL),
		}
		if r.TLS != nil {
			accessCookie.Secure = true
		}
		http.SetCookie(w, accessCookie)

		// update the refresh cookie with newRaw
		refreshCookie := &http.Cookie{
			Name:     "refresh_token",
			Value:    newRaw,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Path:     "/",
			Expires:  time.Now().Add(refreshTokenTTL),
		}
		if r.TLS != nil {
			refreshCookie.Secure = true
		}
		http.SetCookie(w, refreshCookie)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token": accessTok,
			"user": map[string]interface{}{
				"id":           userID,
				"email":        email,
				"display_name": display.String,
			},
		})
	})
}

// LogoutHandler clears the httpOnly access_token cookie and revokes refresh token.
func LogoutHandler(pool *pgxpool.Pool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// revoking refresh token if cookie present
		c, err := r.Cookie("refresh_token")
		if err == nil && c.Value != "" {
			_ = revokeRefreshToken(pool, c.Value)
		}
		// clearing cookies (access & refresh)
		clear := &http.Cookie{
			Name:     "access_token",
			Value:    "",
			Path:     "/",
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, clear)
		clearR := &http.Cookie{
			Name:     "refresh_token",
			Value:    "",
			Path:     "/",
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, clearR)
		clearR2 := *clearR
		clearR2.Path = "/api"
		http.SetCookie(w, &clearR2)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"message": "logged out",
		})
	})
}

// small type to scan nullable display names
type sqlNullString struct {
	String string
	Valid  bool
}
