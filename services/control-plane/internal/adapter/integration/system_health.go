package integration

// system_health.go — probe helpers for the /v1/system-health endpoint.
//
// Each probe is a tiny self-contained function that connects to one
// dependency, performs a minimal liveness command, and returns its
// (latency, error). The helpers live here because (a) the spec earmarks
// `internal/adapter/integration/` for a "probe helper" and (b) the rest
// of the adapter layer already imports the heavy dependencies we'd
// otherwise need (minio-go, neo4j-go-driver). Keeping the probe surface
// inside this package avoids a new top-level package.
//
// The helpers never panic, never block forever, and never log — the
// caller decides what to do with the result.
//
// Design notes:
//
//   - Each probe takes its own per-call timeout via the passed ctx so the
//     handler can fan out 5 probes in parallel and join on the slowest.
//
//   - The Redis probe speaks raw RESP over TCP because the control-plane
//     does not import a Redis client at the top level (only the github
//     adapter's installation_token_cache.go references a Redis interface).
//     A raw TCP write + read is enough to confirm the server is responsive.
//
//   - The Neo4j probe takes an optional driver — when nil it reports the
//     "disabled" status so the operator UI can tell apart "configured and
//     down" from "not wired".

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
)

// ProbeResult is the (latency, error) pair every probe returns. Error is
// non-nil when the probe couldn't reach the dependency OR the dependency
// returned an unhealthy response.
type ProbeResult struct {
	Latency time.Duration
	Err     error
}

// HealthyWithin reports whether the result counts as "healthy" given the
// supplied warning threshold. Latency > threshold but no error → degraded.
func (r ProbeResult) HealthyWithin(threshold time.Duration) string {
	if r.Err != nil {
		return "down"
	}
	if threshold > 0 && r.Latency > threshold {
		return "degraded"
	}
	return "healthy"
}

// ProbePostgres runs `SELECT 1` on the supplied pool. The pool's
// connection-acquisition timeout still applies; supply a derived ctx with
// your own deadline if you need a tighter SLO.
//
// Returns (0, errors.New("disabled")) when pool is nil so the handler can
// surface a clear "disabled" status without conflating it with a 500.
func ProbePostgres(ctx context.Context, pool *pgxpool.Pool) ProbeResult {
	if pool == nil {
		return ProbeResult{Err: errors.New("disabled")}
	}
	start := time.Now()
	var one int
	err := pool.QueryRow(ctx, "SELECT 1").Scan(&one)
	return ProbeResult{Latency: time.Since(start), Err: err}
}

// ProbeRedis dials the address and writes the RESP PING command directly.
// Returns the latency to the +PONG response. addr is in `host:port` form
// (e.g. "redis:6379"). Empty addr → "disabled".
//
// Speaks RESP3-compatible plain text — the only command issued is PING, which
// every Redis-compatible server answers with "+PONG\r\n". A 2s I/O deadline
// bounds the probe so a slow Redis can't stall the handler.
func ProbeRedis(ctx context.Context, addr string) ProbeResult {
	if addr == "" {
		return ProbeResult{Err: errors.New("disabled")}
	}
	start := time.Now()
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return ProbeResult{Latency: time.Since(start), Err: err}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	// RESP "PING\r\n" as a one-shot inline command. Servers accept it
	// alongside the array form (*1\r\n$4\r\nPING\r\n).
	if _, err := conn.Write([]byte("PING\r\n")); err != nil {
		return ProbeResult{Latency: time.Since(start), Err: err}
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return ProbeResult{Latency: time.Since(start), Err: err}
	}
	line = strings.TrimRight(line, "\r\n")
	if line != "+PONG" {
		return ProbeResult{Latency: time.Since(start), Err: fmt.Errorf("unexpected reply %q", line)}
	}
	return ProbeResult{Latency: time.Since(start)}
}

// ProbeNeo4j runs `RETURN 1` against the supplied driver. nil driver →
// "disabled" so the operator UI can distinguish "configured + down" from
// "not wired at all".
//
// Uses an explicit ExecuteRead so the probe runs on a reader-tier session
// even on a clustered deployment; on a single-node Neo4j the call falls
// back to the only server.
func ProbeNeo4j(ctx context.Context, drv neo4j.DriverWithContext) ProbeResult {
	if drv == nil {
		return ProbeResult{Err: errors.New("disabled")}
	}
	start := time.Now()
	session := drv.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	_, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, qErr := tx.Run(ctx, "RETURN 1", nil)
		if qErr != nil {
			return nil, qErr
		}
		_, qErr = res.Single(ctx)
		return nil, qErr
	})
	return ProbeResult{Latency: time.Since(start), Err: err}
}

// MinIOConfig bundles the inputs ProbeMinIO needs to construct an ephemeral
// client. Keeping it as a struct so the handler doesn't have to know about
// minio-go directly.
type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Bucket    string // optional — when set, we additionally probe BucketExists
}

// ProbeMinIO checks whether a MinIO/S3 endpoint accepts a credential-less
// HEAD on the bucket. Empty Endpoint → "disabled". The probe uses a fresh
// client every call so a credential rotation doesn't leave a stale handle
// here.
func ProbeMinIO(ctx context.Context, cfg MinIOConfig) ProbeResult {
	if cfg.Endpoint == "" {
		return ProbeResult{Err: errors.New("disabled")}
	}
	start := time.Now()
	c, err := miniogo.New(cfg.Endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return ProbeResult{Latency: time.Since(start), Err: err}
	}
	bucket := cfg.Bucket
	if bucket == "" {
		// Without a specific bucket the cheapest reachability check is
		// ListBuckets — only the response status matters.
		_, err = c.ListBuckets(ctx)
		return ProbeResult{Latency: time.Since(start), Err: err}
	}
	_, err = c.BucketExists(ctx, bucket)
	return ProbeResult{Latency: time.Since(start), Err: err}
}

// ProbeTemporal pings the Temporal frontend service via CheckHealth. Nil
// client → "disabled". The supplied ctx's deadline bounds the call.
func ProbeTemporal(ctx context.Context, c client.Client) ProbeResult {
	if c == nil {
		return ProbeResult{Err: errors.New("disabled")}
	}
	start := time.Now()
	_, err := c.CheckHealth(ctx, &client.CheckHealthRequest{})
	return ProbeResult{Latency: time.Since(start), Err: err}
}
