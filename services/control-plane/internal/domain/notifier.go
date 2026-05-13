package domain

import "context"

// NotificationKind enumerates the user-visible event types Phase 6 emits.
// Add cases here when a new event needs delivery routing.
type NotificationKind string

const (
	NotifApprovalRequested NotificationKind = "approval_requested"
	NotifPipelineComplete  NotificationKind = "pipeline_complete"
)

// ChannelTarget identifies a delivery destination. Used by the multi-fanout
// notifier to map a tenant org to its currently connected channels (slack
// webhook URL, email address, console-only fallback).
type ChannelTarget struct {
	Channel string // 'slack' | 'email' | 'console'
	Address string // webhook URL / email / "" for console
}

// Notification is the rendered event a Notifier consumes. LinkURL is the
// deep link the user clicks to land on the relevant Console surface (the
// approvals queue, the pipeline timeline page, etc.).
type Notification struct {
	OrgID         string
	WorkspaceID   string
	WorkflowRunID string
	Kind          NotificationKind
	Severity      Severity
	Scenario      string
	Title         string
	Body          string
	LinkURL       string // console deep link
}

// Notifier is the per-channel port. internal/adapter/notifier/{slack,email,
// console,multi}.go implement it.
type Notifier interface {
	Channel() string // 'slack' | 'email' | 'console'
	Send(ctx context.Context, n Notification) error
}
