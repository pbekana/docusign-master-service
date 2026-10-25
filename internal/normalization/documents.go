package normalization

import (
	"strconv"
	"time"

	"github.com/your-org/docusign-master-service/internal/docusign"
)

// NormalizedDocument is the canonical representation of a DocuSign envelope document.
type NormalizedDocument struct {
	DocumentID     string     `parquet:"document_id"     json:"document_id"`
	EnvelopeID     string     `parquet:"envelope_id"     json:"envelope_id"`
	AccountID      string     `parquet:"account_id"      json:"account_id"`
	OrganizationID string     `parquet:"organization_id" json:"organization_id"`
	Name           string     `parquet:"name"            json:"name"`
	DocumentType   string     `parquet:"document_type"   json:"document_type"`
	FileExtension  *string    `parquet:"file_extension"  json:"file_extension"`
	PageCount      *int       `parquet:"page_count"      json:"page_count"`
	Order          *int       `parquet:"order"           json:"order"`
	URI            string     `parquet:"uri"             json:"uri"`
	CreatedAt      *time.Time `parquet:"created_at"      json:"created_at"`
}

// NormalizeDocuments converts a DocumentsResponse into a slice of NormalizedDocument.
func NormalizeDocuments(raw docusign.DocumentsResponse, envelopeID, accountID, orgID string) []NormalizedDocument {
	result := make([]NormalizedDocument, 0, len(raw.EnvelopeDocuments))
	for _, d := range raw.EnvelopeDocuments {
		result = append(result, normalizeDocument(d, envelopeID, accountID, orgID))
	}
	return result
}

func normalizeDocument(d docusign.RawDocument, envelopeID, accountID, orgID string) NormalizedDocument {
	var pageCount *int
	if n, err := strconv.Atoi(d.PageCount); err == nil {
		v := n
		pageCount = &v
	}
	var order *int
	if n, err := strconv.Atoi(d.Order); err == nil {
		v := n
		order = &v
	}
	return NormalizedDocument{
		DocumentID:     d.DocumentID,
		EnvelopeID:     envelopeID,
		AccountID:      accountID,
		OrganizationID: orgID,
		Name:           d.Name,
		DocumentType:   d.Type,
		FileExtension:  strPtr(d.FileExtension),
		PageCount:      pageCount,
		Order:          order,
		URI:            d.URI,
		CreatedAt:      nil, // RawDocument has no createdDateTime field
	}
}
