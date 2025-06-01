package cache

import (
	"bytes"
	"container/list"
	"context"
	"fmt"
	"io"
	"sync"

	"imageproxy/internal/storage"
)

// LRUCache реализация LRU кэша.
type LRUCache struct {
	capacity int
	mu       sync.Mutex
	list     *list.List
	items    map[string]*list.Element
	storage  storage.Storage
}

type cacheItem struct {
	key   string
	value []byte
}

func NewLRUCache(capacity int, storage storage.Storage) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		list:     list.New(),
		items:    make(map[string]*list.Element),
		storage:  storage,
	}
}

func (c *LRUCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.list.MoveToFront(elem)
		item := elem.Value.(*cacheItem)
		return io.NopCloser(bytes.NewReader(item.value)), nil
	}

	data, err := c.storage.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer data.Close()

	value, err := io.ReadAll(data)
	if err != nil {
		return nil, err
	}

	item := &cacheItem{key: key, value: value}
	elem := c.list.PushFront(item)
	c.items[key] = elem

	if c.list.Len() > c.capacity {
		if err := c.removeOldest(ctx); err != nil {
			return nil, err
		}
	}

	return io.NopCloser(bytes.NewReader(value)), nil
}

func (c *LRUCache) Set(ctx context.Context, key string, value []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.storage.Set(ctx, key, value); err != nil {
		return err
	}

	if elem, ok := c.items[key]; ok {
		c.list.MoveToFront(elem)
		elem.Value.(*cacheItem).value = value
		return nil
	}

	item := &cacheItem{key: key, value: value}
	elem := c.list.PushFront(item)
	c.items[key] = elem

	for c.list.Len() > c.capacity {
		if err := c.removeOldest(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (c *LRUCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.list.Remove(elem)
		delete(c.items, key)
	}

	return c.storage.Delete(ctx, key)
}

func (c *LRUCache) removeOldest(ctx context.Context) error {
	elem := c.list.Back()
	if elem != nil {
		item := elem.Value.(*cacheItem)
		delete(c.items, item.key)
		c.list.Remove(elem)
		if err := c.storage.Delete(ctx, item.key); err != nil {
			return fmt.Errorf("failed to delete from storage: %w", err)
		}
	}
	return nil
}
