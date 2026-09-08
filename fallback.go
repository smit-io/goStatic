package main

import (
	"net/http"
	"os"
	"path"
)

// fallback opens defaultPath when the underlying fs returns os.ErrNotExist
type fallback struct {
	defaultPath string
	fs          http.FileSystem
}

// OpenDefault looks for fb.defaultPath in the directory holding requestPath,
// then in each parent directory, stopping once the root has been checked.
func OpenDefault(fb fallback, requestPath string) (http.File, error) {
	dir := path.Dir(requestPath)

	for {
		f, err := fb.fs.Open(path.Join(dir, fb.defaultPath))
		if !os.IsNotExist(err) {
			return f, err
		}

		// path.Dir is its own fixed point at the root ("/" and "."), so
		// compare against it rather than looping forever.
		parent := path.Dir(dir)
		if parent == dir {
			return f, err
		}
		dir = parent
	}
}

func (fb fallback) Open(requestPath string) (http.File, error) {
	f, err := fb.fs.Open(requestPath)
	if os.IsNotExist(err) {
		if len(fb.defaultPath) == 0 || fb.defaultPath[0] == '/' {
			return fb.fs.Open(fb.defaultPath)
		}
		return OpenDefault(fb, requestPath)
	}
	return f, err
}
