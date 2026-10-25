package docusign

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// RawTemplate holds the raw DocuSign template metadata returned by the API.
type RawTemplate struct {
	TemplateID           string `json:"templateId"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	Shared               string `json:"shared"`
	PageCount            string `json:"pageCount"`
	CreatedDateTime      string `json:"createdDateTime"`
	LastModifiedDateTime string `json:"lastModifiedDateTime"`
	FolderName           string `json:"folderName"`
	FolderID             string `json:"folderId"`
	Status               string `json:"status"`
}

// TemplatesResponse is the API response for listing templates.
type TemplatesResponse struct {
	EnvelopeTemplates []RawTemplate `json:"envelopeTemplates"`
	ResultSetSize     string        `json:"resultSetSize"`
	TotalSetSize      string        `json:"totalSetSize"`
	EndPosition       string        `json:"endPosition"`
}

// ListTemplates retrieves envelope templates for accountID with pagination.
func (c *DSClient) ListTemplates(ctx context.Context, accountID string, startPos, count int) (*TemplatesResponse, error) {
	_, baseURI := c.auth.AccountInfo()
	if baseURI == "" {
		return nil, fmt.Errorf("docusign templates: base URI not resolved; call ValidateCredentials first")
	}

	params := url.Values{}
	params.Set("start_position", strconv.Itoa(startPos))
	params.Set("count", strconv.Itoa(count))

	endpoint := fmt.Sprintf("%s/restapi/v2.1/accounts/%s/templates?%s", baseURI, accountID, params.Encode())

	var result TemplatesResponse
	if err := c.doRequest(ctx, "GET", endpoint, &result); err != nil {
		return nil, fmt.Errorf("docusign ListTemplates: %w", err)
	}
	return &result, nil
}
