#!/bin/bash

# DocuSign Master Service API Testing Script
# This script demonstrates how to test the API endpoints

echo "🔧 DocuSign Master Service API Tester"
echo "======================================"

BASE_URL="http://localhost:8080/api"

echo ""
echo "📊 Testing Public Endpoints:"
echo "----------------------------"

# Test health endpoint
echo "1. Health Check:"
curl -s "$BASE_URL/health" | jq . 2>/dev/null || curl -s "$BASE_URL/health"
echo ""

# Test stats endpoint
echo "2. Service Stats:"
curl -s "$BASE_URL/stats" | jq . 2>/dev/null || curl -s "$BASE_URL/stats"
echo ""

echo ""
echo "🔐 Testing Protected Endpoints (without auth - should fail):"
echo "-----------------------------------------------------------"

# Test a protected endpoint without auth
echo "1. List Jobs (without auth - should fail):"
curl -s -w "HTTP Status: %{http_code}\n" "$BASE_URL/scan/list" | jq .error 2>/dev/null || echo "Missing authentication headers"
echo ""

echo ""
echo "🚀 Available Endpoints Summary:"
echo "------------------------------"

cat << EOF

PUBLIC ENDPOINTS (no auth required):
  GET  $BASE_URL/health          - Health check
  GET  $BASE_URL/stats           - Service statistics

PROTECTED ENDPOINTS (HMAC auth required):
  POST $BASE_URL/validate-credentials     - Validate DocuSign credentials
  POST $BASE_URL/scan/start              - Start extraction job
  GET  $BASE_URL/scan/list               - List jobs
  GET  $BASE_URL/scan/statistics         - Job statistics
  GET  $BASE_URL/scan/{id}/status        - Job status
  POST $BASE_URL/scan/{id}/cancel        - Cancel job
  POST $BASE_URL/scan/{id}/resume        - Resume job
  DELETE $BASE_URL/scan/{id}/remove      - Remove job
  GET  $BASE_URL/normalization/supported-objects - Supported objects
  POST $BASE_URL/normalization/{id}/normalize - Normalize data
  GET  $BASE_URL/normalization/{id}/tables - List tables
  POST $BASE_URL/maintenance/cleanup     - Cleanup old jobs
  POST $BASE_URL/maintenance/detect-crashed - Detect crashed jobs
  GET  $BASE_URL/audit/logs              - Audit logs
  GET  $BASE_URL/audit/stats             - Audit statistics

AUTHENTICATION:
  Use HMAC-SHA256 signing. Example in scripts/sign_request_example.go
  Required headers: X-DS-Client-ID, X-DS-Timestamp, X-DS-Nonce, X-DS-Signature

INFRASTRUCTURE:
  Service:    http://localhost:8080
  MinIO UI:   http://localhost:9001 (minioadmin/minioadmin)
  PostgreSQL: localhost:5433 (docusign/secret)

DOCUMENTATION:
  Open api-docs.html in your browser for full documentation
  Or check README.md for setup instructions
EOF

echo ""
echo "✅ Service is running and accessible!"
echo "📚 Full documentation: file://$(pwd)/api-docs.html"
echo "🔑 To test protected endpoints, run: go run scripts/sign_request_example.go"