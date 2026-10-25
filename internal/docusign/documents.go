package docusign

import (
	"context"
	"fmt"
)

// RawDocument holds the raw DocuSign document metadata returned by the API.
type RawDocument struct {
	DocumentID    string `json:"documentId"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	FileExtension string `json:"fileExtension"`
	PageCount     string `json:"pageCount"`
	Order         string `json:"order"`
	URI           string `json:"uri"`
}

// DocumentsResponse is the API response for the envelope document list.
type DocumentsResponse struct {
	EnvelopeDocuments []RawDocument `json:"envelopeDocuments"`
}

// GetDocuments fetches the document list for the given envelope.
func (c *DSClient) GetDocuments(ctx context.Context, accountID, envelopeID string) (*DocumentsResponse, error) {
	_, baseURI := c.auth.AccountInfo()
	if baseURI == "" {
		return nil, fmt.Errorf("docusign documents: base URI not resolved; call ValidateCredentials first")
	}

	endpoint := fmt.Sprintf("%s/restapi/v2.1/accounts/%s/envelopes/%s/documents", baseURI, accountID, envelopeID)

	var result DocumentsResponse
	if err := c.doRequest(ctx, "GET", endpoint, &result); err != nil {
		return nil, fmt.Errorf("docusign GetDocuments: %w", err)
	}
	return &result, nil
}
