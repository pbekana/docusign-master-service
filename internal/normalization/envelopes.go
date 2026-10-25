package normalization

import (
	"time"

	"github.com/your-org/docusign-master-service/internal/docusign"
)

// NormalizedEnvelope is the canonical representation of a DocuSign envelope,
// suitable for Parquet output and JSON serialisation.
type NormalizedEnvelope struct {
	EnvelopeID     string     `parquet:"envelope_id"      json:"envelope_id"`
	AccountID      string     `parquet:"account_id"       json:"account_id"`
	OrganizationID string     `parquet:"organization_id"  json:"organization_id"`
	Status         string     `parquet:"status"           json:"status"`
	Subject        string     `parquet:"subject"          json:"subject"`
	SenderName     string     `parquet:"sender_name"      json:"sender_name"`
	SenderEmail    string     `parquet:"sender_email"     json:"sender_email"`
	CreatedAt      *time.Time `parquet:"created_at"       json:"created_at"`
	LastModifiedAt *time.Time `parquet:"last_modified_at" json:"last_modified_at"`
	SentAt         *time.Time `parquet:"sent_at"          json:"sent_at"`
	CompletedAt    *time.Time `parquet:"completed_at"     json:"completed_at"`
	ExpiresAt      *time.Time `parquet:"expires_at"       json:"expires_at"`
	DeclinedAt     *time.Time `parquet:"declined_at"      json:"declined_at"`
	VoidedAt       *time.Time `parquet:"voided_at"        json:"voided_at"`
	VoidedReason   *string    `parquet:"voided_reason"    json:"voided_reason"`
	EmailSubject   *string    `parquet:"email_subject"    json:"email_subject"`
	InitialSentAt  *time.Time `parquet:"initial_sent_at"  json:"initial_sent_at"`
	IsTemplate     bool       `parquet:"is_template"      json:"is_template"`
}

// NormalizeEnvelope converts a raw DocuSign envelope into a NormalizedEnvelope.
func NormalizeEnvelope(raw docusign.RawEnvelope, accountID, orgID string) NormalizedEnvelope {
	return NormalizedEnvelope{
		EnvelopeID:     raw.EnvelopeID,
		AccountID:      accountID,
		OrganizationID: orgID,
		Status:         raw.Status,
		Subject:        raw.Subject,
		SenderName:     raw.SenderName,
		SenderEmail:    raw.SenderEmail,
		CreatedAt:      parseTime(raw.CreatedDateTime),
		LastModifiedAt: parseTime(raw.LastModifiedDateTime),
		SentAt:         parseTime(raw.SentDateTime),
		CompletedAt:    parseTime(raw.CompletedDateTime),
		ExpiresAt:      parseTime(raw.ExpiresAfter),
		DeclinedAt:     parseTime(raw.DeclinedDateTime),
		VoidedAt:       parseTime(raw.VoidedDateTime),
		VoidedReason:   strPtr(raw.VoidedReason),
		EmailSubject:   strPtr(raw.EmailSubject),
		InitialSentAt:  parseTime(raw.InitialSentDateTime),
		IsTemplate:     false,
	}
}
