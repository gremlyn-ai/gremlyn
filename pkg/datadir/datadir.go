// Package datadir manages the Gremlyn data directory (~/.gremlyn/).
// It provides helpers for locating and creating the shared data directory
// used by all Gremlyn services (Shield, Arena) for SQLite databases,
// config caches, and other persistent data.
package datadir

import (
	"os"
	"path/filepath"
)

const dirName = ".gremlyn"

// Dir returns the Gremlyn data directory path, creating it if necessary.
// The directory defaults to ~/.gremlyn/ but can be overridden via
// the GREMLYN_DATA_DIR environment variable.
func Dir() (string, error) {
	dir := os.Getenv("GREMLYN_DATA_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, dirName)
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// DBPath returns the full path for a database file inside the data directory.
// It ensures the data directory exists before returning.
func DBPath(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}
