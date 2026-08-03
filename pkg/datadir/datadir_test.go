package datadir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDir_Default(t *testing.T) {
	t.Setenv("GREMLYN_DATA_DIR", "")

	dir, err := Dir()
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, ".gremlyn"), dir)
	assert.DirExists(t, dir)
}

func TestDir_EnvOverride(t *testing.T) {
	tmp := t.TempDir()
	custom := filepath.Join(tmp, "custom-gremlyn")
	t.Setenv("GREMLYN_DATA_DIR", custom)

	dir, err := Dir()
	require.NoError(t, err)

	assert.Equal(t, custom, dir)
	assert.DirExists(t, dir)
}

func TestDBPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GREMLYN_DATA_DIR", tmp)

	p, err := DBPath("shield.db")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(tmp, "shield.db"), p)
}
