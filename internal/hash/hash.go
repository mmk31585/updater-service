package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

func SHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()

	if _, err := io.CopyBuffer(
		hasher,
		file,
		make([]byte, 1024*1024),
	); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
