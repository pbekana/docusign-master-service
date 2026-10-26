// Package handlers provides HTTP handlers for the DocuSign Master Service API.
package handlers

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"
)

//go:embed openapi.json
var openAPIFS embed.FS

// SwaggerHandler serves OpenAPI/Swagger documentation.
type SwaggerHandler struct{}

// NewSwaggerHandler creates a new SwaggerHandler.
func NewSwaggerHandler() *SwaggerHandler {
	return &SwaggerHandler{}
}

// GetSwaggerJSON returns the OpenAPI specification in JSON format.
func (h *SwaggerHandler) GetSwaggerJSON(w http.ResponseWriter, r *http.Request) {
	data, err := openAPIFS.ReadFile("openapi.json")
	if err != nil {
		http.Error(w, "OpenAPI specification not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// GetSwaggerUI serves the Swagger UI interface.
func (h *SwaggerHandler) GetSwaggerUI(w http.ResponseWriter, r *http.Request) {
	swaggerUI := `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>DocuSign Master Service - API Documentation</title>
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5.11.0/index.css" />
  <link rel="icon" type="image/png" href="https://unpkg.com/swagger-ui-dist@5.11.0/favicon-32x32.png" sizes="32x32" />
  <link rel="icon" type="image/png" href="https://unpkg.com/swagger-ui-dist@5.11.0/favicon-16x16.png" sizes="16x16" />
  <style>
    html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin: 0; background: #fafafa; }
    .swagger-ui .info { margin: 20px 0; }
    .swagger-ui .scheme-container { margin: 20px 0; padding: 20px 0; background: #f7f7f7; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" charset="UTF-8"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-standalone-preset.js" charset="UTF-8"></script>
  <script>
    window.onload = function() {
      const ui = SwaggerUIBundle({
        url: "/api/swagger.json",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        plugins: [
          SwaggerUIBundle.plugins.DownloadUrl
        ],
        layout: "StandaloneLayout",
        requestInterceptor: (req) => {
          // Add HMAC headers for protected endpoints
          if (req.url.includes('/api/') && !req.url.includes('/health') && !req.url.includes('/stats')) {
            const timestamp = Math.floor(Date.now() / 1000);
            const nonce = crypto.randomUUID();
            const bodyHash = req.body ? CryptoJS.SHA256(req.body).toString(CryptoJS.enc.Hex) : '';
            
            // These would need to be calculated with your actual HMAC secret
            // For demo purposes, we'll add placeholder headers
            req.headers['X-DS-Client-ID'] = 'coordinator';
            req.headers['X-DS-Timestamp'] = timestamp.toString();
            req.headers['X-DS-Nonce'] = nonce;
            req.headers['X-DS-Signature'] = 'demo-signature-placeholder';
          }
          return req;
        }
      });
      window.ui = ui;
    };
  </script>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/crypto-js/4.2.0/crypto-js.min.js"></script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(swaggerUI))
}

// GetRoutes returns a list of all available API routes.
func (h *SwaggerHandler) GetRoutes(w http.ResponseWriter, r *http.Request) {
	routes := map[string]interface{}{
		"api": "DocuSign Master Service v0.1.0",
		"endpoints": map[string][]map[string]string{
			"public": {
				{"method": "GET", "path": "/api/health", "description": "Health check"},
				{"method": "GET", "path": "/api/stats", "description": "Service statistics"},
				{"method": "GET", "path": "/api/swagger", "description": "Swagger UI documentation"},
				{"method": "GET", "path": "/api/swagger.json", "description": "OpenAPI specification"},
				{"method": "GET", "path": "/api/routes", "description": "List all API routes"},
			},
			"protected": {
				{"method": "POST", "path": "/api/validate-credentials", "description": "Validate DocuSign credentials"},
				{"method": "POST", "path": "/api/scan/start", "description": "Start a new data extraction job"},
				{"method": "GET", "path": "/api/scan/list", "description": "List all jobs"},
				{"method": "GET", "path": "/api/scan/statistics", "description": "Get job statistics"},
				{"method": "GET", "path": "/api/scan/{scan_id}/status", "description": "Get job status"},
				{"method": "POST", "path": "/api/scan/{scan_id}/cancel", "description": "Cancel a job"},
				{"method": "POST", "path": "/api/scan/{scan_id}/resume", "description": "Resume a failed/cancelled job"},
				{"method": "DELETE", "path": "/api/scan/{scan_id}/remove", "description": "Remove a job"},
				{"method": "GET", "path": "/api/normalization/supported-objects", "description": "Get supported DocuSign objects"},
				{"method": "POST", "path": "/api/normalization/{scan_id}/normalize", "description": "Normalize job data"},
				{"method": "GET", "path": "/api/normalization/{scan_id}/tables", "description": "List normalized tables"},
				{"method": "POST", "path": "/api/maintenance/cleanup", "description": "Clean up old jobs"},
				{"method": "POST", "path": "/api/maintenance/detect-crashed", "description": "Detect crashed jobs"},
				{"method": "GET", "path": "/api/audit/logs", "description": "Query audit logs"},
				{"method": "GET", "path": "/api/audit/stats", "description": "Get audit statistics"},
			},
		},
		"authentication": map[string]string{
			"type": "HMAC-SHA256",
			"description": "Protected endpoints require HMAC-SHA256 signed requests",
			"headers": "X-DS-Client-ID, X-DS-Timestamp, X-DS-Nonce, X-DS-Signature",
			"canonical_string": "METHOD\\nPATH\\nTIMESTAMP\\nNONCE\\nSHA256(BODY)",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(routes)
}

// GetAPIDocs returns basic API documentation.
func (h *SwaggerHandler) GetAPIDocs(w http.ResponseWriter, r *http.Request) {
	docs := `DocuSign Master Service API v0.1.0

API ENDPOINTS:

PUBLIC (No authentication required):
  GET  /api/health          - Health check
  GET  /api/stats           - Service statistics
  GET  /api/swagger         - Swagger UI documentation
  GET  /api/swagger.json    - OpenAPI specification
  GET  /api/routes          - List all API routes

PROTECTED (HMAC authentication required):
  POST /api/validate-credentials     - Validate DocuSign credentials
  POST /api/scan/start              - Start a new data extraction job
  GET  /api/scan/list               - List all jobs (paginated)
  GET  /api/scan/statistics         - Get job statistics
  GET  /api/scan/{scan_id}/status   - Get job status
  POST /api/scan/{scan_id}/cancel   - Cancel a job
  POST /api/scan/{scan_id}/resume   - Resume a failed/cancelled job
  DELETE /api/scan/{scan_id}/remove - Remove a job
  GET  /api/normalization/supported-objects - Get supported DocuSign objects
  POST /api/normalization/{scan_id}/normalize - Normalize job data
  GET  /api/normalization/{scan_id}/tables - List normalized tables
  POST /api/maintenance/cleanup     - Clean up old jobs
  POST /api/maintenance/detect-crashed - Detect crashed jobs
  GET  /api/audit/logs              - Query audit logs (paginated)
  GET  /api/audit/stats             - Get audit statistics

AUTHENTICATION:
  Protected endpoints require HMAC-SHA256 signed requests.
  Required headers:
    X-DS-Client-ID: Client identifier (coordinator/engineer)
    X-DS-Timestamp: Unix timestamp (seconds)
    X-DS-Nonce: Unique request identifier
    X-DS-Signature: HMAC-SHA256 signature

  Canonical string format:
    METHOD\nPATH\nTIMESTAMP\nNONCE\nSHA256(BODY)

  Example signing code in scripts/sign_request_example.go

ENVIRONMENT:
  Service is running at: http://localhost:8080
  MinIO: http://localhost:9001 (minioadmin/minioadmin)
  PostgreSQL: localhost:5433 (docusign/secret)

For full OpenAPI specification: GET /api/swagger.json
For interactive documentation: GET /api/swagger`

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(docs))
}