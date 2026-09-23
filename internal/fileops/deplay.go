package fileops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Deployer struct{}

func NewDeployer() *Deployer {
	return &Deployer{}
}
func (d *Deployer) Backup(
	target string,
	backup string,
) error {
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf(
			"target file does not exist: %w",
			err,
		)
	}

	if err := os.MkdirAll(
		filepath.Dir(backup),
		0755,
	); err != nil {
		return err
	}

	tempBackup := backup + ".tmp"

	if err := copyFile(
		target,
		tempBackup,
	); err != nil {
		return fmt.Errorf(
			"create backup: %w",
			err,
		)
	}

	if err := syncFile(tempBackup); err != nil {
		return err
	}

	if err := os.Rename(
		tempBackup,
		backup,
	); err != nil {
		return fmt.Errorf(
			"finalize backup: %w",
			err,
		)
	}

	return nil
}
func (d *Deployer) Apply(
	staged string,
	target string,
) error {
	if _, err := os.Stat(staged); err != nil {
		return fmt.Errorf(
			"staged file does not exist: %w",
			err,
		)
	}

	tempTarget := target + ".new"

	if err := copyFile(
		staged,
		tempTarget,
	); err != nil {
		return fmt.Errorf(
			"copy staged file: %w",
			err,
		)
	}

	if err := syncFile(tempTarget); err != nil {
		return err
	}

	if err := os.Rename(
		tempTarget,
		target,
	); err != nil {
		return fmt.Errorf(
			"replace target: %w",
			err,
		)
	}

	return nil
}
func (d *Deployer) Rollback(
	backup string,
	target string,
) error {
	if _, err := os.Stat(backup); err != nil {
		return fmt.Errorf(
			"backup does not exist: %w",
			err,
		)
	}

	tempTarget := target + ".rollback"

	if err := copyFile(
		backup,
		tempTarget,
	); err != nil {
		return fmt.Errorf(
			"restore backup: %w",
			err,
		)
	}

	if err := syncFile(tempTarget); err != nil {
		return err
	}

	if err := os.Rename(
		tempTarget,
		target,
	); err != nil {
		return fmt.Errorf(
			"finalize rollback: %w",
			err,
		)
	}

	return nil
}
func copyFile(
	source string,
	destination string,
) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}

	defer input.Close()

	output, err := os.OpenFile(
		destination,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0644,
	)
	if err != nil {
		return err
	}

	_, copyErr := io.CopyBuffer(
		output,
		input,
		make([]byte, 1024*1024),
	)

	if copyErr != nil {
		_ = output.Close()
		return copyErr
	}

	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}

	return output.Close()
}

func syncFile(path string) error {
	file, err := os.OpenFile(
		path,
		os.O_WRONLY,
		0,
	)
	if err != nil {
		return err
	}

	defer file.Close()

	return file.Sync()
}
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

	return hex.EncodeToString(
		hasher.Sum(nil),
	), nil
}
