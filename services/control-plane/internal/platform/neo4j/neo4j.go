// Package neo4j hosts the Neo4j driver lifecycle helpers used by the
// graphstore adapter (internal/adapter/graphstore/neo4j) and the
// cmd/seed-neo4j CLI. It is the only package that may import the
// neo4j-go-driver/v5 module directly — everywhere else flows through the
// domain.Graph port.
package neo4j

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Config carries the bolt URI + basic-auth credentials. Neo4j only supports
// basic auth in community edition; rotation is a Phase 7 concern.
type Config struct {
	URI      string
	User     string
	Password string
}

// New opens a driver with default pool settings. Caller is responsible for
// calling driver.Close at shutdown.
func New(ctx context.Context, cfg Config) (neo4j.DriverWithContext, error) {
	drv, err := neo4j.NewDriverWithContext(cfg.URI,
		neo4j.BasicAuth(cfg.User, cfg.Password, ""),
		func(c *neo4j.Config) {
			c.MaxConnectionPoolSize = 20
			c.ConnectionAcquisitionTimeout = 30 * time.Second
		},
	)
	if err != nil {
		return nil, fmt.Errorf("neo4j.New: %w", err)
	}
	return drv, nil
}

// Verify probes the driver with a 60-second budget. Called from main.go at
// startup; on failure the Sentinel goroutine logs WARN and disables the
// Pathfinder graph-evidence path.
func Verify(ctx context.Context, drv neo4j.DriverWithContext) error {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return drv.VerifyConnectivity(cctx)
}
