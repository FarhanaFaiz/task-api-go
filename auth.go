package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const userContextKey contextKey = "authenticated-user"

type authClaims struct {
	UserID int `json:"user_id"`
	jwt.RegisteredClaims
}

func registerHandler(users userStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON")
			return
		}
		request.Email = strings.ToLower(strings.TrimSpace(request.Email))
		if request.Email == "" || !strings.Contains(request.Email, "@") || len(request.Password) < 8 {
			writeError(w, http.StatusBadRequest, "A valid email and password of at least 8 characters are required")
			return
		}

		passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Unable to secure password")
			return
		}
		user, err := users.CreateUser(r.Context(), request.Email, passwordHash)
		if errors.Is(err, errEmailExists) {
			writeError(w, http.StatusConflict, "Email is already registered")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Unable to register user")
			return
		}
		writeJSON(w, http.StatusCreated, user)
	}
}

func loginHandler(users userStore, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON")
			return
		}

		user, passwordHash, err := users.GetUserByEmail(r.Context(), request.Email)
		if err != nil || bcrypt.CompareHashAndPassword(passwordHash, []byte(request.Password)) != nil {
			writeError(w, http.StatusUnauthorized, "Invalid email or password")
			return
		}

		now := time.Now()
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, authClaims{
			UserID: user.ID,
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   user.Email,
				IssuedAt:  jwt.NewNumericDate(now),
				ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			},
		})
		signedToken, err := token.SignedString([]byte(jwtSecret))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Unable to create token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"token": signedToken})
	}
}

func authMiddleware(jwtSecret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		scheme, tokenString, found := strings.Cut(header, " ")
		if !found || !strings.EqualFold(scheme, "Bearer") || tokenString == "" {
			writeError(w, http.StatusUnauthorized, "Authorization bearer token is required")
			return
		}

		token, err := jwt.ParseWithClaims(tokenString, &authClaims{}, func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(jwtSecret), nil
		})
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}
		claims, ok := token.Claims.(*authClaims)
		if !ok || claims.UserID < 1 {
			writeError(w, http.StatusUnauthorized, "Invalid token claims")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, claims.UserID)))
	})
}

func authenticatedUserID(r *http.Request) (int, bool) {
	userID, ok := r.Context().Value(userContextKey).(int)
	return userID, ok && userID > 0
}
