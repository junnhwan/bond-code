package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/memory"
	"github.com/stretchr/testify/require"
)

func TestMemoryCommandShowsIndex(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.Save(memory.MemoryFile{
		Type: memory.TypeFeedback, Name: "Short answers", Description: "Prefer short answers",
		Body: "Use short answers.",
	}))

	cmd := MemoryCommand()
	result, err := cmd.Run(context.Background(), command.Env{MemoryStore: store, MemoryMaxChars: 4000}, nil)
	require.NoError(t, err)
	require.Contains(t, result.Output, "Short answers")
}

func TestMemoryCommandListAndSearch(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.Save(memory.MemoryFile{
		Type: memory.TypeProject, Name: "Focused work", Description: "Stay focused on memory rewrite",
		Body: "Keep the memory rewrite focused.",
	}))

	cmd := MemoryCommand()
	list, err := cmd.Run(context.Background(), command.Env{MemoryStore: store}, []string{"list"})
	require.NoError(t, err)
	require.Contains(t, list.Output, "Stay focused on memory rewrite")

	search, err := cmd.Run(context.Background(), command.Env{MemoryStore: store, MemoryMaxChars: 4000}, []string{"search", "focused"})
	require.NoError(t, err)
	require.Contains(t, search.Output, "memory rewrite")
}

func TestMemoryCommandCompactRebuildsIndex(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.Save(memory.MemoryFile{
		Type: memory.TypeUser, Name: "Role", Description: "Go developer", Body: "User writes Go.",
	}))
	cmd := MemoryCommand()
	result, err := cmd.Run(context.Background(), command.Env{MemoryStore: store}, []string{"compact"})
	require.NoError(t, err)
	require.True(t, strings.Contains(result.Output, "rebuilt") || strings.Contains(result.Output, "Role"))
}

func TestMemoryCommandDir(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	require.NoError(t, err)
	cmd := MemoryCommand()
	result, err := cmd.Run(context.Background(), command.Env{MemoryStore: store}, []string{"dir"})
	require.NoError(t, err)
	require.Equal(t, store.Dir(), result.Output)
}
