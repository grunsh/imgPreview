package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStorage_BasicOperations(t *testing.T) {
	tempDir := t.TempDir() // Используем t.TempDir() для автоматической очистки

	ctx := context.Background()
	store, err := NewFileStorage(tempDir)
	require.NoError(t, err)

	// Test Set and Get
	data := []byte("test data")
	err = store.Set(ctx, "key1", data)
	assert.NoError(t, err)

	// Test Get
	reader, err := store.Get(ctx, "key1")
	assert.NoError(t, err)

	readData, err := io.ReadAll(reader)
	assert.NoError(t, err)
	reader.Close() // Явно закрываем reader
	assert.Equal(t, data, readData)

	// Проверяем что файл действительно создан
	filePath := filepath.Join(tempDir, store.sanitizeKey("key1"))
	_, err = os.Stat(filePath)
	assert.NoError(t, err)

	// Test Size
	assert.Equal(t, 1, store.Size())

	// Test Get non-existent key
	_, err = store.Get(ctx, "nonexistent")
	assert.ErrorIs(t, err, os.ErrNotExist)

	// Test Delete
	err = store.Delete(ctx, "key1")
	assert.NoError(t, err)
	assert.Equal(t, 0, store.Size())

	// Verify deleted
	_, err = os.Stat(filePath)
	assert.True(t, os.IsNotExist(err))
}

func TestFileStorage_SanitizeKey(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filestorage_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ctx := context.Background()
	store, err := NewFileStorage(tempDir)
	require.NoError(t, err)

	testCases := []struct {
		name     string
		key      string
		expected string
	}{
		{"With colon", "host:port/path", "host_port_path"},
		{"With slashes", "path/to/file", "path_to_file"},
		{"With backslashes", "path\\to\\file", "path_to_file"},
		{"With dots", "../parent", "__parent"},
		{"Mixed", "host:8080/path/../file", "host_8080_path__file"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte("data")
			err := store.Set(ctx, tc.key, data)
			assert.NoError(t, err)

			// Проверяем что файл создан с правильным именем
			sanitized := store.sanitizeKey(tc.key)
			filePath := filepath.Join(tempDir, sanitized)
			_, err = os.Stat(filePath)
			assert.NoError(t, err)

			// Cleanup
			_ = store.Delete(ctx, tc.key)
		})
	}
}

func TestFileStorage_ConcurrentAccess(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filestorage_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ctx := context.Background()
	store, err := NewFileStorage(tempDir)
	require.NoError(t, err)

	const numWorkers = 50
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	// Concurrent writes
	for i := 0; i < numWorkers; i++ {
		go func(i int) {
			defer wg.Done()
			key := "key_" + strconv.Itoa(i)
			data := []byte("value_" + strconv.Itoa(i))
			err := store.Set(ctx, key, data)
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Verify all writes completed
	assert.Equal(t, numWorkers, store.Size())

	// Concurrent reads
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func(i int) {
			defer wg.Done()
			key := "key_" + strconv.Itoa(i)
			reader, err := store.Get(ctx, key)
			assert.NoError(t, err)
			defer reader.Close()

			data, err := io.ReadAll(reader)
			assert.NoError(t, err)
			assert.Equal(t, "value_"+strconv.Itoa(i), string(data))
		}(i)
	}
	wg.Wait()

	// Concurrent delete
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func(i int) {
			defer wg.Done()
			key := "key_" + strconv.Itoa(i)
			err := store.Delete(ctx, key)
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 0, store.Size())
}

func TestFileStorage_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()
	store, err := NewFileStorage(tempDir)
	require.NoError(t, err)

	t.Run("Empty key", func(t *testing.T) {
		err := store.Set(ctx, "", []byte("data"))
		assert.NoError(t, err)

		reader, err := store.Get(ctx, "")
		assert.NoError(t, err)
		reader.Close()

		assert.Equal(t, 1, store.Size())

		// Cleanup
		err = store.Delete(ctx, "")
		assert.NoError(t, err)
	})

	t.Run("Nil data", func(t *testing.T) {
		err := store.Set(ctx, "nil_key", nil)
		assert.NoError(t, err)

		reader, err := store.Get(ctx, "nil_key")
		assert.NoError(t, err)
		defer reader.Close()

		data, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.Empty(t, data)
	})

	t.Run("Delete non-existent", func(t *testing.T) {
		err := store.Delete(ctx, "nonexistent")
		assert.NoError(t, err)
	})
}

func TestFileStorage_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewFileStorage(tempDir)
	require.NoError(t, err)

	// Сначала добавим данные
	ctx := context.Background()
	err = store.Set(ctx, "key", []byte("value"))
	assert.NoError(t, err)

	// Тестируем с отмененным контекстом
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("Set with canceled context", func(t *testing.T) {
		err := store.Set(ctx, "key2", []byte("value"))
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("Get with canceled context", func(t *testing.T) {
		_, err := store.Get(ctx, "key")
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("Delete with canceled context", func(t *testing.T) {
		err := store.Delete(ctx, "key")
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestNewFileStorage(t *testing.T) {
	t.Run("Create new directory", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := NewFileStorage(filepath.Join(tempDir, "subdir"))
		assert.NoError(t, err)
		assert.NotNil(t, store)
	})

	t.Run("Empty directory", func(t *testing.T) {
		_, err := NewFileStorage("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "base directory cannot be empty")
	})

	t.Run("Non-existent parent", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("Test doesn't work when running as root")
		}

		// Создаем временный каталог, чтобы получить гарантированно несуществующий путь
		tempDir := t.TempDir()
		nonExistentPath := filepath.Join(tempDir, "nonexistent", "subdir")

		_, err := NewFileStorage(nonExistentPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parent directory does not exist")
	})

	t.Run("No write permissions", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Test doesn't work reliably on Windows")
		}

		// Создаем директорию без прав на запись
		tempDir := t.TempDir()
		readOnlyDir := filepath.Join(tempDir, "readonly")
		if err := os.Mkdir(readOnlyDir, 0o555); err != nil {
			t.Fatal(err)
		}

		_, err := NewFileStorage(filepath.Join(readOnlyDir, "subdir"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no write access")
	})
}
