package normalization

import (
	"testing"
	"time"

	"github.com/your-org/docusign-master-service/internal/docusign"
)

func TestNormalizeRecipients_Flatten(t *testing.T) {
	raw := docusign.RecipientsResponse{
		Signers: []docusign.RawRecipient{
			{
				RecipientID:       "r-001",
				Name:              "Bob Jones",
				Email:             "bob@example.com",
				Status:            "completed",
				RoutingOrder:      "1",
				SignedDateTime:    "2024-01-15T12:00:00Z",
				DeliveredDateTime: "2024-01-15T11:00:00Z",
				RoleName:          "Approver",
				CreatedDateTime:   "2024-01-15T10:00:00Z",
			},
		},
		CarbonCopies: []docusign.RawRecipient{
			{
				RecipientID:  "r-002",
				Name:         "Carol White",
				Email:        "carol@example.com",
				Status:       "sent",
				RoutingOrder: "2",
			},
		},
	}

	got := NormalizeRecipients(raw, "env-001", "acc-123", "org-456")

	if len(got) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(got))
	}

	// First recipient — signer.
	s := got[0]
	if s.RecipientType != "signer" {
		t.Errorf("type: got %q, want %q", s.RecipientType, "signer")
	}
	if s.RecipientID != "r-001" {
		t.Errorf("RecipientID: got %q", s.RecipientID)
	}
	if s.EnvelopeID != "env-001" {
		t.Errorf("EnvelopeID: got %q", s.EnvelopeID)
	}
	if s.RoutingOrder != 1 {
		t.Errorf("RoutingOrder: got %d, want 1", s.RoutingOrder)
	}
	wantSigned := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	if s.SignedAt == nil || !s.SignedAt.Equal(wantSigned) {
		t.Errorf("SignedAt: got %v, want %v", s.SignedAt, wantSigned)
	}
	if s.RoleName == nil || *s.RoleName != "Approver" {
		t.Errorf("RoleName: got %v", s.RoleName)
	}

	// Second recipient — carbon_copy.
	cc := got[1]
	if cc.RecipientType != "carbon_copy" {
		t.Errorf("type: got %q, want %q", cc.RecipientType, "carbon_copy")
	}
	if cc.SignedAt != nil {
		t.Errorf("SignedAt: expected nil for cc, got %v", cc.SignedAt)
	}
}

func TestNormalizeRecipients_OptionalNilFields(t *testing.T) {
	raw := docusign.RecipientsResponse{
		Signers: []docusign.RawRecipient{
			{
				RecipientID:  "r-003",
				Name:         "Dan Lee",
				Email:        "dan@example.com",
				Status:       "sent",
				RoutingOrder: "0",
				// All optional fields empty.
			},
		},
	}

	got := NormalizeRecipients(raw, "env-002", "acc-123", "org-456")
	if len(got) != 1 {
		t.Fatalf("expected 1 recipient, got %d", len(got))
	}
	r := got[0]
	if r.DeclineReason != nil {
		t.Errorf("DeclineReason: expected nil")
	}
	if r.Note != nil {
		t.Errorf("Note: expected nil")
	}
	if r.ClientUserID != nil {
		t.Errorf("ClientUserID: expected nil")
	}
	if r.CreatedAt != nil {
		t.Errorf("CreatedAt: expected nil")
	}
}
