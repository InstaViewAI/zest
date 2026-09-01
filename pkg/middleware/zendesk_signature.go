package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"zest/pkg/contracts"
)

const (
	signatureHeader          = "X-Zendesk-Webhook-Signature"
	signatureTimestampHeader = "X-Zendesk-Webhook-Signature-Timestamp"

	// maxWebhookBody caps how much of a webhook body we buffer for signing.
	maxWebhookBody = 1 << 20 // 1 MiB
)

// VerifyZendeskSignature validates the HMAC-SHA256 signature Zendesk attaches to
// webhook deliveries. An empty secret disables verification, which is fine
// locally but means anyone who learns the URL can post escalations in prod.
func VerifyZendeskSignature(secret string) gin.HandlerFunc {
	if secret == "" {
		log.Print("WARNING: ZENDESK_WEBHOOK_SECRET is empty, webhook signature verification is DISABLED")

		return func(c *gin.Context) { c.Next() }
	}

	key := []byte(secret)

	return func(c *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBody))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest,
				contracts.ErrorResponse{Error: "failed to read request body"})
			return
		}

		// The body is consumed by reading it, so hand the handler a fresh copy.
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		signature := c.GetHeader(signatureHeader)
		timestamp := c.GetHeader(signatureTimestampHeader)
		if signature == "" || timestamp == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				contracts.ErrorResponse{Error: "missing webhook signature"})
			return
		}

		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(timestamp))
		mac.Write(body)
		expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(expected), []byte(signature)) {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				contracts.ErrorResponse{Error: "invalid webhook signature"})
			return
		}

		c.Next()
	}
}
