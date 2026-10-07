package datadir

import (
	"os"
	"path/filepath"
)

const dirName = ".gremlyn"

func Dir() (string, error) {
	dir := os.Getenv("GREMLYN_DATA_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, dirName)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
