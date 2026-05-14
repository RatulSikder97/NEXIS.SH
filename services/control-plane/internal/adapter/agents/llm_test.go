package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// ----- Test doubles ----------------------------------------------------

// stubLLM is a no-network LLMProvider. If Complete is called and panicOnCall
// is true, the test fails — used to prove the budget guard short-circuits
// BEFORE any provider invocation.
type stubLLM struct {
	resp        domain.CompletionResponse
	err         error
	calls       int
	panicOnCall bool
	t           *testing.T
}

func (s *stubLLM) Name() string { return "stub" }
func (s *stubLLM) Info(_ context.Context) (domain.LLMInfo, error) {
	return domain.LLMInfo{Provider: "stub"}, nil
}
func (s *stubLLM) Complete(_ context.Context, _ domain.CompletionRequest) (domain.CompletionResponse, error) {
	s.calls++
	if s.panicOnCall {
		s.t.Fatalf("LLM Complete called but the budget guard should have rejected the request first")
	}
	return s.resp, s.err
}

// budgetLedger is the in-memory TokenLedger stub. cap=0 means unlimited
// (mirrors the production "no row yet" semantic where CheckBudget returns
// nil because there is no row to compare against).
type budgetLedger struct {
	cap         int
	used        int
	records     []domain.TokenLedgerEntry
	checkCalls  int
	recordCalls int
}

func newBudgetLedger(cap int) *budgetLedger { return &budgetLedger{cap: cap} }

func (l *budgetLedger) CheckBudget(_ context.Context, _ string, expected int) error {
	l.checkCalls++
	if l.cap == 0 {
		return nil
	}
	if l.used+expected > l.cap {
		return domain.ErrBudgetExceeded
	}
	return nil
}
func (l *budgetLedger) Record(_ context.Context, e domain.TokenLedgerEntry) error {
	l.recordCalls++
	l.records = append(l.records, e)
	if e.Status == "succeeded" {
		l.used += e.TokensIn
	}
	return nil
}
func (l *budgetLedger) GetBudget(_ context.Context, _ string) (domain.TokenBudget, error) {
	return domain.TokenBudget{}, nil
}
func (l *budgetLedger) RotatePeriods(_ context.Context, _ time.Time) error { return nil }

// compile-time conformance
var _ domain.TokenLedger = (*budgetLedger)(nil)

// ----- Tests -------------------------------------------------------------

// TestBudget_PerOrgCapEnforced verifies the pre-flight CheckBudget gate: when
// the configured cap is 1000 tokens and the org has already consumed 800, a
// new request that would push the total over the cap is rejected with
// ErrBudgetExceeded and the LLM Provider is NOT invoked.
func TestBudget_PerOrgCapEnforced(t *testing.T) {
	llm := &stubLLM{panicOnCall: true, t: t}
	led := newBudgetLedger(1000)
	led.used = 800
	client := &LLMClient{Provider: llm, Ledger: led}

	_, err := client.Invoke(context.Background(), InvokeRequest{
		Agent:             domain.AgentNameArchitect,
		Model:             "gpt-4o-mini",
		OrgID:             "org-1",
		EstimatedTokensIn: 250, // 800 + 250 = 1050 > 1000
	})

	if !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
	if llm.calls != 0 {
		t.Errorf("LLM Complete must not be called when over cap; got %d", llm.calls)
	}
	if led.recordCalls != 1 {
		t.Errorf("expected one budget_exceeded ledger row, got %d", led.recordCalls)
	}
	if led.records[0].Status != "budget_exceeded" {
		t.Errorf("ledger row status: got %q want budget_exceeded", led.records[0].Status)
	}
}

// TestBudget_NoCapMeansUnlimited verifies that a ledger with cap=0 admits any
// request regardless of size. CheckBudget is still consulted (so we still get
// the audit trail) but the LLM call proceeds and the response flows through.
func TestBudget_NoCapMeansUnlimited(t *testing.T) {
	resp := domain.CompletionResponse{
		Content:      "ok",
		InputTokens:  9999,
		OutputTokens: 100,
		Model:        "gpt-4o-mini",
	}
	llm := &stubLLM{resp: resp, t: t}
	led := newBudgetLedger(0) // unlimited
	client := &LLMClient{Provider: llm, Ledger: led}

	out, err := client.Invoke(context.Background(), InvokeRequest{
		Agent:             domain.AgentNameArchitect,
		Model:             "gpt-4o-mini",
		OrgID:             "org-1",
		EstimatedTokensIn: 1_000_000,
	})

	if err != nil {
		t.Fatalf("unlimited cap should admit any request: %v", err)
	}
	if llm.calls != 1 {
		t.Fatalf("LLM should have been called exactly once, got %d", llm.calls)
	}
	if out.TokensIn != 9999 {
		t.Errorf("TokensIn forwarded: got %d want 9999", out.TokensIn)
	}
	if led.checkCalls != 1 {
		t.Errorf("CheckBudget should be called once even for unlimited, got %d", led.checkCalls)
	}
	if led.recordCalls != 1 || led.records[0].Status != "succeeded" {
		t.Errorf("expected one 'succeeded' ledger row, got %+v", led.records)
	}
}

// TestAgent_BudgetExceeded_NoLLMCall is the strongest claim: when the ledger
// says "no budget", the LLM Provider is never touched. We make the provider
// fail loudly if its Complete method is invoked, then trigger an over-budget
// request and assert (a) the error is wrapped ErrBudgetExceeded and (b) the
// provider's call counter stayed at zero.
func TestAgent_BudgetExceeded_NoLLMCall(t *testing.T) {
	llm := &stubLLM{panicOnCall: true, t: t}
	led := newBudgetLedger(100)
	led.used = 100 // already at cap
	client := &LLMClient{Provider: llm, Ledger: led}

	_, err := client.Invoke(context.Background(), InvokeRequest{
		Agent:             domain.AgentNameArchitect,
		Model:             "gpt-4o-mini",
		OrgID:             "org-1",
		EstimatedTokensIn: 1, // even one token over must be rejected
	})

	if !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
	if llm.calls != 0 {
		t.Fatalf("LLM Provider was invoked %d times; expected 0", llm.calls)
	}
}

// TestBudget_NilLedgerSkipsCheck — defensive: if no ledger is wired (e.g. a
// startup path that hasn't built it yet) the budget check is silently skipped
// and the LLM call proceeds. Documents the production behavior so a future
// change that removes the nil-guard is caught by this test.
func TestBudget_NilLedgerSkipsCheck(t *testing.T) {
	resp := domain.CompletionResponse{
		Content:      "fine",
		InputTokens:  10,
		OutputTokens: 5,
		Model:        "m",
	}
	llm := &stubLLM{resp: resp, t: t}
	client := &LLMClient{Provider: llm} // Ledger is nil

	out, err := client.Invoke(context.Background(), InvokeRequest{
		Agent:             domain.AgentNameArchitect,
		Model:             "m",
		OrgID:             "o",
		EstimatedTokensIn: 999_999_999,
	})
	if err != nil {
		t.Fatalf("nil ledger should bypass budget check: %v", err)
	}
	if out.Content != "fine" {
		t.Errorf("content not forwarded: %+v", out)
	}
	if llm.calls != 1 {
		t.Errorf("expected one LLM call, got %d", llm.calls)
	}
}

// TestBudget_ExceededRecordsAuditRow — when the budget is exceeded, a single
// audit-bearing ledger row with status="budget_exceeded" is persisted so the
// operator can see WHY the agent didn't run. This is the difference between a
// silent drop and an observable refusal.
func TestBudget_ExceededRecordsAuditRow(t *testing.T) {
	llm := &stubLLM{panicOnCall: true, t: t}
	led := newBudgetLedger(50)
	led.used = 50
	client := &LLMClient{Provider: llm, Ledger: led}

	_, _ = client.Invoke(context.Background(), InvokeRequest{
		Agent:             domain.AgentNameQA,
		Model:             "gpt-4o-mini",
		OrgID:             "org-audit",
		WorkflowRunID:     "wf-123",
		EstimatedTokensIn: 1,
	})

	if led.recordCalls != 1 {
		t.Fatalf("expected exactly one ledger row, got %d", led.recordCalls)
	}
	row := led.records[0]
	if row.Status != "budget_exceeded" {
		t.Errorf("status: got %q want budget_exceeded", row.Status)
	}
	if row.OrgID != "org-audit" {
		t.Errorf("OrgID: got %q want org-audit", row.OrgID)
	}
	if row.WorkflowRunID != "wf-123" {
		t.Errorf("WorkflowRunID: got %q want wf-123", row.WorkflowRunID)
	}
	if row.Agent != domain.AgentNameQA {
		t.Errorf("Agent: got %q want qa", row.Agent)
	}
}
