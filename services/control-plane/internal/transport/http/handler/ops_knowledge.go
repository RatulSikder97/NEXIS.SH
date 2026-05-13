package handler

// pgvector retrieval state — GET /v1/knowledge/status.
//
// Reads code_embeddings (Phase 5) for the caller's org. The schema today
// keys embeddings by repo_sha rather than workspace_id, so the response
// groups by repo_sha and synthesises the WorkspaceID field from it. The
// frontend treats the value as opaque; it just needs SOMETHING stable so
// it can render the chunk counts per indexed repo.
//
// last_query_ms is read from the `query_logs` cache when present;
// otherwise the field stays 0. Phase 5 didn't ship a structured query
// log, so the field is a forward-compat placeholder.

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// KnowledgeStatus wires GET /v1/knowledge/status. Returns the total chunk
// count + per-(repo_sha) breakdown for the caller's org.
//
// Admin pool — read uniform with the rest of the operator endpoints.
func KnowledgeStatus(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		resp := dto.KnowledgeStatusResp{Workspaces: []dto.KnowledgeWorkspaceStatus{}}
		if pool == nil {
			writeJSON(w, http.StatusOK, resp)
			return
		}

		// Total chunks.
		if err := pool.QueryRow(r.Context(),
			`SELECT count(*) FROM code_embeddings WHERE org_id=$1`,
			princ.OrgID,
		).Scan(&resp.TotalChunks); err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		// Per-(repo_sha) breakdown — the column is the closest thing to a
		// workspace identifier on the embeddings table today. The Name
		// field surfaces the same value so the dashboard has a non-empty
		// label even before a richer mapping is wired.
		rows, err := pool.Query(r.Context(), `
            SELECT repo_sha,
                   count(*)                                   AS chunk_count,
                   MAX(created_at)                            AS last_indexed_at
            FROM code_embeddings
            WHERE org_id=$1
            GROUP BY repo_sha
            ORDER BY last_indexed_at DESC NULLS LAST`,
			princ.OrgID)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var (
				sha           string
				chunkCount    int
				lastIndexedAt *time.Time
			)
			if err := rows.Scan(&sha, &chunkCount, &lastIndexedAt); err != nil {
				httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			ws := dto.KnowledgeWorkspaceStatus{
				WorkspaceID: sha,
				Name:        sha,
				ChunkCount:  chunkCount,
			}
			if lastIndexedAt != nil && !lastIndexedAt.IsZero() {
				ws.LastIndexedAt = lastIndexedAt.UTC().Format(time.RFC3339)
			}
			resp.Workspaces = append(resp.Workspaces, ws)
		}

		// last_query_ms — no structured query log yet; leave 0 so the
		// dashboard can render the field as a forward-compat placeholder
		// without breaking when it does ship.
		resp.LastQueryMs = 0

		writeJSON(w, http.StatusOK, resp)
	}
}
