package meter

// PeriodStatus is the lifecycle status of a billing period.
// Legal values are enforced by the CHECK constraint on periods.status
// (internal/store/migrations/0001_periods.sql).
type PeriodStatus string

const (
	PeriodStatusOpen     PeriodStatus = "open"
	PeriodStatusClosed   PeriodStatus = "closed"
	PeriodStatusAnchored PeriodStatus = "anchored"
)

// AnchorStatus is the lifecycle status of an anchor attempt.
// Legal values are enforced by the CHECK constraint on anchor_attempts.status
// (internal/store/migrations/0007_anchor_attempts.sql).
type AnchorStatus string

const (
	AnchorStatusSubmitting AnchorStatus = "submitting"
	AnchorStatusConfirmed  AnchorStatus = "confirmed"
	AnchorStatusFailed     AnchorStatus = "failed"
)
