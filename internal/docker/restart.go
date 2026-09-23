package docker

import (
	"context"
	"fmt"
	"os/exec"
)

type Runner struct {
	containers map[string]string
}

func NewRunner() *Runner {
	return &Runner{
		containers: map[string]string{
			"hello-service":  "hello-service",
			"config-service": "config-service",
			"test-service":   "test-service",
		},
	}
}

func (r *Runner) Restart(
	ctx context.Context,
	service string,
) error {
	container, ok := r.containers[service]
	if !ok {
		return fmt.Errorf(
			"unknown service %q",
			service,
		)
	}

	cmd := exec.CommandContext(
		ctx,
		"docker",
		"restart",
		"-t",
		"10",
		container,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"docker restart %s failed: %w: %s",
			container,
			err,
			string(output),
		)
	}

	return nil
}