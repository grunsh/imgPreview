package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type FileStorage struct {
	baseDir string
	mu      sync.RWMutex
	size    int
}

func NewFileStorage(baseDir string) (*FileStorage, error) {
	if baseDir == "" {
		return nil, errors.New("base directory cannot be empty")
	}

	//nolint:gocritic
	//// Проверяем, что путь абсолютный
	//if !filepath.IsAbs(baseDir) {
	//	return nil, errors.New("base directory path must be absolute")
	//}

	// Проверяем, что родительская директория существует и доступна для записи
	parentDir := filepath.Dir(baseDir)
	if _, err := os.Stat(parentDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("parent directory does not exist: %w", err)
	}

	// Пытаемся создать тестовый файл в родительской директории
	testFile := filepath.Join(parentDir, "test_write_access")
	if err := os.WriteFile(testFile, []byte("test"), 0o600); err != nil {
		return nil, fmt.Errorf("no write access to parent directory: %w", err)
	}
	os.Remove(testFile)

	// Создаем целевую директорию
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	// Проверяем что мы можем писать в целевую директорию
	testFile = filepath.Join(baseDir, "test_write_access")
	if err := os.WriteFile(testFile, []byte("test"), 0o600); err != nil {
		return nil, fmt.Errorf("no write access to base directory: %w", err)
	}
	os.Remove(testFile)

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read base directory: %w", err)
	}

	return &FileStorage{
		baseDir: baseDir,
		size:    len(entries),
	}, nil
}

func (s *FileStorage) sanitizeKey(key string) string {
	if key == "" {
		return "empty"
	}
	key = strings.ReplaceAll(key, ":", "_")
	key = strings.ReplaceAll(key, "/", "_")
	key = strings.ReplaceAll(key, "\\", "_")
	key = strings.ReplaceAll(key, "..", "__")
	return strings.TrimLeft(key, "_")
}

func (s *FileStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	safeKey := s.sanitizeKey(key)
	path := filepath.Join(s.baseDir, safeKey)

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

func (s *FileStorage) Set(ctx context.Context, key string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	safeKey := s.sanitizeKey(key)
	path := filepath.Join(s.baseDir, safeKey)

	// Проверяем существует ли файл
	_, err := os.Stat(path)
	exists := !os.IsNotExist(err)

	// Создаем все необходимые директории
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Записываем файл
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Обновляем счетчик
	if !exists {
		s.size++
	}

	return nil
}

func (s *FileStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	safeKey := s.sanitizeKey(key)
	path := filepath.Join(s.baseDir, safeKey)

	// Проверяем существует ли файл
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	// Удаляем файл
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	s.size--
	return nil
}

func (s *FileStorage) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.size
}
