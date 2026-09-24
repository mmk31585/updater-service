package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

const BufferSize = 1024 * 1024

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: verify <file_path> <expected_sha256>\n")
		os.Exit(1)
	}

	filePath := os.Args[1]
	expected := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	hasher := sha256.New()
	_, err = io.CopyBuffer(hasher, f, make([]byte, BufferSize))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to hash file: %v\n", err)
		os.Exit(1)
	}

	actual := hex.EncodeToString(hasher.Sum(nil))

	if actual == expected {
		fmt.Printf("PASS: SHA256 match\n")
		fmt.Printf("  File: %s\n", filePath)
		fmt.Printf("  SHA256: %s\n", actual)
		os.Exit(0)
	} else {
		fmt.Fprintf(os.Stderr, "FAIL: SHA256 mismatch\n")
		fmt.Fprintf(os.Stderr, "  File: %s\n", filePath)
		fmt.Fprintf(os.Stderr, "  Expected: %s\n", expected)
		fmt.Fprintf(os.Stderr, "  Actual:   %s\n", actual)
		os.Exit(1)
	}
}
