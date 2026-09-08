package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// authMiddleware checks basic auth
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)

		auth := strings.SplitN(r.Header.Get("Authorization"), " ", 2)

		if len(auth) != 2 || auth[0] != "Basic" {
			http.Error(w, "authorization failed", http.StatusUnauthorized)
			return
		}

		payload, err := base64.StdEncoding.DecodeString(auth[1])
		if err != nil {
			http.Error(w, "authorization failed", http.StatusUnauthorized)
			return
		}

		pair := strings.SplitN(string(payload), ":", 2)
		if len(pair) != 2 {
			http.Error(w, "authorization failed", http.StatusUnauthorized)
			return
		}

		// Both comparisons always run, and each is constant time, so neither
		// the username nor the password leaks through response timing.
		userMatch := subtle.ConstantTimeCompare([]byte(pair[0]), []byte(username))
		passMatch := subtle.ConstantTimeCompare([]byte(pair[1]), []byte(password))

		if userMatch&passMatch != 1 {
			http.Error(w, "authorization failed", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func parseAuth(auth string) {
	identity := strings.Split(auth, ":")
	if len(identity) != 2 {
		log.Fatal().Msg("basic auth must be like this: user:password")
	}

	username = identity[0]
	password = identity[1]
}

func generateRandomAuth() {
	username = *defaultUsernameBasicAuth
	password = generateRandomString()
	log.Info().Str("user", username).Str("password", password).Msg("User generated for basic auth")
}

func generateRandomString() string {

	b := make([]byte, *sizeRandom)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%X", b)
}
