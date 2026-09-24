package services

import (
	"io"
	"os"
	"path/filepath"
)

type ServiceDefinition struct {
	Container  string
	ConfigPath string
	HealthURL  string
}

func NewServiceDefinition(root, service string) (ServiceDefinition, bool) {
	services := map[string]ServiceDefinition{
		"hello-service": {
			Container:  "hello-service",
			ConfigPath: filepath.Join(root, "hello-service", "config.yaml"),
			HealthURL:  "http://localhost:18080/health",
		},
		"config-service": {
			Container:  "config-service",
			ConfigPath: filepath.Join(root, "config-service", "config", "config.yaml"),
		},
		"test-service": {
			Container:  "test-service",
			ConfigPath: filepath.Join(root, "test-service", "config", "config.yaml"),
		},
		"data-service": {
			Container:  "data-service",
			ConfigPath: filepath.Join(root, "data-service", "config.yaml"),
			HealthURL:  "http://data-service:8080/health",
		},
	}
	def, ok := services[service]
	return def, ok
}

func BackupFile(source, backup string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(backup)
	if err != nil {
		return err
	}
	defer output.Close()
	_, copyErr := io.Copy(output, input)
	if copyErr != nil {
		return copyErr
	}
	return output.Sync()
}

func ReplaceConfig(staged, target, backup string) error {
	tempTarget := target + ".new"
	if err := copyFile(staged, tempTarget); err != nil {
		return err
	}
	if err := syncFile(tempTarget); err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(tempTarget, target); err != nil {
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer output.Close()
	_, err = io.Copy(output, input)
	if err != nil {
		return err
	}
	return output.Sync()
}

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
