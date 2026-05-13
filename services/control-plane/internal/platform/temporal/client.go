// Package temporal wires the Temporal Go SDK client + worker. The dev server
// runs in docker-compose at temporal:7233; production deployments target
// Temporal Cloud via the same dial-string by swapping env. The single client
// is shared across worker registration + WorkflowService.StartWorkflow on the
// caller side.
package temporal

import (
	"fmt"
	"log/slog"

	"go.temporal.io/sdk/client"
)

// Config carries everything Dial needs to construct a Temporal client. The
// struct is shaped so that future cloud auth (api key, mTLS) can be added
// without breaking callers.
type Config struct {
	HostPort  string
	Namespace string
}

// Dial returns a connected client. Retries are handled by the SDK internally;
// we surface an error only if the initial namespace lookup fails. Callers
// should defer c.Close() on shutdown.
func Dial(cfg Config, logger *slog.Logger) (client.Client, error) {
	if cfg.HostPort == "" {
		return nil, fmt.Errorf("TEMPORAL_HOST_PORT empty")
	}
	c, err := client.Dial(client.Options{
		HostPort:  cfg.HostPort,
		Namespace: cfg.Namespace,
		// SDK default ConnectionOptions are fine for the dev server. Phase 7
		// will swap in cloud auth + tighter health checks here.
	})
	if err != nil {
		return nil, fmt.Errorf("temporal dial: %w", err)
	}
	logger.Info("temporal client connected",
		"host_port", cfg.HostPort,
		"namespace", cfg.Namespace)
	return c, nil
}
