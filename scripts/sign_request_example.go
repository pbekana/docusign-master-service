// Command sign_request_example demonstrates how to build a correctly signed
// DocuSign Master Service request using HMAC-SHA256.
//
// Usage:
//
//	HMAC_SECRET_KEY_CORE=mysecret go run scripts/sign_request_example.go [METHOD] [PATH] [BODY_JSON]
//
// Example:
//
//	HMAC_SECRET_KEY_CORE=mysecret go run scripts/sign_request_example.go POST /api/scan/start '{"organization_id":"org-1"}'
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	secret := os.Getenv("HMAC_SECRET_KEY_CORE")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "HMAC_SECRET_KEY_CORE environment variable is required")
		os.Exit(1)
	}

	method := "GET"
	path := "/api/health"
	bodyJSON := ""

	if len(os.Args) > 1 {
		method = strings.ToUpper(os.Args[1])
	}
	if len(os.Args) > 2 {
		path = os.Args[2]
	}
	if len(os.Args) > 3 {
		bodyJSON = os.Args[3]
	}

	clientID := "coordinator"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := generateNonce()

	// Compute SHA-256 of body.
	bodyHash := sha256.Sum256([]byte(bodyJSON))
	bodyHashHex := hex.EncodeToString(bodyHash[:])

	// Build canonical string: METHOD\nPATH\nTIMESTAMP\nNONCE\nSHA256HEX(BODY)
	canonical := strings.Join([]string{
		method,
		path,
		timestamp,
		nonce,
		bodyHashHex,
	}, "\n")

	// Compute HMAC-SHA256.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	signature := hex.EncodeToString(mac.Sum(nil))

	fmt.Println("=== Required Headers ===")
	fmt.Printf("X-Client-ID: %s\n", clientID)
	fmt.Printf("X-Timestamp: %s\n", timestamp)
	fmt.Printf("X-Nonce: %s\n", nonce)
	fmt.Printf("X-Signature: %s\n", signature)
	fmt.Println()
	fmt.Println("=== curl Example ===")
	fmt.Printf("curl -X %s http://localhost:8080%s \\\n", method, path)
	fmt.Printf("  -H 'X-Client-ID: %s' \\\n", clientID)
	fmt.Printf("  -H 'X-Timestamp: %s' \\\n", timestamp)
	fmt.Printf("  -H 'X-Nonce: %s' \\\n", nonce)
	fmt.Printf("  -H 'X-Signature: %s'", signature)
	if bodyJSON != "" {
		fmt.Printf(" \\\n  -H 'Content-Type: application/json' \\\n  -d '%s'", bodyJSON)
	}
	fmt.Println()
	fmt.Println()
	fmt.Println("=== Canonical String ===")
	fmt.Println(canonical)
}

// generateNonce returns a hex-encoded random-looking nonce based on the current
// nanosecond timestamp. For production use, replace with crypto/rand.
func generateNonce() string {
	nano := time.Now().UnixNano()
	h := sha256.Sum256([]byte(strconv.FormatInt(nano, 10)))
	return hex.EncodeToString(h[:16])
}
