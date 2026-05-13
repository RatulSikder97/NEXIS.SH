// Package handler — billing HTTP surface.
//
// Seven endpoints under /v1/billing:
//
//   - GET    /v1/billing/payment-method        (owner|admin)
//   - POST   /v1/billing/payment-method        (owner)
//   - DELETE /v1/billing/payment-method        (owner)
//   - GET    /v1/billing/invoices              (owner|admin)
//   - POST   /v1/billing/invoices/recompute    (owner|admin, dev only)
//   - GET    /v1/billing/usage                 (owner|admin)
package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// toPaymentMethodResp converts a domain.PaymentMethod into the wire shape.
func toPaymentMethodResp(pm domain.PaymentMethod) dto.PaymentMethodResp {
	return dto.PaymentMethodResp{
		ID:                      pm.ID,
		OrgID:                   pm.OrgID,
		Provider:                pm.Provider,
		Brand:                   pm.Brand,
		Last4:                   pm.Last4,
		ExpMonth:                pm.ExpMonth,
		ExpYear:                 pm.ExpYear,
		BillingEmail:            pm.BillingEmail,
		ExternalCustomerID:      pm.ExternalCustomerID,
		ExternalPaymentMethodID: pm.ExternalPaymentMethodID,
		CreatedAt:               pm.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// toInvoiceResp converts a domain.Invoice into the wire shape.
func toInvoiceResp(inv domain.Invoice) dto.InvoiceResp {
	return dto.InvoiceResp{
		ID:          inv.ID,
		OrgID:       inv.OrgID,
		PeriodStart: inv.PeriodStart.UTC().Format(time.RFC3339),
		PeriodEnd:   inv.PeriodEnd.UTC().Format(time.RFC3339),
		TotalCents:  inv.TotalCents,
		Status:      inv.Status,
		CreatedAt:   inv.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// BillingGetPaymentMethod wires GET /v1/billing/payment-method. Returns 404
// when no method is on file.
func BillingGetPaymentMethod(provider domain.BillingProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		pm, err := provider.GetPaymentMethod(r.Context(), princ)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "no payment method")
				return
			}
			if errors.Is(err, domain.ErrNotImplemented) {
				writeError(w, http.StatusNotImplemented, "not implemented")
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		httpJSON(w, http.StatusOK, toPaymentMethodResp(pm))
	}
}

// BillingAddPaymentMethod wires POST /v1/billing/payment-method. Body is
// validated by the BillingProvider — handler is a thin shell.
func BillingAddPaymentMethod(provider domain.BillingProvider, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.AddPaymentMethodReq
		if !decodeBody(w, r, &req) {
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		pm, err := provider.AttachCard(r.Context(), princ, domain.AddCardInput{
			CardNumber:   req.CardNumber,
			ExpMonth:     req.ExpMonth,
			ExpYear:      req.ExpYear,
			CVC:          req.CVC,
			PostalCode:   req.PostalCode,
			BillingEmail: req.BillingEmail,
		})
		if err != nil {
			if errors.Is(err, domain.ErrNotImplemented) {
				writeError(w, http.StatusNotImplemented, "not implemented")
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		auditWrite(r, aud, princ, "billing.payment_method_attached", pm.ID, map[string]any{
			"provider": pm.Provider,
			"brand":    pm.Brand,
			"last4":    pm.Last4,
		})
		writeJSON(w, http.StatusCreated, toPaymentMethodResp(pm))
	}
}

// BillingDeletePaymentMethod wires DELETE /v1/billing/payment-method.
func BillingDeletePaymentMethod(provider domain.BillingProvider, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		if err := provider.DetachCard(r.Context(), princ); err != nil {
			if errors.Is(err, domain.ErrNotImplemented) {
				writeError(w, http.StatusNotImplemented, "not implemented")
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		auditWrite(r, aud, princ, "billing.payment_method_detached", princ.OrgID, map[string]any{})
		w.WriteHeader(http.StatusNoContent)
	}
}

// BillingInvoices wires GET /v1/billing/invoices.
func BillingInvoices(billingRepo *repo.BillingRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		rows, err := billingRepo.ListInvoices(r.Context(), princ.OrgID)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]dto.InvoiceResp, 0, len(rows))
		for _, x := range rows {
			out = append(out, toInvoiceResp(x))
		}
		httpJSON(w, http.StatusOK, out)
	}
}

// BillingRecompute wires POST /v1/billing/invoices/recompute (dev only). It
// runs the InvoiceRoller's logic once on demand so demos can see invoices
// without waiting 24 hours. Gated by appEnv in the route registration.
func BillingRecompute(billingRepo *repo.BillingRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roller := &usecase.InvoiceRoller{Billing: billingRepo}
		if err := roller.Run(r.Context()); err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// BillingUsage wires GET /v1/billing/usage. Default window is the current
// calendar month; ?since=... and ?until=... override (RFC3339).
func BillingUsage(billingRepo *repo.BillingRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		now := time.Now().UTC()
		since := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		until := since.AddDate(0, 1, 0)
		if v := r.URL.Query().Get("since"); v != "" {
			if t, err := parseTimeFlexible(v); err == nil {
				since = t
			}
		}
		if v := r.URL.Query().Get("until"); v != "" {
			if t, err := parseTimeFlexible(v); err == nil {
				until = t
			}
		}
		breakdown, err := billingRepo.UsageSum(r.Context(), princ.OrgID, since, until)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		httpJSON(w, http.StatusOK, map[string]any{
			"since":        since.Format(time.RFC3339),
			"until":        until.Format(time.RFC3339),
			"total_cents":  breakdown.TotalCents,
			"by_workspace": breakdown.ByWorkspace,
			"by_kind":      breakdown.ByKind,
		})
	}
}

// parseTimeFlexible accepts RFC3339 or unix-seconds. Used by the usage
// window query string so quick-and-dirty curl calls work without TZ awareness.
func parseTimeFlexible(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Unix(n, 0).UTC(), nil
	}
	return time.Time{}, errors.New("invalid time")
}
