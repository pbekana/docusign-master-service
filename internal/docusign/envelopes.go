package docusign

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// RawEnvelope holds the raw DocuSign envelope fields returned by the API.
type RawEnvelope struct {
	EnvelopeID           string `json:"envelopeId"`
	Status               string `json:"status"`
	Subject              string `json:"emailSubject"`
	SenderName           string `json:"senderName"`
	SenderEmail          string `json:"senderEmail"`
	CreatedDateTime      string `json:"createdDateTime"`
	LastModifiedDateTime string `json:"lastModifiedDateTime"`
	SentDateTime         string `json:"sentDateTime"`
	CompletedDateTime    string `json:"completedDateTime"`
	ExpiresAfter         string `json:"expiresAfter"`
	DeclinedDateTime     string `json:"declinedDateTime"`
	VoidedDateTime       string `json:"voidedDateTime"`
	VoidedReason         string `json:"voidedReason"`
	EmailSubject         string `json:"emailSubject_raw"` // kept for normalization
	InitialSentDateTime  string `json:"initialSentDateTime"`
}

// EnvelopeListResponse is the API response for listing envelopes.
type EnvelopeListResponse struct {
	Envelopes     []RawEnvelope `json:"envelopes"`
	ResultSetSize string        `json:"resultSetSize"`
	TotalSetSize  string        `json:"totalSetSize"`
	EndPosition   string        `json:"endPosition"`
	NextUri       string        `json:"nextUri"`
}

// ListEnvelopes retrieves envelopes for accountID within the given date range.
// startPos is the 0-based start position for pagination; count is the page size.
func (c *DSClient) ListEnvelopes(ctx context.Context, accountID, fromDate, toDate string, startPos, count int) (*EnvelopeListResponse, error) {
	_, baseURI := c.auth.AccountInfo()
	if baseURI == "" {
		return nil, fmt.Errorf("docusign envelopes: base URI not resolved; call ValidateCredentials first")
	}

	params := url.Values{}
	params.Set("from_date", fromDate)
	if toDate != "" {
		params.Set("to_date", toDate)
	}
	params.Set("start_position", strconv.Itoa(startPos))
	params.Set("count", strconv.Itoa(count))
	params.Set("include", "recipients")

	endpoint := fmt.Sprintf("%s/restapi/v2.1/accounts/%s/envelopes?%s", baseURI, accountID, params.Encode())

	var result EnvelopeListResponse
	if err := c.doRequest(ctx, "GET", endpoint, &result); err != nil {
		return nil, fmt.Errorf("docusign ListEnvelopes: %w", err)
	}
	return &result, nil
}
