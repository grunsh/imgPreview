package storage

import (
	"context"
	"io"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemoryStorage_BasicOperations(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()

	// Test Set and Get
	data := []byte("test data")
	err := store.Set(ctx, "key1", data)
	assert.NoError(t, err)

	// Test Get
	reader, err := store.Get(ctx, "key1")
	assert.NoError(t, err)
	defer reader.Close()

	readData, err := io.ReadAll(reader)
	assert.NoError(t, err)
	assert.Equal(t, data, readData)

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
	_, err = store.Get(ctx, "key1")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestMemoryStorage_Concurrency(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()
	const numWorkers = 100

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

func TestMemoryStorage_EdgeCases(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()

	// Test empty key
	err := store.Set(ctx, "", []byte("data"))
	assert.NoError(t, err)

	// Test nil data
	err = store.Set(ctx, "nil_key", nil)
	assert.NoError(t, err)

	reader, err := store.Get(ctx, "nil_key")
	assert.NoError(t, err)
	defer reader.Close()

	data, err := io.ReadAll(reader)
	assert.NoError(t, err)
	assert.Empty(t, data)

	// Test double delete
	err = store.Delete(ctx, "nonexistent")
	assert.NoError(t, err)
}

func TestMemoryStorage_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := NewMemoryStorage()

	// Сначала добавим данные
	err := store.Set(ctx, "key", []byte("value"))
	assert.NoError(t, err)

	// Отменяем контекст
	cancel()

	// Проверяем операции с отмененным контекстом
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

func TestMemoryStorage_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	store := NewMemoryStorage()

	_, err := store.Get(ctx, "key")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
