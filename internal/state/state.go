package state

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type State struct {
	// Since is the server time of the last successful fetch; the next fetch asks for notifications updated after it.
	Since        time.Time `json:"since"`
	LastModified string    `json:"last_modified,omitempty"`
	// Seen maps thread IDs to the updated_at already handled, so the overlapping fetch window doesn't resend them.
	Seen map[string]time.Time `json:"seen,omitempty"`
}

func Load(path string) (State, error) {
	var s State
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w (delete the file to start over)", path, err)
	}
	return s, nil
}

// Save replaces the file at path atomically, syncing first so a crash can't leave it empty.
func Save(path string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
