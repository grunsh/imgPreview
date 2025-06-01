package processor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"time"

	"github.com/disintegration/imaging"
	"imageproxy/internal/cache"
)

// ImageProcessor обработчик изображений.
type ImageProcessor struct {
	cache  *cache.LRUCache
	client *http.Client
}

func NewImageProcessor(cache *cache.LRUCache) *ImageProcessor {
	return &ImageProcessor{
		cache:  cache,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *ImageProcessor) GetOriginalImage(ctx context.Context, url string) (image.Image, error) {
	// Ключ кэша - только URL без размеров
	cacheKey := url

	// Пытаемся получить из кэша
	cachedData, err := p.cache.Get(ctx, cacheKey)
	if err == nil {
		defer cachedData.Close()
		img, _, err := image.Decode(cachedData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode cached image: %w", err)
		}
		return img, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to get from cache: %w", err)
	}

	// Если в кэше нет, скачиваем изображение
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	// Декодируем изображение
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// Сохраняем оригинал в кэш
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG); err != nil {
		return nil, fmt.Errorf("failed to encode image for cache: %w", err)
	}

	if err := p.cache.Set(ctx, cacheKey, buf.Bytes()); err != nil {
		return nil, fmt.Errorf("failed to cache image: %w", err)
	}

	return img, nil
}

func (p *ImageProcessor) ProcessImage(ctx context.Context, url string, width, height int) ([]byte, string, error) {
	// Получаем оригинальное изображение (из кэша или скачиваем)
	img, err := p.GetOriginalImage(ctx, url)
	if err != nil {
		return nil, "", err
	}

	// Масштабируем изображение с использованием библиотеки imaging
	resizedImg := imaging.Resize(img, width, height, imaging.Lanczos)

	// Кодируем в JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resizedImg, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", fmt.Errorf("failed to encode image: %w", err)
	}

	return buf.Bytes(), "image/jpeg", nil
}
