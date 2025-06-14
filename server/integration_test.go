package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	nginxContainerName = "test-nginx"
	nginxImage         = "my-nginx-image"
	nginxPort          = "8082"
	appPort            = "8081"
	testImageName      = "003.jpg"
)

func TestIntegration(t *testing.T) {
	if os.Getenv("CI") == "true" || os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Skip("Skipping integration test in CI")
	}

	// Проверяем доступность docker
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	// Проверяем, что образ существует
	checkDockerImageExists(t)

	// Останавливаем и удаляем старый контейнер, если существует
	cleanupOldContainer(t)

	// Запускаем новый контейнер с nginx
	startNginxContainer(t)
	defer cleanupOldContainer(t)

	// Проверяем доступность nginx
	verifyNginxIsReady(t)

	// Запускаем наше приложение
	startApplication(t)

	// Выполняем тестовые запросы
	testImageResizing(t)
	testCacheHit(t)
	testRemoteServerNotFound(t)
	testRemoteImageNotFound(t)
	testInvalidImageContent(t)
	testRemoteServerError(t)
	testSmallImageResizing(t)
	testHeaderForwarding(t)
}

func checkDockerImageExists(t *testing.T) {
	t.Helper()
	cmd := exec.Command("docker", "image", "inspect", nginxImage) // #nosec G204
	if err := cmd.Run(); err != nil {
		t.Fatalf("Docker image %s does not exist. Please build it first with 'docker build -t %s .'",
			nginxImage, nginxImage)
	}
}

func cleanupOldContainer(t *testing.T) {
	t.Helper()
	cmd := exec.Command("docker", "rm", "-f", nginxContainerName) // #nosec G204
	_ = cmd.Run()
}

func startNginxContainer(t *testing.T) {
	t.Helper()
	t.Log("Starting nginx container...")

	cmd := exec.Command("docker", "run", // #nosec G204
		"--name", nginxContainerName,
		"-d",
		"-p", fmt.Sprintf("%s:80", nginxPort),
		nginxImage)
	out, err := cmd.CombinedOutput()
	t.Logf("Docker run output: %s", string(out))
	require.NoError(t, err, "Failed to start nginx container")
}

func verifyNginxIsReady(t *testing.T) {
	t.Helper()
	t.Log("Waiting for nginx to become ready...")
	client := http.Client{Timeout: 1 * time.Second}
	url := fmt.Sprintf("http://localhost:%s/images/%s", nginxPort, testImageName)

	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			t.Logf("Failed to create request: %v", err)
			return false
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Logf("Nginx not ready yet: %v", err)
			return false
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Logf("Unexpected status code: %d", resp.StatusCode)
			return false
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Logf("Failed to read response: %v", err)
			return false
		}

		if !bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
			t.Log("Response is not a JPEG image")
			return false
		}

		return true
	}, 30*time.Second, 500*time.Millisecond, "Nginx did not become ready")
}

func startApplication(t *testing.T) {
	t.Helper()
	t.Log("Starting application...")
	os.Setenv("PORT", appPort)
	os.Setenv("STORAGE_TYPE", "memory")
	go main()

	client := http.Client{Timeout: 1 * time.Second}
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://localhost:%s/health", appPort), nil)
		if err != nil {
			t.Logf("Failed to create request: %v", err)
			return false
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Logf("Application not ready yet: %v", err)
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 100*time.Millisecond, "Application did not start")
}

func testImageResizing(t *testing.T) {
	t.Helper()
	t.Run("Resize image from nginx", func(t *testing.T) {
		client := http.Client{Timeout: 5 * time.Second}
		url := fmt.Sprintf("http://localhost:%s/fill/300/200/localhost:%s/images/%s",
			appPort, nginxPort, testImageName)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		require.NoError(t, err, "Failed to create request")

		resp, err := client.Do(req)
		require.NoError(t, err, "Request failed")
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "Unexpected status code")
		require.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"), "Unexpected content type")

		imgData, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "Failed to read response body")
		require.True(t, len(imgData) > 0, "Empty image received")
		require.True(t, bytes.HasPrefix(imgData, []byte{0xFF, 0xD8, 0xFF}), "Invalid JPEG format")
	})
}

func testCacheHit(t *testing.T) {
	t.Helper()
	t.Run("Image found in cache", func(t *testing.T) {
		client := http.Client{Timeout: 5 * time.Second}
		url := fmt.Sprintf("http://localhost:%s/fill/300/200/localhost:%s/images/%s",
			appPort, nginxPort, testImageName)

		// Первый запрос - должен загрузить в кэш
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		require.NoError(t, err, "Failed to create first request")

		resp, err := client.Do(req)
		require.NoError(t, err, "First request failed")
		resp.Body.Close()

		// Второй запрос - должен использовать кэш
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()

		req2, err := http.NewRequestWithContext(ctx2, "GET", url, nil)
		require.NoError(t, err, "Failed to create second request")

		resp, err = client.Do(req2)
		require.NoError(t, err, "Second request failed")
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "Unexpected status code")
		require.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"), "Unexpected content type")

		imgData, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "Failed to read response body")
		require.True(t, len(imgData) > 0, "Empty image received")
		require.True(t, bytes.HasPrefix(imgData, []byte{0xFF, 0xD8, 0xFF}), "Invalid JPEG format")
	})
}

func testRemoteServerNotFound(t *testing.T) {
	t.Helper()
	t.Run("Remote server not found", func(t *testing.T) {
		client := http.Client{Timeout: 5 * time.Second}
		url := fmt.Sprintf("http://localhost:%s/fill/300/200/nonexistentserver/images/%s",
			appPort, testImageName)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		require.NoError(t, err, "Failed to create request")

		resp, err := client.Do(req)
		require.NoError(t, err, "Request failed")
		defer resp.Body.Close()

		require.Equal(t, http.StatusInternalServerError, resp.StatusCode, "Unexpected status code")
		body, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(body), "failed to download image", "Unexpected error message")
	})
}

func testRemoteImageNotFound(t *testing.T) {
	t.Helper()
	t.Run("Remote image not found (404)", func(t *testing.T) {
		// Создаем тестовый HTTP сервер, который возвращает 404
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := http.Client{Timeout: 5 * time.Second}
		url := fmt.Sprintf("http://localhost:%s/fill/300/200/%s/nonexistent.jpg",
			appPort, server.URL[len("http://"):])

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		require.NoError(t, err, "Failed to create request")

		resp, err := client.Do(req)
		require.NoError(t, err, "Request failed")
		defer resp.Body.Close()

		require.Equal(t, http.StatusInternalServerError, resp.StatusCode, "Unexpected status code")
		body, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(body), "server returned status: 404", "Unexpected error message")
	})
}

func testInvalidImageContent(t *testing.T) {
	t.Helper()
	t.Run("Remote server returns non-image content", func(t *testing.T) {
		// Создаем тестовый HTTP сервер, который возвращает exe-файл
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			if _, err := w.Write([]byte("MZ")); err != nil {
				t.Errorf("Failed to write response: %v", err)
				return
			}
		}))
		defer server.Close()

		client := http.Client{Timeout: 5 * time.Second}
		url := fmt.Sprintf("http://localhost:%s/fill/300/200/%s/fake.exe",
			appPort, server.URL[len("http://"):])

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		require.NoError(t, err, "Failed to create request")

		resp, err := client.Do(req)
		require.NoError(t, err, "Request failed")
		defer resp.Body.Close()

		require.Equal(t, http.StatusInternalServerError, resp.StatusCode, "Unexpected status code")
		body, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(body), "failed to decode image", "Unexpected error message")
	})
}

func testRemoteServerError(t *testing.T) {
	t.Helper()
	t.Run("Remote server returns error", func(t *testing.T) {
		// Создаем тестовый HTTP сервер, который возвращает 500
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := http.Client{Timeout: 5 * time.Second}
		url := fmt.Sprintf("http://localhost:%s/fill/300/200/%s/images/%s",
			appPort, server.URL[len("http://"):], testImageName)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		require.NoError(t, err, "Failed to create request")

		resp, err := client.Do(req)
		require.NoError(t, err, "Request failed")
		defer resp.Body.Close()

		require.Equal(t, http.StatusInternalServerError, resp.StatusCode, "Unexpected status code")
		body, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(body), "server returned status: 500", "Unexpected error message")
	})
}

func testSmallImageResizing(t *testing.T) {
	t.Helper()
	t.Run("Image smaller than requested size", func(t *testing.T) {
		// Создаем тестовый HTTP сервер с маленьким изображением (10x10)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Создаем маленькое изображение 10x10
			img := image.NewRGBA(image.Rect(0, 0, 10, 10))
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, nil); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(buf.Bytes())
		}))
		defer server.Close()

		client := http.Client{Timeout: 5 * time.Second}
		// Запрашиваем размер больше, чем оригинал (300x200)
		url := fmt.Sprintf("http://localhost:%s/fill/300/200/%s/small.jpg",
			appPort, server.URL[len("http://"):])

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		require.NoError(t, err, "Failed to create request")

		resp, err := client.Do(req)
		require.NoError(t, err, "Request failed")
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "Unexpected status code")
		require.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"), "Unexpected content type")

		imgData, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "Failed to read response body")
		require.True(t, len(imgData) > 0, "Empty image received")
		require.True(t, bytes.HasPrefix(imgData, []byte{0xFF, 0xD8, 0xFF}), "Invalid JPEG format")
	})
}

func testHeaderForwarding(t *testing.T) {
	t.Helper()
	t.Run("HTTP headers forwarding", func(t *testing.T) {
		// Создаем тестовый HTTP сервер с кастомными заголовками
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Custom-Header", "test-value")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")

			// Создаем тестовое изображение
			img := image.NewRGBA(image.Rect(0, 0, 10, 10))
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, nil); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			if _, err := w.Write(buf.Bytes()); err != nil {
				t.Errorf("Failed to write response: %v", err)
				return
			}
		}))
		defer server.Close()

		client := http.Client{Timeout: 5 * time.Second}
		url := fmt.Sprintf("http://localhost:%s/fill/300/200/%s/test.jpg",
			appPort, server.URL[len("http://"):])

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		require.NoError(t, err, "Failed to create request")

		resp, err := client.Do(req)
		require.NoError(t, err, "Request failed")
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "Unexpected status code")
		require.Equal(t, "test-value", resp.Header.Get("X-Custom-Header"), "Custom header not forwarded")
		require.Equal(t, "public, max-age=3600", resp.Header.Get("Cache-Control"), "Cache-Control header not forwarded")
		require.Equal(t, "Wed, 21 Oct 2015 07:28:00 GMT", resp.Header.Get("Last-Modified"),
			"Last-Modified header not forwarded")
	})
}
