package filetransfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const ChunkSize int64 = 16 * 1024 * 1024

type Downloader struct {
	Client *http.Client
}

func NewDownloader() *Downloader {
	return &Downloader{
		Client: &http.Client{},
	}
}

func (d *Downloader) Download(
	ctx context.Context,
	url string,
	dest string,
	expectedSize int64,
	expectedSHA256 string,
) error {
	partPath := dest + ".part"

	if err := os.MkdirAll(
		filepath.Dir(dest),
		0755,
	); err != nil {
		return err
	}

	currentSize, err := fileSize(partPath)
	if err != nil {
		return err
	}

	if currentSize > expectedSize {
		if err := os.Truncate(partPath, 0); err != nil {
			return err
		}

		currentSize = 0
	}

	hasher := sha256.New()

	if currentSize > 0 {
		if err := hashExistingFile(
			partPath,
			hasher,
		); err != nil {
			return err
		}
	}

	file, err := os.OpenFile(
		partPath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		return err
	}

	defer file.Close()

	offset := currentSize

	for offset < expectedSize {
		end := offset + ChunkSize - 1

		if end >= expectedSize {
			end = expectedSize - 1
		}

		err := d.downloadRange(
			ctx,
			url,
			file,
			hasher,
			offset,
			end,
		)
		if err != nil {
			return err
		}

		offset = end + 1

		if err := file.Sync(); err != nil {
			return err
		}
	}

	sum := hex.EncodeToString(
		hasher.Sum(nil),
	)

	if sum != expectedSHA256 {
		return fmt.Errorf(
			"sha256 mismatch: expected=%s actual=%s",
			expectedSHA256,
			sum,
		)
	}

	if err := file.Close(); err != nil {
		return err
	}

	if err := os.Rename(
		partPath,
		dest,
	); err != nil {
		return err
	}

	return nil
}
func (d *Downloader) downloadRange(
	ctx context.Context,
	url string,
	file *os.File,
	hasher io.Writer,
	start int64,
	end int64,
) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return err
	}

	req.Header.Set(
		"Range",
		fmt.Sprintf(
			"bytes=%d-%d",
			start,
			end,
		),
	)

	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf(
			"expected 206, got %d",
			resp.StatusCode,
		)
	}

	expectedBytes := end - start + 1

	writer := io.MultiWriter(
		file,
		hasher,
	)

	n, err := io.CopyBuffer(
		writer,
		io.LimitReader(
			resp.Body,
			expectedBytes,
		),
		make([]byte, 1024*1024),
	)
	if err != nil {
		return err
	}

	if n != expectedBytes {
		return fmt.Errorf(
			"incomplete range: expected=%d got=%d",
			expectedBytes,
			n,
		)
	}

	return nil
}
func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)

	if os.IsNotExist(err) {
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

func hashExistingFile(
	path string,
	hasher io.Writer,
) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}

	defer file.Close()

	_, err = io.CopyBuffer(
		hasher,
		file,
		make([]byte, 1024*1024),
	)

	return err
}
