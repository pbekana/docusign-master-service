package normalization

import (
	"testing"
	"time"

	"github.com/your-org/docusign-master-service/internal/docusign"
)

func TestNormalizeEnvelope(t *testing.T) {
	raw := docusign.RawEnvelope{
		EnvelopeID:           "env-001",
		Status:               "completed",
		Subject:              "Please sign this",
		SenderName:           "Alice Smith",
		SenderEmail:          "alice@example.com",
		CreatedDateTime:      "2024-01-15T10:00:00Z",
		LastModifiedDateTime: "2024-01-15T11:00:00Z",
		SentDateTime:         "2024-01-15T10:05:00Z",
		CompletedDateTime:    "2024-01-15T12:00:00Z",
		ExpiresAfter:         "",
		DeclinedDateTime:     "",
		VoidedDateTime:       "",
		VoidedReason:         "",
		EmailSubject:         "Contract Agreement",
		InitialSentDateTime:  "2024-01-15T10:04:00Z",
	}

	got := NormalizeEnvelope(raw, "acc-123", "org-456")

	if got.EnvelopeID != "env-001" {
		t.Errorf("EnvelopeID: got %q, want %q", got.EnvelopeID, "env-001")
	}
	if got.AccountID != "acc-123" {
		t.Errorf("AccountID: got %q, want %q", got.AccountID, "acc-123")
	}
	if got.OrganizationID != "org-456" {
		t.Errorf("OrganizationID: got %q, want %q", got.OrganizationID, "org-456")
	}
	if got.Status != "completed" {
		t.Errorf("Status: got %q, want %q", got.Status, "completed")
	}
	if got.SenderEmail != "alice@example.com" {
		t.Errorf("SenderEmail: got %q", got.SenderEmail)
	}

	// Time fields.
	wantCreated := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	if got.CreatedAt == nil || !got.CreatedAt.Equal(wantCreated) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, wantCreated)
	}

	wantCompleted := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	if got.CompletedAt == nil || !got.CompletedAt.Equal(wantCompleted) {
		t.Errorf("CompletedAt: got %v, want %v", got.CompletedAt, wantCompleted)
	}

	// Nil optional fields.
	if got.VoidedAt != nil {
		t.Errorf("VoidedAt: expected nil, got %v", got.VoidedAt)
	}
	if got.VoidedReason != nil {
		t.Errorf("VoidedReason: expected nil, got %v", *got.VoidedReason)
	}
	if got.ExpiresAt != nil {
		t.Errorf("ExpiresAt: expected nil, got %v", got.ExpiresAt)
	}

	// Email subject pointer.
	if got.EmailSubject == nil || *got.EmailSubject != "Contract Agreement" {
		t.Errorf("EmailSubject: expected 'Contract Agreement', got %v", got.EmailSubject)
	}

	// IsTemplate default.
	if got.IsTemplate {
		t.Error("IsTemplate: expected false")
	}
}

func TestNormalizeEnvelope_VoidedFields(t *testing.T) {
	raw := docusign.RawEnvelope{
		EnvelopeID:     "env-002",
		Status:         "voided",
		VoidedDateTime: "2024-03-20T09:30:00Z",
		VoidedReason:   "Sent in error",
	}

	got := NormalizeEnvelope(raw, "acc-123", "org-456")

	if got.VoidedAt == nil {
		t.Fatal("VoidedAt: expected non-nil")
	}
	wantVoided := time.Date(2024, 3, 20, 9, 30, 0, 0, time.UTC)
	if !got.VoidedAt.Equal(wantVoided) {
		t.Errorf("VoidedAt: got %v, want %v", got.VoidedAt, wantVoided)
	}
	if got.VoidedReason == nil || *got.VoidedReason != "Sent in error" {
		t.Errorf("VoidedReason: got %v", got.VoidedReason)
	}
}

func TestNormalizeEnvelope_InvalidTime(t *testing.T) {
	raw := docusign.RawEnvelope{
		EnvelopeID:      "env-003",
		CreatedDateTime: "not-a-date",
	}
	got := NormalizeEnvelope(raw, "acc", "org")
	if got.CreatedAt != nil {
		t.Errorf("expected nil for unparseable time, got %v", got.CreatedAt)
	}
}
