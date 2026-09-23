package docker

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/mmk31585/updater-service/internal/services"
)

type Runner struct {
	services map[string]services.ServiceDefinition
}

func NewRunner(servicesRoot string) *Runner {
	r := &Runner{
		services: make(map[string]services.ServiceDefinition),
	}

	for _, name := range []string{"hello-service", "config-service", "test-service"} {
		if def, ok := services.NewServiceDefinition(servicesRoot, name); ok {
			r.services[name] = def
		}
	}

	return r
}

func (r *Runner) Restart(ctx context.Context,service string,) error {
	definition, ok := r.services[service]
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
		definition.Container,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"docker restart %s failed: %w: %s",
			definition.Container,
			err,
			string(output),
		)
	}

	return nil
}
func (r *Runner) HealthURL(service string,) (string, bool) {
	definition, ok := r.services[service]

	if !ok {
		return "", false
	}

	return definition.HealthURL, true
}

func (r *Runner) Service(service string,) (services.ServiceDefinition, bool) {
	definition, ok := r.services[service]
	if !ok {
		return services.ServiceDefinition{}, false
	}
	return definition, true
}
