package dto

// AddPaymentMethodReq is the body accepted by POST /v1/billing/payment-method.
// CardNumber and CVC are at the BillingProvider boundary only — they never
// hit the repo. The local provider strips spaces from CardNumber before
// validation.
type AddPaymentMethodReq struct {
	CardNumber   string `json:"card_number"`
	ExpMonth     int    `json:"exp_month"`
	ExpYear      int    `json:"exp_year"`
	CVC          string `json:"cvc"`
	PostalCode   string `json:"postal_code"`
	BillingEmail string `json:"billing_email"`
}

// PaymentMethodResp is the wire shape returned by GET / POST
// /v1/billing/payment-method. Provider name + brand + last4 are surfaced so
// the dashboard can render "Visa ••••4242" without round-tripping.
type PaymentMethodResp struct {
	ID                      string `json:"id"`
	OrgID                   string `json:"org_id"`
	Provider                string `json:"provider"`
	Brand                   string `json:"brand"`
	Last4                   string `json:"last4"`
	ExpMonth                int    `json:"exp_month"`
	ExpYear                 int    `json:"exp_year"`
	BillingEmail            string `json:"billing_email,omitempty"`
	ExternalCustomerID      string `json:"external_customer_id,omitempty"`
	ExternalPaymentMethodID string `json:"external_payment_method_id,omitempty"`
	CreatedAt               string `json:"created_at"`
}

// SetupIntentResp is the response to POST /v1/billing/payment-method/intent.
// ClientSecret is the opaque value the browser hands to Stripe.js to confirm
// the SetupIntent; for the local provider it is a synthetic "seti_local_..."
// string that the dashboard treats identically.
type SetupIntentResp struct {
	ClientSecret string `json:"client_secret"`
}

// ConfirmSetupIntentReq is the body of POST /v1/billing/payment-method/confirm.
// PaymentMethod is the pm_... id the browser obtained from Stripe.js after a
// successful SetupIntent confirmation.
type ConfirmSetupIntentReq struct {
	PaymentMethod string `json:"payment_method"`
	BillingEmail  string `json:"billing_email,omitempty"`
}

// InvoiceResp is one row in GET /v1/billing/invoices.
type InvoiceResp struct {
	ID          string `json:"id"`
	OrgID       string `json:"org_id"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	TotalCents  int64  `json:"total_cents"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}
