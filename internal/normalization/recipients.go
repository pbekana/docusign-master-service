package normalization

import (
	"strconv"
	"time"

	"github.com/your-org/docusign-master-service/internal/docusign"
)

// NormalizedRecipient is the canonical representation of a DocuSign recipient.
type NormalizedRecipient struct {
	RecipientID    string     `parquet:"recipient_id"    json:"recipient_id"`
	EnvelopeID     string     `parquet:"envelope_id"     json:"envelope_id"`
	AccountID      string     `parquet:"account_id"      json:"account_id"`
	OrganizationID string     `parquet:"organization_id" json:"organization_id"`
	RecipientType  string     `parquet:"recipient_type"  json:"recipient_type"`
	Name           string     `parquet:"name"            json:"name"`
	Email          string     `parquet:"email"           json:"email"`
	Status         string     `parquet:"status"          json:"status"`
	RoutingOrder   int        `parquet:"routing_order"   json:"routing_order"`
	SignedAt       *time.Time `parquet:"signed_at"       json:"signed_at"`
	DeliveredAt    *time.Time `parquet:"delivered_at"    json:"delivered_at"`
	DeclinedAt     *time.Time `parquet:"declined_at"     json:"declined_at"`
	DeclineReason  *string    `parquet:"decline_reason"  json:"decline_reason"`
	Note           *string    `parquet:"note"            json:"note"`
	RoleName       *string    `parquet:"role_name"       json:"role_name"`
	ClientUserID   *string    `parquet:"client_user_id"  json:"client_user_id"`
	CreatedAt      *time.Time `parquet:"created_at"      json:"created_at"`
}

// NormalizeRecipients flattens all recipient types from a RecipientsResponse
// into a slice of NormalizedRecipient, preserving type information.
func NormalizeRecipients(raw docusign.RecipientsResponse, envelopeID, accountID, orgID string) []NormalizedRecipient {
	var result []NormalizedRecipient

	type typedList struct {
		recipientType string
		list          []docusign.RawRecipient
	}
	groups := []typedList{
		{"signer", raw.Signers},
		{"carbon_copy", raw.CarbonCopies},
		{"certified_delivery", raw.CertifiedDeliveries},
		{"editor", raw.Editors},
		{"witness", raw.Witnesses},
	}

	for _, g := range groups {
		for _, r := range g.list {
			result = append(result, normalizeRecipient(r, g.recipientType, envelopeID, accountID, orgID))
		}
	}
	return result
}

func normalizeRecipient(r docusign.RawRecipient, recipientType, envelopeID, accountID, orgID string) NormalizedRecipient {
	routingOrder := 0
	if n, err := strconv.Atoi(r.RoutingOrder); err == nil {
		routingOrder = n
	}
	return NormalizedRecipient{
		RecipientID:    r.RecipientID,
		EnvelopeID:     envelopeID,
		AccountID:      accountID,
		OrganizationID: orgID,
		RecipientType:  recipientType,
		Name:           r.Name,
		Email:          r.Email,
		Status:         r.Status,
		RoutingOrder:   routingOrder,
		SignedAt:       parseTime(r.SignedDateTime),
		DeliveredAt:    parseTime(r.DeliveredDateTime),
		DeclinedAt:     parseTime(r.DeclinedDateTime),
		DeclineReason:  strPtr(r.DeclineReason),
		Note:           strPtr(r.Note),
		RoleName:       strPtr(r.RoleName),
		ClientUserID:   strPtr(r.ClientUserID),
		CreatedAt:      parseTime(r.CreatedDateTime),
	}
}
