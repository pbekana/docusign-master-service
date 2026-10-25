package normalization

import (
	"strconv"
	"strings"
	"time"

	"github.com/your-org/docusign-master-service/internal/docusign"
)

// NormalizedTemplate is the canonical representation of a DocuSign envelope template.
type NormalizedTemplate struct {
	TemplateID     string     `parquet:"template_id"     json:"template_id"`
	AccountID      string     `parquet:"account_id"      json:"account_id"`
	OrganizationID string     `parquet:"organization_id" json:"organization_id"`
	Name           string     `parquet:"name"            json:"name"`
	Description    *string    `parquet:"description"     json:"description"`
	Shared         bool       `parquet:"shared"          json:"shared"`
	PageCount      *int       `parquet:"page_count"      json:"page_count"`
	CreatedAt      *time.Time `parquet:"created_at"      json:"created_at"`
	LastModifiedAt *time.Time `parquet:"last_modified_at" json:"last_modified_at"`
	FolderName     *string    `parquet:"folder_name"     json:"folder_name"`
	FolderID       *string    `parquet:"folder_id"       json:"folder_id"`
	Status         string     `parquet:"status"          json:"status"`
}

// NormalizeTemplate converts a RawTemplate into a NormalizedTemplate.
func NormalizeTemplate(raw docusign.RawTemplate, accountID, orgID string) NormalizedTemplate {
	var pageCount *int
	if n, err := strconv.Atoi(raw.PageCount); err == nil {
		v := n
		pageCount = &v
	}

	shared := strings.EqualFold(raw.Shared, "true")

	return NormalizedTemplate{
		TemplateID:     raw.TemplateID,
		AccountID:      accountID,
		OrganizationID: orgID,
		Name:           raw.Name,
		Description:    strPtr(raw.Description),
		Shared:         shared,
		PageCount:      pageCount,
		CreatedAt:      parseTime(raw.CreatedDateTime),
		LastModifiedAt: parseTime(raw.LastModifiedDateTime),
		FolderName:     strPtr(raw.FolderName),
		FolderID:       strPtr(raw.FolderID),
		Status:         raw.Status,
	}
}
