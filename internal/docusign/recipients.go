package docusign

import (
	"context"
	"fmt"
)

// RawRecipient holds the raw DocuSign recipient fields returned by the API.
type RawRecipient struct {
	RecipientID       string `json:"recipientId"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	Status            string `json:"status"`
	RoutingOrder      string `json:"routingOrder"`
	SignedDateTime    string `json:"signedDateTime"`
	DeliveredDateTime string `json:"deliveredDateTime"`
	DeclinedDateTime  string `json:"declinedDateTime"`
	DeclineReason     string `json:"declineReason"`
	Note              string `json:"note"`
	RoleName          string `json:"roleName"`
	ClientUserID      string `json:"clientUserId"`
	CreatedDateTime   string `json:"createdDateTime"`
}

// RecipientsResponse is the API response for envelope recipients.
type RecipientsResponse struct {
	Signers              []RawRecipient `json:"signers"`
	CarbonCopies         []RawRecipient `json:"carbonCopies"`
	CertifiedDeliveries  []RawRecipient `json:"certifiedDeliveries"`
	Editors              []RawRecipient `json:"editors"`
	Witnesses            []RawRecipient `json:"witnesses"`
}

// GetRecipients fetches all recipients for the given envelope.
func (c *DSClient) GetRecipients(ctx context.Context, accountID, envelopeID string) (*RecipientsResponse, error) {
	_, baseURI := c.auth.AccountInfo()
	if baseURI == "" {
		return nil, fmt.Errorf("docusign recipients: base URI not resolved; call ValidateCredentials first")
	}

	endpoint := fmt.Sprintf("%s/restapi/v2.1/accounts/%s/envelopes/%s/recipients", baseURI, accountID, envelopeID)

	var result RecipientsResponse
	if err := c.doRequest(ctx, "GET", endpoint, &result); err != nil {
		return nil, fmt.Errorf("docusign GetRecipients: %w", err)
	}
	return &result, nil
}
