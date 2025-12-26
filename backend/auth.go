package backend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// GenerateJWT creates an HS256 JWT with a 24h expiry.
func GenerateJWT(userID, email, displayName string) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", errors.New("JWT_SECRET not configured")
	}
	claims := jwt.MapClaims{
		"sub":          userID,
		"email":        email,
		"display_name": displayName,
		"iat":          time.Now().Unix(),
		"exp":          time.Now().Add(24 * time.Hour).Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(secret))
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

		token, err := GenerateJWT(id, email, display.String)
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}

		// setting httpOnly cookie (secure when TLS)
		cookie := &http.Cookie{
			Name:     "access_token",
			Value:    token,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Path:     "/",
			Expires:  time.Now().Add(24 * time.Hour),
		}
		if r.TLS != nil {
			cookie.Secure = true
		}
		http.SetCookie(w, cookie)

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

// LogoutHandler clears the httpOnly access_token cookie.
func LogoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie := &http.Cookie{
			Name:     "access_token",
			Value:    "",
			Path:     "/",
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		if r.TLS != nil {
			cookie.Secure = true
		}
		http.SetCookie(w, cookie)
		w.WriteHeader(http.StatusOK)
	})
}
