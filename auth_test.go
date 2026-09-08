package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

// setCredentials installs the package level credentials for the duration of a
// test and restores whatever was there before.
func setCredentials(t *testing.T, user, pass string) {
	t.Helper()
	prevUser, prevPass := username, password
	username, password = user, pass
	t.Cleanup(func() { username, password = prevUser, prevPass })
}

func basicHeader(payload string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(payload))
}

func TestAuthMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{name: "correct credentials", header: basicHeader("gopher:s3cret"), wantStatus: http.StatusOK},
		{name: "no header", header: "", wantStatus: http.StatusUnauthorized},
		{name: "wrong password", header: basicHeader("gopher:wrong"), wantStatus: http.StatusUnauthorized},
		{name: "wrong username", header: basicHeader("nobody:s3cret"), wantStatus: http.StatusUnauthorized},
		{name: "empty credentials", header: basicHeader(":"), wantStatus: http.StatusUnauthorized},
		{name: "unsupported scheme", header: "Bearer " + base64.StdEncoding.EncodeToString([]byte("gopher:s3cret")), wantStatus: http.StatusUnauthorized},
		{name: "scheme without payload", header: "Basic", wantStatus: http.StatusUnauthorized},
		{name: "undecodable base64", header: "Basic !!!not-base64!!!", wantStatus: http.StatusUnauthorized},
		// The payload below decodes to the username with no colon. It used to
		// index pair[1] out of range and panic the handler, and only for this
		// input, because || short-circuited whenever the username differed.
		{name: "username only, no colon", header: basicHeader("gopher"), wantStatus: http.StatusUnauthorized},
		{name: "empty payload", header: basicHeader(""), wantStatus: http.StatusUnauthorized},
		{name: "colon only in username position", header: basicHeader("gopher:"), wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCredentials(t, "gopher", "s3cret")

			var served bool
			handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				served = true
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if wantServed := tt.wantStatus == http.StatusOK; served != wantServed {
				t.Errorf("next handler served = %v, want %v", served, wantServed)
			}
		})
	}
}

// A password containing a colon must survive the split intact.
func TestAuthMiddlewarePasswordWithColon(t *testing.T) {
	setCredentials(t, "gopher", "pass:with:colons")

	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", basicHeader("gopher:pass:with:colons"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuthMiddlewareSetsWWWAuthenticate(t *testing.T) {
	setCredentials(t, "gopher", "s3cret")

	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Header().Get("WWW-Authenticate"), `Basic realm="Restricted"`; got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
}

// parseAuth used to ignore its argument and read the *setBasicAuth global.
func TestParseAuthUsesItsArgument(t *testing.T) {
	setCredentials(t, "", "")

	other := "ignored:ignored"
	prev := setBasicAuth
	setBasicAuth = &other
	t.Cleanup(func() { setBasicAuth = prev })

	parseAuth("alice:wonderland")

	if username != "alice" {
		t.Errorf("username = %q, want %q", username, "alice")
	}
	if password != "wonderland" {
		t.Errorf("password = %q, want %q", password, "wonderland")
	}
}

func TestGenerateRandomAuth(t *testing.T) {
	setCredentials(t, "", "")

	size := 8
	prevSize, prevUser := sizeRandom, defaultUsernameBasicAuth
	user := "gopher"
	sizeRandom, defaultUsernameBasicAuth = &size, &user
	t.Cleanup(func() { sizeRandom, defaultUsernameBasicAuth = prevSize, prevUser })

	generateRandomAuth()

	if username != "gopher" {
		t.Errorf("username = %q, want %q", username, "gopher")
	}
	// The password is hex encoded, so it is twice the requested byte count.
	if len(password) != 2*size {
		t.Errorf("len(password) = %d, want %d", len(password), 2*size)
	}
}

func TestGenerateRandomStringIsNotRepeated(t *testing.T) {
	size := 16
	prev := sizeRandom
	sizeRandom = &size
	t.Cleanup(func() { sizeRandom = prev })

	first, second := generateRandomString(), generateRandomString()

	if len(first) != 2*size {
		t.Errorf("len = %d, want %d", len(first), 2*size)
	}
	if first == second {
		t.Errorf("two generated passwords were identical: %q", first)
	}
}
