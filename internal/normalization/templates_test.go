package normalization

import (
	"testing"
	"time"

	"github.com/your-org/docusign-master-service/internal/docusign"
)

func TestNormalizeTemplate(t *testing.T) {
	raw := docusign.RawTemplate{
		TemplateID:           "tmpl-001",
		Name:                 "NDA Template",
		Description:          "Non-disclosure agreement",
		Shared:               "true",
		PageCount:            "3",
		CreatedDateTime:      "2023-06-01T09:00:00Z",
		LastModifiedDateTime: "2024-01-10T15:30:00Z",
		FolderName:           "Legal",
		FolderID:             "folder-001",
		Status:               "active",
	}

	got := NormalizeTemplate(raw, "acc-123", "org-456")

	if got.TemplateID != "tmpl-001" {
		t.Errorf("TemplateID: got %q", got.TemplateID)
	}
	if got.AccountID != "acc-123" {
		t.Errorf("AccountID: got %q", got.AccountID)
	}
	if got.OrganizationID != "org-456" {
		t.Errorf("OrganizationID: got %q", got.OrganizationID)
	}
	if got.Name != "NDA Template" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.Description == nil || *got.Description != "Non-disclosure agreement" {
		t.Errorf("Description: got %v", got.Description)
	}
	if !got.Shared {
		t.Error("Shared: expected true")
	}
	if got.PageCount == nil || *got.PageCount != 3 {
		t.Errorf("PageCount: got %v", got.PageCount)
	}

	wantCreated := time.Date(2023, 6, 1, 9, 0, 0, 0, time.UTC)
	if got.CreatedAt == nil || !got.CreatedAt.Equal(wantCreated) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, wantCreated)
	}

	wantModified := time.Date(2024, 1, 10, 15, 30, 0, 0, time.UTC)
	if got.LastModifiedAt == nil || !got.LastModifiedAt.Equal(wantModified) {
		t.Errorf("LastModifiedAt: got %v, want %v", got.LastModifiedAt, wantModified)
	}

	if got.FolderName == nil || *got.FolderName != "Legal" {
		t.Errorf("FolderName: got %v", got.FolderName)
	}
	if got.FolderID == nil || *got.FolderID != "folder-001" {
		t.Errorf("FolderID: got %v", got.FolderID)
	}
	if got.Status != "active" {
		t.Errorf("Status: got %q", got.Status)
	}
}

func TestNormalizeTemplate_OptionalNil(t *testing.T) {
	raw := docusign.RawTemplate{
		TemplateID: "tmpl-002",
		Name:       "Minimal Template",
		Shared:     "false",
		Status:     "draft",
	}

	got := NormalizeTemplate(raw, "acc-123", "org-456")

	if got.Description != nil {
		t.Errorf("Description: expected nil, got %v", *got.Description)
	}
	if got.PageCount != nil {
		t.Errorf("PageCount: expected nil, got %d", *got.PageCount)
	}
	if got.CreatedAt != nil {
		t.Errorf("CreatedAt: expected nil, got %v", got.CreatedAt)
	}
	if got.FolderName != nil {
		t.Errorf("FolderName: expected nil, got %v", *got.FolderName)
	}
	if got.FolderID != nil {
		t.Errorf("FolderID: expected nil, got %v", *got.FolderID)
	}
	if got.Shared {
		t.Error("Shared: expected false")
	}
}
