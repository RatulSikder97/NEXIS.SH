package domain

import "time"

// NASATLXResponse is one row in nasa_tlx_responses. The six subscales follow
// the standard NASA Task Load Index 0..20 scale (10cm bar / 0.5cm increments).
// RecoveryRunID is the optional cross-reference to a workflow_runs.id so the
// survey result can be correlated to the recovery the user just observed.
//
// Org and user are nullable on the table so the survey can record an
// anonymous response if the front-end omits them, but the HTTP handler always
// fills them from the authenticated principal — the nullability is for
// admin-pool / migration paths.
type NASATLXResponse struct {
	ID             string
	UserID         *string
	OrgID          *string
	RecoveryRunID  *string
	MentalDemand   int
	PhysicalDemand int
	TemporalDemand int
	Performance    int
	Effort         int
	Frustration    int
	Notes          string
	CreatedAt      time.Time
}
