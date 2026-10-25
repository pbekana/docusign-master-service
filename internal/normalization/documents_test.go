package normalization

import (
	"testing"

	"github.com/your-org/docusign-master-service/internal/docusign"
)

func TestNormalizeDocuments(t *testing.T) {
	raw := docusign.DocumentsResponse{
		EnvelopeDocuments: []docusign.RawDocument{
			{
				DocumentID:    "doc-001",
				Name:          "Contract.pdf",
				Type:          "content",
				FileExtension: "pdf",
				PageCount:     "5",
				Order:         "1",
				URI:           "/restapi/v2.1/accounts/acc/envelopes/env/documents/doc-001",
			},
			{
				DocumentID:    "doc-002",
				Name:          "Summary",
				Type:          "summary",
				FileExtension: "",
				PageCount:     "",
				Order:         "",
				URI:           "/restapi/v2.1/accounts/acc/envelopes/env/documents/doc-002",
			},
		},
	}

	got := NormalizeDocuments(raw, "env-001", "acc-123", "org-456")

	if len(got) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(got))
	}

	d1 := got[0]
	if d1.DocumentID != "doc-001" {
		t.Errorf("DocumentID: got %q", d1.DocumentID)
	}
	if d1.EnvelopeID != "env-001" {
		t.Errorf("EnvelopeID: got %q", d1.EnvelopeID)
	}
	if d1.DocumentType != "content" {
		t.Errorf("DocumentType: got %q", d1.DocumentType)
	}
	if d1.FileExtension == nil || *d1.FileExtension != "pdf" {
		t.Errorf("FileExtension: got %v", d1.FileExtension)
	}
	if d1.PageCount == nil || *d1.PageCount != 5 {
		t.Errorf("PageCount: got %v", d1.PageCount)
	}
	if d1.Order == nil || *d1.Order != 1 {
		t.Errorf("Order: got %v", d1.Order)
	}

	// Second document — optional fields nil.
	d2 := got[1]
	if d2.FileExtension != nil {
		t.Errorf("FileExtension: expected nil, got %v", *d2.FileExtension)
	}
	if d2.PageCount != nil {
		t.Errorf("PageCount: expected nil, got %v", *d2.PageCount)
	}
	if d2.Order != nil {
		t.Errorf("Order: expected nil, got %v", *d2.Order)
	}
}
