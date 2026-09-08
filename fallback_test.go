package main

import (
	"io"
	"net/http"
	"os"
	"testing"
	"testing/fstest"
)

func testFS(files ...string) http.FileSystem {
	m := fstest.MapFS{}
	for _, name := range files {
		m[name] = &fstest.MapFile{Data: []byte("contents of " + name)}
	}
	return http.FS(m)
}

func readAll(t *testing.T, f http.File) string {
	t.Helper()
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	return string(b)
}

func TestFallbackServesExistingFile(t *testing.T) {
	fb := fallback{defaultPath: "index.html", fs: testFS("index.html", "a/b/c.txt")}

	f, err := fb.Open("/a/b/c.txt")
	if err != nil {
		t.Fatalf("Open(/a/b/c.txt) = %v, want no error", err)
	}
	if got, want := readAll(t, f), "contents of a/b/c.txt"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestFallbackRelative(t *testing.T) {
	tests := []struct {
		name        string
		files       []string
		request     string
		wantContent string
	}{
		{
			name:        "resolves in the requested directory",
			files:       []string{"index.html", "a/index.html"},
			request:     "/a/missing.html",
			wantContent: "contents of a/index.html",
		},
		{
			name:        "walks up to the parent directory",
			files:       []string{"index.html", "a/index.html"},
			request:     "/a/b/missing.html",
			wantContent: "contents of a/index.html",
		},
		{
			name:        "walks up to the root",
			files:       []string{"index.html", "a/b/c.txt"},
			request:     "/a/b/missing.html",
			wantContent: "contents of index.html",
		},
		{
			name:        "applies at the root itself",
			files:       []string{"index.html"},
			request:     "/missing.html",
			wantContent: "contents of index.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := fallback{defaultPath: "index.html", fs: testFS(tt.files...)}

			f, err := fb.Open(tt.request)
			if err != nil {
				t.Fatalf("Open(%q) = %v, want no error", tt.request, err)
			}
			if got := readAll(t, f); got != tt.wantContent {
				t.Errorf("body = %q, want %q", got, tt.wantContent)
			}
		})
	}
}

// A relative fallback whose target is missing everywhere used to recurse
// forever, because path.Dir("/") is "/" and the loop had no fixed-point check.
// The stack overflow that followed was fatal and unrecoverable.
func TestFallbackRelativeMissingTerminates(t *testing.T) {
	tests := []struct {
		name    string
		request string
	}{
		{name: "nested request", request: "/a/b/missing.html"},
		{name: "root request", request: "/missing.html"},
		{name: "missing directory", request: "/nope/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := fallback{defaultPath: "index.html", fs: testFS("a/b/c.txt")}

			_, err := fb.Open(tt.request)
			if !os.IsNotExist(err) {
				t.Fatalf("Open(%q) = %v, want a not-exist error", tt.request, err)
			}
		})
	}
}

func TestFallbackAbsolute(t *testing.T) {
	fb := fallback{defaultPath: "/index.html", fs: testFS("index.html", "a/b/c.txt")}

	f, err := fb.Open("/a/b/missing.html")
	if err != nil {
		t.Fatalf("Open(/a/b/missing.html) = %v, want no error", err)
	}
	if got, want := readAll(t, f), "contents of index.html"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestFallbackAbsoluteMissing(t *testing.T) {
	fb := fallback{defaultPath: "/index.html", fs: testFS("a/b/c.txt")}

	if _, err := fb.Open("/a/b/missing.html"); !os.IsNotExist(err) {
		t.Fatalf("Open(/a/b/missing.html) = %v, want a not-exist error", err)
	}
}

func TestFallbackEmptyDefaultPath(t *testing.T) {
	fb := fallback{defaultPath: "", fs: testFS("a/b/c.txt")}

	if _, err := fb.Open("/missing.html"); err == nil {
		t.Fatal("Open(/missing.html) = nil error, want an error")
	}
}
