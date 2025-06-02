package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockStorage правильная реализация Storage для тестов.
type MockStorage struct {
	mock.Mock
}

func (m *MockStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return io.NopCloser(bytes.NewReader(args.Get(0).([]byte))), args.Error(1)
}

func (m *MockStorage) Set(ctx context.Context, key string, data []byte) error {
	args := m.Called(ctx, key, data)
	return args.Error(0)
}

func (m *MockStorage) Delete(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func (m *MockStorage) Size() int {
	args := m.Called()
	return args.Int(0)
}

func TestLRUCache_Eviction(t *testing.T) {
	ctx := context.Background()
	mockStorage := new(MockStorage)
	cache := NewLRUCache(2, mockStorage)

	// Настройка моков
	mockStorage.On("Set", ctx, "key1", []byte("value1")).Return(nil)
	mockStorage.On("Set", ctx, "key2", []byte("value2")).Return(nil)
	mockStorage.On("Set", ctx, "key3", []byte("value3")).Return(nil)
	mockStorage.On("Delete", ctx, "key1").Return(nil)
	mockStorage.On("Size").Return(0)

	// Заполнение кэша
	assert.NoError(t, cache.Set(ctx, "key1", []byte("value1")))
	assert.NoError(t, cache.Set(ctx, "key2", []byte("value2")))

	// Добавление элемента, который должен вытеснить key1
	assert.NoError(t, cache.Set(ctx, "key3", []byte("value3")))

	// Проверка вызовов
	mockStorage.AssertCalled(t, "Delete", ctx, "key1")
}

func TestLRUCache_GetUpdatesLRU(t *testing.T) {
	ctx := context.Background()
	mockStorage := new(MockStorage)
	cache := NewLRUCache(2, mockStorage)

	// Настройка моков
	mockStorage.On("Set", ctx, "key1", []byte("value1")).Return(nil)
	mockStorage.On("Set", ctx, "key2", []byte("value2")).Return(nil)
	mockStorage.On("Set", ctx, "key3", []byte("value3")).Return(nil)
	mockStorage.On("Delete", ctx, "key2").Return(nil)
	mockStorage.On("Get", ctx, "key1").Return([]byte("value1"), nil)
	mockStorage.On("Size").Return(0)

	// Заполнение кэша
	assert.NoError(t, cache.Set(ctx, "key1", []byte("value1")))
	assert.NoError(t, cache.Set(ctx, "key2", []byte("value2")))

	// Обновление LRU для key1
	_, err := cache.Get(ctx, "key1")
	assert.NoError(t, err)

	// Добавление нового элемента (должен вытеснить key2)
	assert.NoError(t, cache.Set(ctx, "key3", []byte("value3")))

	mockStorage.AssertCalled(t, "Delete", ctx, "key2")
}

func TestLRUCache_ErrorHandling(t *testing.T) {
	ctx := context.Background()
	mockStorage := new(MockStorage)
	cache := NewLRUCache(1, mockStorage)

	storageError := errors.New("storage error")
	mockStorage.On("Set", ctx, "key1", []byte("value1")).Return(nil)
	mockStorage.On("Set", ctx, "key2", []byte("value2")).Return(nil)
	mockStorage.On("Delete", ctx, "key1").Return(storageError)
	mockStorage.On("Size").Return(0)

	assert.NoError(t, cache.Set(ctx, "key1", []byte("value1")))
	err := cache.Set(ctx, "key2", []byte("value2"))
	assert.EqualError(t, err, "failed to delete from storage: storage error")
}

func TestLRUCache_EdgeCases(t *testing.T) {
	ctx := context.Background()
	mockStorage := new(MockStorage)
	cache := NewLRUCache(0, mockStorage)

	mockStorage.On("Set", ctx, "key1", []byte("value1")).Return(nil)
	mockStorage.On("Delete", ctx, "key1").Return(nil)
	mockStorage.On("Size").Return(0)

	assert.NoError(t, cache.Set(ctx, "key1", []byte("value1")))
	mockStorage.AssertCalled(t, "Delete", ctx, "key1")
}
