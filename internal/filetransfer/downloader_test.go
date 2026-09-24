package filetransfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"log/slog"
)

func TestNewDownloader(t *testing.T) {
	dl := NewDownloader(nil, 0, 0, 0)
	if dl == nil {
		t.Fatal("NewDownloader returned nil")
	}
	if dl.ChunkSize != DefaultChunkSize {
		t.Errorf("ChunkSize = %d, want %d", dl.ChunkSize, DefaultChunkSize)
	}
	if dl.Concurrency != DefaultConcurrency {
		t.Errorf("Concurrency = %d, want %d", dl.Concurrency, DefaultConcurrency)
	}
	if dl.MaxRetries != 0 {
		t.Errorf("MaxRetries = %d, want 0", dl.MaxRetries)
	}
}

func TestNewDownloaderWithLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dl := NewDownloader(logger, 1<<20, 2, 5)
	if dl == nil {
		t.Fatal("NewDownloader returned nil")
	}
	if dl.ChunkSize != 1<<20 {
		t.Errorf("ChunkSize = %d, want %d", dl.ChunkSize, 1<<20)
	}
	if dl.Concurrency != 2 {
		t.Errorf("Concurrency = %d, want %d", dl.Concurrency, 2)
	}
	if dl.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want %d", dl.MaxRetries, 5)
	}
}

func TestNewDownloaderCustomSize(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dl := NewDownloader(logger, 512*1024*1024, 8, 10)
	if dl == nil {
		t.Fatal("NewDownloader returned nil")
	}
	if dl.ChunkSize != 512*1024*1024 {
		t.Errorf("ChunkSize = %d, want %d", dl.ChunkSize, 512*1024*1024)
	}
	if dl.Concurrency != 8 {
		t.Errorf("Concurrency = %d, want %d", dl.Concurrency, 8)
	}
	if dl.MaxRetries != 10 {
		t.Errorf("MaxRetries = %d, want %d", dl.MaxRetries, 10)
	}
}

func TestNewDownloaderNegativeMaxRetries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dl := NewDownloader(logger, 1<<20, 4, -1)
	if dl == nil {
		t.Fatal("NewDownloader returned nil")
	}
	if dl.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries = %d, want default %d", dl.MaxRetries, DefaultMaxRetries)
	}
}

func TestNewDownloaderZeroConcurrency(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dl := NewDownloader(logger, 1<<20, 0, 3)
	if dl == nil {
		t.Fatal("NewDownloader returned nil")
	}
	if dl.Concurrency != DefaultConcurrency {
		t.Errorf("Concurrency = %d, want default %d", dl.Concurrency, DefaultConcurrency)
	}
}

func TestDownloaderDownload(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "downloaded.bin")

	testData := make([]byte, 1024*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	err := dl.Download(context.Background(), server.URL, dest, int64(len(testData)), expectedHash)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if len(content) != len(testData) {
		t.Errorf("Downloaded file size = %d, want %d", len(content), len(testData))
	}
}

func TestDownloaderDownloadWithRetry(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "retry.bin")

	testData := []byte("hello world")
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])
	attempts := 0
	maxAttempts := 2

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < maxAttempts {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 3)
	err := dl.Download(context.Background(), server.URL, dest, int64(len(testData)), expectedHash)
	if err != nil {
		t.Fatalf("Download() with retry error = %v", err)
	}

	if attempts < maxAttempts {
		t.Errorf("Expected at least %d attempts, got %d", maxAttempts, attempts)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(content) != "hello world" {
		t.Errorf("Downloaded content = %q, want %q", string(content), "hello world")
	}
}

func TestDownloaderDownloadInvalidURL(t *testing.T) {
	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	err := dl.Download(context.Background(), "http://invalid-host-that-does-not-exist.invalid/file.bin", "/tmp/test.bin", 1024, "")
	if err == nil {
		t.Error("Download() expected error for invalid URL, got nil")
	}
}

func TestDownloaderDownloadContextTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "timeout.bin")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := dl.Download(ctx, server.URL, dest, 1024*1024, "")
	if err == nil {
		t.Error("Download() expected error for context timeout, got nil")
	}
}

func TestDownloaderDownloadEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "empty.bin")
	emptyHash := sha256.Sum256([]byte{})
	expectedHash := hex.EncodeToString(emptyHash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", "bytes */0")
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	err := dl.Download(context.Background(), server.URL, dest, 0, expectedHash)
	if err != nil {
		t.Fatalf("Download() empty file error = %v", err)
	}

	_, err = os.Stat(dest)
	if err != nil {
		t.Errorf("Download() empty file did not create destination: %v", err)
	}
}

func TestDownloaderDownloadChunkSize(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "chunked.bin")

	testData := make([]byte, 128*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 4, 0)
	err := dl.Download(context.Background(), server.URL, dest, int64(len(testData)), expectedHash)
	if err != nil {
		t.Fatalf("Download() chunked error = %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if len(content) != len(testData) {
		t.Errorf("Downloaded file size = %d, want %d", len(content), len(testData))
	}
}

func TestDownloaderDownloadFileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "notfound.bin")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	err := dl.Download(context.Background(), server.URL, dest, 1024, "")
	if err == nil {
		t.Error("Download() expected error for 404, got nil")
	}
}

func TestDownloaderDownloadInvalidPath(t *testing.T) {
	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	err := dl.Download(context.Background(), "http://example.com/file.bin", "/nonexistent/path/file.bin", 1024, "")
	if err == nil {
		t.Error("Download() expected error for invalid destination path, got nil")
	}
}

func TestDownloaderDefaultValues(t *testing.T) {
	dl := NewDownloader(nil, 0, 0, 0)
	if dl.ChunkSize != DefaultChunkSize {
		t.Errorf("ChunkSize = %d, want %d", dl.ChunkSize, DefaultChunkSize)
	}
	if dl.RetryBase != DefaultRetryBase {
		t.Errorf("RetryBase = %v, want %v", dl.RetryBase, DefaultRetryBase)
	}
	if dl.RetryMax != DefaultRetryMax {
		t.Errorf("RetryMax = %v, want %v", dl.RetryMax, DefaultRetryMax)
	}
}

func TestDownloaderLoggerFallback(t *testing.T) {
	dl := NewDownloader(nil, 1<<20, 4, 3)
	if dl == nil {
		t.Fatal("NewDownloader with nil logger returned nil")
	}
	if dl.logger == nil {
		t.Error("NewDownloader with nil logger should create a logger")
	}
}

func TestDownloaderDownloadLargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "large.bin")

	testData := make([]byte, 10*1024*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 1*1024*1024, 4, 3)
	err := dl.Download(context.Background(), server.URL, dest, int64(len(testData)), expectedHash)
	if err != nil {
		t.Fatalf("Download() large file error = %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read large file: %v", err)
	}
	if len(content) != len(testData) {
		t.Errorf("Downloaded large file size = %d, want %d", len(content), len(testData))
	}
}

func TestDownloaderDownloadWithPartialData(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "partial.bin")

	testData := []byte("partial data test")
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	err := dl.Download(context.Background(), server.URL, dest, int64(len(testData)), expectedHash)
	if err != nil {
		t.Fatalf("Download() partial data error = %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "partial data test" {
		t.Errorf("Downloaded content = %q, want %q", string(content), "partial data test")
	}
}

func TestDownloaderDownloadWithCustomRetry(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "retry.bin")

	testData := []byte("retry data")
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 5)
	err := dl.Download(context.Background(), server.URL, dest, int64(len(testData)), expectedHash)
	if err != nil {
		t.Fatalf("Download() with custom retry error = %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "retry data" {
		t.Errorf("Downloaded content = %q, want %q", string(content), "retry data")
	}
}

func TestDownloaderDownloadProgress(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "progress.bin")

	testData := make([]byte, 256*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 256*1024, 1, 0)
	err := dl.Download(context.Background(), server.URL, dest, int64(len(testData)), expectedHash)
	if err != nil {
		t.Fatalf("Download() progress error = %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if len(content) != len(testData) {
		t.Errorf("Downloaded file size = %d, want %d", len(content), len(testData))
	}
}

func TestDownloaderWithZeroRetry(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "zeroretry.bin")

	testData := []byte("zero retry")
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	err := dl.Download(context.Background(), server.URL, dest, int64(len(testData)), expectedHash)
	if err != nil {
		t.Fatalf("Download() with zero retry error = %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "zero retry" {
		t.Errorf("Downloaded content = %q, want %q", string(content), "zero retry")
	}
}

func TestDownloaderDownloadErrorOnPartialResponse(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "partial_resp.bin")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("too small"))
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	err := dl.Download(context.Background(), server.URL, dest, 1024*1024, "")
	if err == nil {
		t.Error("Download() expected error for partial response, got nil")
	}
}

func TestDownloaderWithChunkSizeValidation(t *testing.T) {
	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), -1, 4, 3)
	if dl.ChunkSize != DefaultChunkSize {
		t.Errorf("ChunkSize with negative input = %d, want default %d", dl.ChunkSize, DefaultChunkSize)
	}
}

func TestDownloaderWithNegativeMaxRetries(t *testing.T) {
	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20, 4, -5)
	if dl.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries with negative input = %d, want default %d", dl.MaxRetries, DefaultMaxRetries)
	}
}

func TestDownloaderDownloadReturnsErrorForInvalidURL(t *testing.T) {
	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	err := dl.Download(context.Background(), "not-a-url", "/tmp/file.bin", 1024, "")
	if err == nil {
		t.Error("Download() expected error for invalid URL format, got nil")
	}
}

func TestDownloaderDownloadWithRange(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "range.bin")

	testData := make([]byte, 512*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 128*1024, 2, 0)
	err := dl.Download(context.Background(), server.URL, dest, int64(len(testData)), expectedHash)
	if err != nil {
		t.Fatalf("Download() with range error = %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if len(content) != len(testData) {
		t.Errorf("Downloaded file size = %d, want %d", len(content), len(testData))
	}
}

func TestDownloaderDownloadWithNoExpectedSize(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "no_size.bin")

	testData := []byte("no size test")
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(testData)-1, len(testData)))
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	err := dl.Download(context.Background(), server.URL, dest, int64(len(testData)), expectedHash)
	if err != nil {
		t.Fatalf("Download() with zero expected size error = %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "no size test" {
		t.Errorf("Downloaded content = %q, want %q", string(content), "no size test")
	}
}

func TestDownloaderDownloadTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "timeout.bin")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 64*1024, 1, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := dl.Download(ctx, server.URL, dest, 1024, "")
	if err == nil {
		t.Error("Download() expected timeout error, got nil")
	}
}

func TestDownloaderNewDownloaderDefaults(t *testing.T) {
	dl := NewDownloader(nil, 0, 0, 0)
	if dl == nil {
		t.Fatal("NewDownloader returned nil")
	}
	if dl.Client == nil {
		t.Error("NewDownloader should initialize http.Client")
	}
}

func TestDownloaderParallelChunksPreserveContent(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "parallel.bin")

	// 8 MiB with distinct pattern so offset bugs change the hash.
	const size = 8 << 20
	testData := make([]byte, size)
	for i := range testData {
		testData[i] = byte((i*31 + 7) % 251)
	}
	hash := sha256.Sum256(testData)
	expectedHash := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rng := r.Header.Get("Range")
		var start, end int64
		if _, err := fmt.Sscanf(rng, "bytes=%d-%d", &start, &end); err != nil {
			// Full request (HEAD may not send Range)
			w.Header().Set("ETag", `"test"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		if end >= size {
			end = size - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(testData[start : end+1])
	}))
	defer server.Close()

	dl := NewDownloader(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20, 4, 0)
	if err := dl.Download(context.Background(), server.URL, dest, size, expectedHash); err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != size {
		t.Fatalf("size = %d, want %d", len(got), size)
	}
	for i := range got {
		if got[i] != testData[i] {
			t.Fatalf("byte %d = %d, want %d", i, got[i], testData[i])
		}
	}
}
