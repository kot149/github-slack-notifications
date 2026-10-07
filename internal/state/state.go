package state

import (
	"encoding/json"
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
	return s, json.Unmarshal(b, &s)
}

func Save(path string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
