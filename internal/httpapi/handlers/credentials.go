package handlers

import (
	"encoding/json"
	"net/http"
)

// CredentialsHandler serves the credential validation endpoint.
type CredentialsHandler struct {
	dsAuth DocuSignAuth
}

// NewCredentialsHandler creates a CredentialsHandler.
func NewCredentialsHandler(dsAuth DocuSignAuth) *CredentialsHandler {
	return &CredentialsHandler{dsAuth: dsAuth}
}

// validateCredentialsRequest holds optional overrides for the validation call.
type validateCredentialsRequest struct {
	DocusignAccountID string `json:"docusign_account_id"`
	DocusignUserID    string `json:"docusign_user_id"`
}

// PostValidateCredentials handles POST /api/validate-credentials.
func (h *CredentialsHandler) PostValidateCredentials(w http.ResponseWriter, r *http.Request) {
	var req validateCredentialsRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}

	// Validate credentials (fields from request body are for documentation /
	// future extension; the auth client uses its configured credentials).
	err := h.dsAuth.ValidateCredentials(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	accountID, baseURI := h.dsAuth.AccountInfo()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":      true,
		"account_id": accountID,
		"base_uri":   baseURI,
	})
}
