package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates a file inside a temporary directory and returns its path.
func writeFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// setHeaderConfigs installs header rules for the duration of a test.
func setHeaderConfigs(t *testing.T, configs HeaderConfigArray) {
	t.Helper()
	prev := headerConfigs
	headerConfigs = configs
	t.Cleanup(func() { headerConfigs = prev })
}

func TestFileExists(t *testing.T) {
	file := writeFile(t, "config.json", "{}")
	dir := t.TempDir()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "regular file", path: file, want: true},
		{name: "directory", path: dir, want: false},
		{name: "missing file", path: filepath.Join(dir, "absent.json"), want: false},
		// Stat fails with ENOTDIR rather than a not-exist error here. The
		// nil FileInfo used to be dereferenced anyway, panicking at startup.
		{name: "path through a regular file", path: filepath.Join(file, "nested.json"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fileExists(tt.path); got != tt.want {
				t.Errorf("fileExists(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestInitHeaderConfig(t *testing.T) {
	valid := `{"configs":[{"path":"*","fileExtension":"css","headers":[{"key":"X-One","value":"1"}]}]}`

	tests := []struct {
		name      string
		contents  string
		wantValid bool
		wantRules int
	}{
		{name: "valid config", contents: valid, wantValid: true, wantRules: 1},
		{name: "malformed json", contents: `{"configs":[ BROKEN`, wantValid: false},
		{name: "empty config list", contents: `{"configs":[]}`, wantValid: false},
		{name: "empty object", contents: `{}`, wantValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setHeaderConfigs(t, HeaderConfigArray{})
			path := writeFile(t, "headerConfig.json", tt.contents)

			if got := initHeaderConfig(path); got != tt.wantValid {
				t.Fatalf("initHeaderConfig() = %v, want %v", got, tt.wantValid)
			}
			if got := len(headerConfigs.Configs); got != tt.wantRules {
				t.Errorf("parsed %d rules, want %d", got, tt.wantRules)
			}
		})
	}
}

func TestInitHeaderConfigUnreadablePaths(t *testing.T) {
	file := writeFile(t, "config.json", "{}")

	tests := []struct {
		name string
		path string
	}{
		{name: "missing file", path: filepath.Join(t.TempDir(), "absent.json")},
		{name: "directory", path: t.TempDir()},
		{name: "path through a regular file", path: filepath.Join(file, "nested.json")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setHeaderConfigs(t, HeaderConfigArray{})

			if initHeaderConfig(tt.path) {
				t.Errorf("initHeaderConfig(%q) = true, want false", tt.path)
			}
		})
	}
}

func TestCustomHeadersMiddleware(t *testing.T) {
	configs := HeaderConfigArray{Configs: []HeaderConfig{
		{
			Path:          "*",
			FileExtension: "css",
			Headers:       []HeaderDefiniton{{Key: "X-Css", Value: "yes"}},
		},
		{
			Path:          "/admin",
			FileExtension: "*",
			Headers:       []HeaderDefiniton{{Key: "X-Admin", Value: "yes"}},
		},
		{
			Path:          "*",
			FileExtension: "*",
			Headers:       []HeaderDefiniton{{Key: "X-All", Value: "yes"}},
		},
	}}

	tests := []struct {
		name        string
		target      string
		wantHeaders map[string]string
		absent      []string
	}{
		{
			name:        "extension rule matches",
			target:      "/site.css",
			wantHeaders: map[string]string{"X-Css": "yes", "X-All": "yes"},
			absent:      []string{"X-Admin"},
		},
		{
			name:        "path prefix rule matches",
			target:      "/admin/page.html",
			wantHeaders: map[string]string{"X-Admin": "yes", "X-All": "yes"},
			absent:      []string{"X-Css"},
		},
		{
			name:        "both rules match",
			target:      "/admin/site.css",
			wantHeaders: map[string]string{"X-Css": "yes", "X-Admin": "yes", "X-All": "yes"},
		},
		{
			name:        "only the catch-all matches",
			target:      "/index.html",
			wantHeaders: map[string]string{"X-All": "yes"},
			absent:      []string{"X-Css", "X-Admin"},
		},
		{
			name:        "extensionless path",
			target:      "/about",
			wantHeaders: map[string]string{"X-All": "yes"},
			absent:      []string{"X-Css"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setHeaderConfigs(t, configs)

			var served bool
			handler := customHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				served = true
			}))

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.target, nil))

			if !served {
				t.Error("next handler was not called")
			}
			for key, want := range tt.wantHeaders {
				if got := rec.Header().Get(key); got != want {
					t.Errorf("header %s = %q, want %q", key, got, want)
				}
			}
			for _, key := range tt.absent {
				if got := rec.Header().Get(key); got != "" {
					t.Errorf("header %s = %q, want it to be absent", key, got)
				}
			}
		})
	}
}

// A later rule wins when two rules set the same header.
func TestCustomHeadersMiddlewareLastRuleWins(t *testing.T) {
	setHeaderConfigs(t, HeaderConfigArray{Configs: []HeaderConfig{
		{Path: "*", FileExtension: "*", Headers: []HeaderDefiniton{{Key: "X-Cache", Value: "first"}}},
		{Path: "*", FileExtension: "*", Headers: []HeaderDefiniton{{Key: "X-Cache", Value: "second"}}},
	}})

	handler := customHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/index.html", nil))

	if got, want := rec.Header().Get("X-Cache"), "second"; got != want {
		t.Errorf("X-Cache = %q, want %q", got, want)
	}
}
