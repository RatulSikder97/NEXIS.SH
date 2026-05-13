// Package handler — Slack interactivity HTTP surface.
//
// One endpoint: POST /v1/integrations/slack/interactivity. Slack hits this on
// every button click in a message the bot posted (Approval Gate's approve /
// reject blocks). The request is form-encoded with a single "payload" field
// holding a JSON string; Slack signs the *raw* body with the workspace app's
// Signing Secret, so the handler MUST read the body before any form parsing,
// verify the signature, and only then dispatch.
//
// Auth model: Slack-signed request only. No session cookie, no bearer. The
// signature IS the authentication — we reject every request that fails
// VerifyRequestSignature with 401. Once verified, the user identity comes
// from the payload's user.email field (populated when the bot has the
// users:read.email scope).
//
// Coordination note: the wiring (mount this handler under the slack route +
// inject the approvals service) belongs to server.go, which is owned by the
// coordinator. This file just exports the HandlerFunc factory.
package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// SlackApprovalsService is the narrow port the SlackInteractivity handler
// depends on. It exists locally in the handler package because the broader
// domain.ApprovalsService surface has not landed yet; the controller will
// either rename this into domain/ or wire a small adapter that satisfies it
// against the existing internal/adapter/approval.SignalerService.
//
// Decide is invoked once a verified block_actions payload has been dispatched:
//   - workflowRunID — the approval row's run id, lifted from the button's
//     value field ("<run_id>:<decision>" — see encodeButtonValue below).
//   - decision      — domain.ApprovalApproved or domain.ApprovalRejected.
//   - actorEmail    — the Slack user's email from the payload, used purely
//     for audit attribution.
type SlackApprovalsService interface {
	Decide(ctx context.Context, workflowRunID string, decision domain.ApprovalDecisionState, actorEmail string) error
}

// slackInteractivityResp is the JSON shape we return on every decoded request.
// Slack itself does not require a JSON body — a bare 200 with no body is
// valid — but returning {ok, message} lets BetterStack-style probes assert on
// the response shape instead of just the status code.
type slackInteractivityResp struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// SlackInteractivity wires POST /v1/integrations/slack/interactivity. The
// handler:
//
//  1. Reads the raw body in full (must precede signature verification).
//  2. Verifies X-Slack-Signature against signingSecret over the raw body.
//  3. Parses the form, extracts the "payload" JSON, decodes to
//     slack.InteractivityPayload.
//  4. For block_actions with action_id="approve"|"reject", decodes the value
//     field ("<run_id>:<decision>") and dispatches to approvals.Decide.
//  5. Responds 200 + {ok:true}.
//
// Any failure in steps 1-3 surfaces as 401 (Slack treats anything other
// than 200 as a delivery failure and will retry, so we keep the response
// quick + body small). Failures in step 4 surface as 500 — that is a
// server-side fault, not a Slack-trust fault.
func SlackInteractivity(approvals SlackApprovalsService, signingSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Cap the body at 1 MiB. Slack interactivity payloads are small
		// (kilobytes); anything larger is malformed or hostile.
		const maxBody = 1 << 20
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, slackInteractivityResp{Message: "read body"})
			return
		}

		ts := r.Header.Get("X-Slack-Request-Timestamp")
		sig := r.Header.Get("X-Slack-Signature")
		if err := slack.VerifyRequestSignature(string(signingSecret), string(bodyBytes), ts, sig); err != nil {
			writeJSON(w, http.StatusUnauthorized, slackInteractivityResp{Message: "invalid signature"})
			return
		}

		form, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, slackInteractivityResp{Message: "bad form"})
			return
		}
		payload, err := slack.ParseInteractivity(form)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, slackInteractivityResp{Message: err.Error()})
			return
		}
		if len(payload.Actions) == 0 {
			// Defensive: block_actions always carries at least one action;
			// receiving zero means the wire shape changed under us.
			writeJSON(w, http.StatusBadRequest, slackInteractivityResp{Message: "no actions"})
			return
		}

		// Dispatch each action. In practice Slack only sends one per click
		// (the click that triggered the request), but iterating is robust
		// against future multi-action interactions.
		for _, a := range payload.Actions {
			runID, decision, derr := decodeButtonValue(a.Value)
			if derr != nil {
				writeJSON(w, http.StatusBadRequest, slackInteractivityResp{Message: derr.Error()})
				return
			}
			if err := approvals.Decide(r.Context(), runID, decision, payload.User.Email); err != nil {
				slog.Default().Error("slack interactivity decide", "err", err, "run_id", runID, "decision", decision)
				writeJSON(w, http.StatusInternalServerError, slackInteractivityResp{Message: "decide failed"})
				return
			}
		}

		writeJSON(w, http.StatusOK, slackInteractivityResp{OK: true, Message: "decision recorded"})
	}
}

// decodeButtonValue parses the "value" field embedded on every approve/reject
// button. The wire format is "<workflow_run_id>:<decision>" where decision
// is the literal string "approved" or "rejected". The notifier that posts
// the block keeps the same encoding — keep both sides in sync.
func decodeButtonValue(v string) (runID string, decision domain.ApprovalDecisionState, err error) {
	if v == "" {
		return "", "", errors.New("empty button value")
	}
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return "", "", errors.New("expected <run_id>:<decision>")
	}
	switch domain.ApprovalDecisionState(parts[1]) {
	case domain.ApprovalApproved:
		decision = domain.ApprovalApproved
	case domain.ApprovalRejected:
		decision = domain.ApprovalRejected
	default:
		return "", "", errors.New("decision must be approved or rejected")
	}
	return parts[0], decision, nil
}
