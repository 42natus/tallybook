package settle

// ChannelStatus is the lifecycle status of a payment channel.
// Legal values are enforced by the CHECK constraint on channels.status
// (internal/store/migrations/0004_channels.sql).
type ChannelStatus string

const (
	ChannelStatusOpen     ChannelStatus = "open"
	ChannelStatusClosing  ChannelStatus = "closing"
	ChannelStatusClosed   ChannelStatus = "closed"
	ChannelStatusRefunded ChannelStatus = "refunded"
)

// SettlementStatus is the lifecycle status of a settlement.
// Legal values are enforced by the CHECK constraint on settlements.status
// (internal/store/migrations/0006_settlements.sql).
type SettlementStatus string

const (
	SettlementStatusSubmitting SettlementStatus = "submitting"
	SettlementStatusConfirmed  SettlementStatus = "confirmed"
	SettlementStatusFailed     SettlementStatus = "failed"
)

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
