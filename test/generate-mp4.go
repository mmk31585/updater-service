package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
)

const TargetSize = 2 * 1024 * 1024 * 1024
const OutputPath = "./data/test.mp4"
const HashPath = "./data/test.mp4.sha256"
const BufferSize = 1024 * 1024

func main() {
	fmt.Printf("Generating %d bytes (%d GB) MP4 test file...\n", TargetSize, TargetSize/(1024*1024*1024))

	if err := os.MkdirAll(filepath.Dir(OutputPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create directory: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(OutputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	mp4Header := buildMP4Header()
	written, err := writer.Write(mp4Header)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write header: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("MP4 header written: %d bytes\n", written)

	buf := make([]byte, BufferSize)
	for i := range buf {
		buf[i] = byte(rand.IntN(256))
	}

	remaining := TargetSize - int64(len(mp4Header))
	chunkCount := remaining / int64(BufferSize)
	lastChunk := remaining % int64(BufferSize)

	progressInterval := max(chunkCount/50, 1)

	for i := range chunkCount {
		_, err := writer.Write(buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write data: %v\n", err)
			os.Exit(1)
		}
		if i%progressInterval == 0 {
			done := int64(len(mp4Header)) + (i * int64(BufferSize))
			pct := float64(done) / float64(TargetSize) * 100
			fmt.Printf("\rProgress: %.1f%% (%d/%d GB)", pct, done/(1024*1024*1024), TargetSize/(1024*1024*1024))
		}
	}

	if lastChunk > 0 {
		partial := buf[:lastChunk]
		_, err := writer.Write(partial)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write final chunk: %v\n", err)
			os.Exit(1)
		}
	}

	err = f.Sync()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to sync: %v\n", err)
		os.Exit(1)
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	err = os.WriteFile(HashPath, []byte(sum), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write hash: %v\n", err)
		os.Exit(1)
	}

	info, err := f.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to stat file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nDone!\n")
	fmt.Printf("File: %s\n", OutputPath)
	fmt.Printf("Size: %d bytes (%.2f GB)\n", info.Size(), float64(info.Size())/(1024*1024*1024))
	fmt.Printf("SHA256: %s\n", sum)
	fmt.Printf("Hash saved to: %s\n", HashPath)
}

func buildMP4Header() []byte {
	ftypBox := []byte("isom")
	minorVersion := []byte{0, 0, 0, 0}
	compatBrands := []byte("isomavc1")

	boxContent := append(minorVersion, ftypBox...)
	boxContent = append(boxContent, compatBrands...)

	boxSize := 4 + 4 + len(boxContent)
	header := make([]byte, 0, boxSize)
	header = append(header, encodeSize(uint64(boxSize))...)
	header = append(header, []byte("ftyp")...)
	header = append(header, boxContent...)

	return header
}

func encodeSize(size uint64) []byte {
	b := make([]byte, 4)
	if size > 0xFFFFFFFF {
		b[0] = 0
		b[1] = 1
		b[2] = byte((size >> 24) & 0xFF)
		b[3] = byte((size >> 32) & 0xFF)
	} else {
		b[0] = byte((size >> 24) & 0xFF)
		b[1] = byte((size >> 16) & 0xFF)
		b[2] = byte((size >> 8) & 0xFF)
		b[3] = byte(size & 0xFF)
	}
	return b
}
