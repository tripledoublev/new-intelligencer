package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindDotEnv_PrefersNearestAncestor(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	workspaceDir := filepath.Join(repoDir, "workspace")
	deepDir := filepath.Join(workspaceDir, "nested")

	require.NoError(t, os.MkdirAll(deepDir, 0755))

	repoEnv := filepath.Join(repoDir, ".env")
	workspaceEnv := filepath.Join(workspaceDir, ".env")
	require.NoError(t, os.WriteFile(repoEnv, []byte("BSKY_HANDLE=repo\n"), 0644))
	require.NoError(t, os.WriteFile(workspaceEnv, []byte("BSKY_HANDLE=workspace\n"), 0644))

	path, err := findDotEnv([]string{deepDir})
	require.NoError(t, err)
	assert.Equal(t, workspaceEnv, path)
}

func TestFindDotEnv_FallsBackToSecondSearchRoot(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	binDir := filepath.Join(repoDir, "bin")
	outsideDir := filepath.Join(root, "outside")

	require.NoError(t, os.MkdirAll(binDir, 0755))
	require.NoError(t, os.MkdirAll(outsideDir, 0755))

	repoEnv := filepath.Join(repoDir, ".env")
	require.NoError(t, os.WriteFile(repoEnv, []byte("BSKY_HANDLE=repo\n"), 0644))

	path, err := findDotEnv([]string{outsideDir, binDir})
	require.NoError(t, err)
	assert.Equal(t, repoEnv, path)
}

func TestApplyDotEnvFile_ParsesValuesAndPreservesExistingEnv(t *testing.T) {
	t.Setenv("BSKY_HANDLE", "from-shell")

	root := t.TempDir()
	envPath := filepath.Join(root, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte(`
BSKY_HANDLE=from-file
BSKY_PASSWORD="app password"
export OLLAMA_MODEL='qwen3.5:2b' # local override
BSKY_PDS_HOST=https://bsky.social # default host
EMPTY_VALUE=
INLINE_HASH=value#kept
INLINE_COMMENT=value # trimmed
`), 0644))

	require.NoError(t, applyDotEnvFile(envPath))

	assert.Equal(t, "from-shell", os.Getenv("BSKY_HANDLE"))
	assert.Equal(t, "app password", os.Getenv("BSKY_PASSWORD"))
	assert.Equal(t, "qwen3.5:2b", os.Getenv("OLLAMA_MODEL"))
	assert.Equal(t, "https://bsky.social", os.Getenv("BSKY_PDS_HOST"))
	assert.Equal(t, "value#kept", os.Getenv("INLINE_HASH"))
	assert.Equal(t, "value", os.Getenv("INLINE_COMMENT"))

	value, ok := os.LookupEnv("EMPTY_VALUE")
	require.True(t, ok)
	assert.Equal(t, "", value)
}

func TestApplyDotEnvFile_ReturnsLineNumberForParseErrors(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("BSKY_HANDLE=ok\nNOT_VALID\n"), 0644))

	err := applyDotEnvFile(envPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".env:2")
}
