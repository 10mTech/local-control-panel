// Package debuglog is the rotating log for background refreshes and internal
// errors. It is not part of the audit trail.
package debuglog

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

const maxBytes = 5 << 20

// Open returns a logger writing to <stateDir>/debug.log. When the file is over
// 5 MB it is moved to debug.log.1 first, so at most ~10 MB is kept.
func Open(stateDir string) (*log.Logger, io.Closer, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(stateDir, "debug.log")
	if st, err := os.Stat(path); err == nil && st.Size() > maxBytes {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return log.New(f, "", log.LstdFlags|log.Lmicroseconds), f, nil
}
