package filetransfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mmk31585/updater-service/internal/hash"
	"github.com/mmk31585/updater-service/internal/logging"
)

const (
	DefaultChunkSize   int64         = 64 * 1024 * 1024
	DefaultConcurrency int           = 4
	DefaultMaxRetries  int           = 3
	DefaultRetryBase   time.Duration = 1 * time.Second
	DefaultRetryMax    time.Duration = 60 * time.Second
)

type Downloader struct {
	Client      *http.Client
	ChunkSize   int64
	Concurrency int
	MaxRetries  int
	RetryBase   time.Duration
	RetryMax    time.Duration
	logger      *slog.Logger
	stateMu     sync.Mutex
}

func NewDownloader(logger *slog.Logger, chunkSize int64, concurrency int, maxRetries int) *Downloader {
	if logger == nil {
		logger = logging.NewLogger("file-transfer", "development", "unknown")
	}

	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	if maxRetries < 0 {
		maxRetries = DefaultMaxRetries
	}
	return &Downloader{
		Client:      &http.Client{},
		ChunkSize:   chunkSize,
		Concurrency: concurrency,
		MaxRetries:  maxRetries,
		RetryBase:   DefaultRetryBase,
		RetryMax:    DefaultRetryMax,
		logger:      logger,
	}
}

func (d *Downloader) Download(
	ctx context.Context,
	url string,
	dest string,
	expectedSize int64,
	expectedSHA256 string,
) error {

	dlLogger := d.logger.With(
		"destination", dest,
		"expected_size", expectedSize,
		"expected_sha256", expectedSHA256,
	)

	downloadCtx, cancel := context.WithTimeout(
		ctx,
		30*time.Minute,
	)
	defer cancel()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	partPath := dest + ".part"
	statePath := dest + ".state.json"

	state, err := d.loadOrInitState(statePath, expectedSize)
	if err != nil {
		return fmt.Errorf("failed to initialize download state: %w", err)
	}

	if len(state.PendingChunks()) < len(state.Chunks) {
		dlLogger.Info("resuming download",
			"completed_chunks", len(state.Chunks)-len(state.PendingChunks()),
			"total_chunks", len(state.Chunks),
			"pending_chunks", len(state.PendingChunks()))
	}

	file, err := os.OpenFile(
		partPath,
		os.O_CREATE|os.O_RDWR,
		0o644,
	)
	if err != nil {
		return fmt.Errorf("failed to open part file: %w", err)
	}
	defer file.Close()

	if err := file.Truncate(expectedSize); err != nil {
		return fmt.Errorf("failed to pre-allocate file: %w", err)
	}

	etag, err := d.getETag(downloadCtx, url)
	if err != nil {

		dlLogger.Warn("failed to get ETag for integrity verification", "error", err)
	}

	chunkChan := make(chan int, len(state.PendingChunks()))
	for _, chunkIndex := range state.PendingChunks() {
		chunkChan <- chunkIndex
	}
	close(chunkChan)

	var wg sync.WaitGroup
	errChan := make(chan error, d.Concurrency)

	for i := 0; i < d.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for chunkIndex := range chunkChan {
				select {
				case <-downloadCtx.Done():
					errChan <- downloadCtx.Err()
					return
				default:

					if err := d.downloadChunkWithRetry(
						downloadCtx, url, etag, file, state.Chunks[chunkIndex], chunkIndex, dlLogger,
					); err != nil {
						errChan <- fmt.Errorf("worker %d failed chunk %d: %w",
							workerID, chunkIndex, err)
						return
					}

					if err := d.markChunkComplete(statePath, chunkIndex); err != nil {
						errChan <- fmt.Errorf("failed to mark chunk %d complete: %w",
							chunkIndex, err)
						return
					}

					dlLogger.Info("chunk downloaded successfully",
						"chunk_index", chunkIndex,
						"worker_id", workerID,
						"progress", fmt.Sprintf("%d/%d",
							len(state.Chunks)-len(state.PendingChunks())+1, len(state.Chunks)))
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	if err := <-errChan; err != nil {
		return err
	}

	if err := d.verifyAndFinalize(
		partPath, dest, statePath, expectedSHA256, dlLogger,
	); err != nil {
		return fmt.Errorf("final verification failed: %w", err)
	}

	dlLogger.Info("download completed successfully")
	return nil
}

func (d *Downloader) loadOrInitState(statePath string, expectedSize int64) (*DownloadState, error) {

	if state, err := d.readStateFile(statePath); err == nil && state != nil {

		if int64(state.TotalSize()) == expectedSize {
			return state, nil
		}

	}

	chunkCount := int((expectedSize + d.ChunkSize - 1) / d.ChunkSize)
	chunks := make([]Chunk, chunkCount)
	for i := range chunkCount {
		start := int64(i) * d.ChunkSize
		end := start + d.ChunkSize - 1
		if end >= expectedSize {
			end = expectedSize - 1
		}
		chunks[i] = Chunk{
			Index:     i,
			Start:     start,
			End:       end,
			Completed: false,
		}
	}

	state := &DownloadState{
		Version:   1,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Chunks:    chunks,
	}

	if err := d.saveStateFile(statePath, state); err != nil {
		return nil, fmt.Errorf("failed to save initial state: %w", err)
	}

	return state, nil
}

func (d *Downloader) readStateFile(statePath string) (*DownloadState, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var state DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}
	return &state, nil
}

func (d *Downloader) saveStateFile(statePath string, state *DownloadState) error {
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	tempPath := statePath + ".tmp"
	tempFile, err := os.OpenFile(tempPath,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(tempFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return err
	}
	tempFile.Close()

	return os.Rename(tempPath, statePath)
}

func (d *Downloader) markChunkComplete(statePath string, chunkIndex int) error {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()

	state, err := d.readStateFile(statePath)
	if err != nil {
		return err
	}

	if chunkIndex < 0 || chunkIndex >= len(state.Chunks) {
		return fmt.Errorf("invalid chunk index: %d", chunkIndex)
	}

	state.Chunks[chunkIndex].Completed = true
	state.Chunks[chunkIndex].CompletedAt = time.Now().UTC().Format(time.RFC3339)

	return d.saveStateFile(statePath, state)
}

func (d *Downloader) downloadChunkWithRetry(
	ctx context.Context,
	url string,
	etag string,
	file *os.File,
	chunk Chunk,
	chunkIndex int,
	logger *slog.Logger,
) error {
	var lastErr error

	for attempt := 0; attempt <= d.MaxRetries; attempt++ {

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:

			err := d.downloadChunk(ctx, url, etag, file, chunk.Start, chunk.End)
			if err == nil {
				return nil
			}

			lastErr = err

			if !isRetryableError(err) {
				return fmt.Errorf("non-retryable error on attempt %d: %w",
					attempt+1, err)
			}

			if attempt < d.MaxRetries {
				delay := d.calculateBackoffDelay(attempt)
				logger.Warn("chunk download failed, will retry",
					"chunk_index", chunkIndex,
					"attempt", attempt+1,
					"max_attempts", d.MaxRetries+1,
					"error", err.Error(),
					"retry_in", delay)

				select {
				case <-time.After(delay):

				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", d.MaxRetries+1, lastErr)
}

func (d *Downloader) downloadChunk(
	ctx context.Context,
	url string,
	etag string,
	file *os.File,
	start int64,
	end int64,
) error {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	if etag != "" {
		req.Header.Set("If-Range", etag)
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("expected 206 Partial Content, got %d", resp.StatusCode)
	}

	expectedBytes := end - start + 1
	actualBytes := resp.ContentLength
	if actualBytes != -1 && actualBytes != expectedBytes {
		return fmt.Errorf("content length mismatch: expected %d, got %d",
			expectedBytes, actualBytes)
	}

	offset := start
	remaining := expectedBytes
	buf := make([]byte, 1<<20)

	for remaining > 0 {
		toRead := len(buf)
		if int64(toRead) > remaining {
			toRead = int(remaining)
		}
		n, readErr := resp.Body.Read(buf[:toRead])
		if n > 0 {
			if _, err := file.WriteAt(buf[:n], offset); err != nil {
				return fmt.Errorf("write at offset %d: %w", offset, err)
			}
			offset += int64(n)
			remaining -= int64(n)
		}
		if readErr != nil {
			if readErr == io.EOF {
				if remaining > 0 {
					return fmt.Errorf("unexpected EOF: missing %d bytes", remaining)
				}
				break
			}
			return readErr
		}
	}

	return nil
}

func (d *Downloader) getETag(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HEAD request failed with status %d", resp.StatusCode)
	}

	return resp.Header.Get("ETag"), nil
}

func (d *Downloader) verifyAndFinalize(
	partPath, dest, statePath string,
	expectedSHA256 string,
	logger *slog.Logger,
) error {

	fileInfo, err := os.Stat(partPath)
	if err != nil {

		_ = fileInfo
	}

	sum, err := hash.SHA256(partPath)
	if err != nil {
		return fmt.Errorf("failed to calculate SHA256: %w", err)
	}

	if expectedSHA256 != "" && sum != expectedSHA256 {

		_ = os.Remove(statePath)
		_ = os.Remove(partPath)
		return fmt.Errorf("SHA256 verification failed: expected=%s, got=%s",
			expectedSHA256, sum)
	}

	if err := os.Remove(statePath); err != nil {
		logger.Warn("failed to remove state file", "error", err)

	}

	if err := os.Rename(partPath, dest); err != nil {
		return fmt.Errorf("failed to rename part file: %w", err)
	}

	return nil
}

func (d *Downloader) calculateBackoffDelay(attempt int) time.Duration {

	base := float64(d.RetryBase) * math.Pow(2, float64(attempt))

	jitter := rand.Float64() * base * 0.2
	delay := time.Duration(base + jitter)

	if delay > d.RetryMax {
		return d.RetryMax
	}
	return delay
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}

	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		if urlErr.Timeout() {
			return true
		}

	}

	return false
}

type DownloadState struct {
	Version   int     `json:"version"`
	UpdatedAt string  `json:"updated_at"`
	Chunks    []Chunk `json:"chunks"`
}

func (s *DownloadState) TotalSize() int64 {
	if len(s.Chunks) == 0 {
		return 0
	}
	return s.Chunks[len(s.Chunks)-1].End + 1
}

func (s *DownloadState) PendingChunks() []int {
	var pending []int
	for i, chunk := range s.Chunks {
		if !chunk.Completed {
			pending = append(pending, i)
		}
	}
	return pending
}

type Chunk struct {
	Index       int    `json:"index"`
	Start       int64  `json:"start"`
	End         int64  `json:"end"`
	Completed   bool   `json:"completed"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Retries     int    `json:"retries"`
	Error       string `json:"error,omitempty"`
}
