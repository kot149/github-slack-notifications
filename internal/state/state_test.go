package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := State{Since: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), LastModified: "x", Seen: map[string]time.Time{"1": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Since.Equal(want.Since) || got.LastModified != want.LastModified || !got.Seen["1"].Equal(want.Seen["1"]) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file left behind: %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil || !s.Since.IsZero() {
		t.Errorf("got %+v, %v; want zero state", s, err)
	}
}

func TestLoadCorruptFileNamesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("want an error naming %s, got %v", path, err)
	}
}
