package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
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

	if os.Getenv("CI") == "true" {
		t.Skip("Skipping integration test in CI")
	}

	// 0. Проверяем доступность docker
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	// 1. Проверяем, что образ существует
	checkDockerImageExists(t)

	// 2. Останавливаем и удаляем старый контейнер, если существует
	cleanupOldContainer(t)

	// 3. Запускаем новый контейнер с nginx
	startNginxContainer(t)
	defer cleanupOldContainer(t)

	// 4. Проверяем доступность nginx
	verifyNginxIsReady(t)

	// 5. Запускаем наше приложение
	startApplication(t)

	// 6. Выполняем тестовые запросы
	testImageResizing(t)
}

func checkDockerImageExists(t *testing.T) {
	t.Helper()
	cmd := exec.Command("docker", "image", "inspect", nginxImage)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Docker image %s does not exist. Please build it first with 'docker build -t %s .'",
			nginxImage, nginxImage)
	}
}

func cleanupOldContainer(t *testing.T) {
	t.Helper()
	cmd := exec.Command("docker", "rm", "-f", nginxContainerName)
	_ = cmd.Run() // Игнорируем ошибку, если контейнера нет
}

func startNginxContainer(t *testing.T) {
	t.Helper()
	t.Log("Starting nginx container...")

	//nolint:gosec
	cmd := exec.Command("docker", "run",
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
		//nolint:noctx
		resp, err := client.Get(url)
		if err != nil {
			t.Logf("Nginx not ready yet: %v", err)
			return false
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Logf("Unexpected status code: %d", resp.StatusCode)
			return false
		}

		// Проверяем, что это действительно изображение
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

	// Ждем пока приложение станет доступно
	client := http.Client{Timeout: 1 * time.Second}
	require.Eventually(t, func() bool {
		//nolint:noctx
		resp, err := client.Get(fmt.Sprintf("http://localhost:%s/health", appPort))
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
		url := fmt.Sprintf("http://localhost:%s/fill/300/200/localhost:%s/images/%s",
			appPort, nginxPort, testImageName)
		//nolint:noctx,gosec
		resp, err := http.Get(url)
		require.NoError(t, err, "Request failed")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Unexpected status code: %d, body: %s", resp.StatusCode, string(body))
		}

		require.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"), "Unexpected content type")

		imgData, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "Failed to read response body")
		require.True(t, len(imgData) > 0, "Empty image received")
		require.True(t, bytes.HasPrefix(imgData, []byte{0xFF, 0xD8, 0xFF}), "Invalid JPEG format")
	})
}
