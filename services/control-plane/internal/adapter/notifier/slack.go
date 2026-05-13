package notifier

import (
	"context"
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Slack is the domain.Notifier implementation that renders a
// Notification as a Slack Block Kit payload and posts it via the
// per-org Slack provider. The provider owns the encrypted webhook URL +
// HTTP transport; this notifier only owns rendering + the console
// deep-link template.
type Slack struct {
	prov           *slack.Provider
	consoleBaseURL string
}

// NewSlack constructs a Slack notifier. consoleBaseURL is the Phase 3
// CONSOLE_BASE_URL config — used to render the "Review in console" button.
// Trailing slashes are tolerated; rendering trims them.
func NewSlack(p *slack.Provider, consoleBaseURL string) *Slack {
	return &Slack{prov: p, consoleBaseURL: consoleBaseURL}
}

// Channel returns the canonical channel name "slack".
func (s *Slack) Channel() string { return "slack" }

// Send renders Block Kit JSON and posts via the slack provider's SendBlock.
// Errors are surfaced (the multi-fanout layer absorbs them). When no slack
// connection exists for the org, this returns domain.ErrNotFound — multi
// logs and moves on.
func (s *Slack) Send(ctx context.Context, n domain.Notification) error {
	if s.prov == nil {
		return fmt.Errorf("slack notifier: provider not configured")
	}
	blocks := buildApprovalBlocks(n, s.consoleBaseURL)
	_, err := s.prov.SendBlock(ctx, n.OrgID, blocks)
	return err
}

// buildApprovalBlocks renders the Block Kit message for an
// approval_requested notification. Headline shows the scenario; section
// columns show severity + truncated run id; an action button deep-links
// the operator into the approvals queue.
func buildApprovalBlocks(n domain.Notification, baseURL string) []map[string]any {
	link := n.LinkURL
	if link == "" {
		// Fallback deep link — base URL + canonical approvals route.
		link = trimRight(baseURL, '/') + "/console/approvals/" + n.WorkflowRunID
	}
	headline := n.Title
	if headline == "" {
		headline = fmt.Sprintf("Approval required — %s", n.Scenario)
	}
	body := n.Body
	if body == "" {
		body = "Pipeline is parked at the Approval Gate."
	}
	return []map[string]any{
		{
			"type": "header",
			"text": map[string]any{
				"type": "plain_text",
				"text": headline,
			},
		},
		{
			"type": "section",
			"fields": []map[string]any{
				{"type": "mrkdwn", "text": fmt.Sprintf("*Severity*\n%s", n.Severity)},
				{"type": "mrkdwn", "text": fmt.Sprintf("*Run ID*\n%s", short(n.WorkflowRunID))},
			},
		},
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": "*Plan:* " + body,
			},
		},
		{
			"type": "actions",
			"elements": []map[string]any{
				{
					"type": "button",
					"style": "primary",
					"text": map[string]any{
						"type": "plain_text",
						"text": "Review in console",
					},
					"url": link,
				},
			},
		},
	}
}

// short returns the first 8 chars of id (or the full string when shorter).
// Used to keep the Slack message compact without leaking the full uuid.
func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// trimRight strips one trailing rune from s if present.
func trimRight(s string, r rune) string {
	if s == "" {
		return s
	}
	last := []rune(s)
	if last[len(last)-1] == r {
		return string(last[:len(last)-1])
	}
	return s
}

// compile-time conformance check
var _ domain.Notifier = (*Slack)(nil)
