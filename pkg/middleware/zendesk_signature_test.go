package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newSignedRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.POST("/hook", VerifyZendeskSignature(secret), func(c *gin.Context) {
		// Reading the body here proves the middleware restored it.
		body, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusOK, string(body))
	})

	return r
}

func sign(secret, timestamp, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte(body))

	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestValidSignaturePassesAndBodyIsReadable(t *testing.T) {
	const secret, ts, body = "s3cret", "2026-09-01T00:00:00Z", `{"ticket_id":"1"}`

	req := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(body))
	req.Header.Set(signatureTimestampHeader, ts)
	req.Header.Set(signatureHeader, sign(secret, ts, body))

	w := httptest.NewRecorder()
	newSignedRouter(secret).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != body {
		t.Errorf("handler saw %q, want %q — body not restored", w.Body.String(), body)
	}
}

func TestInvalidSignatureIsRejected(t *testing.T) {
	const ts, body = "2026-09-01T00:00:00Z", `{"ticket_id":"1"}`

	req := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(body))
	req.Header.Set(signatureTimestampHeader, ts)
	req.Header.Set(signatureHeader, sign("wrong-secret", ts, body))

	w := httptest.NewRecorder()
	newSignedRouter("s3cret").ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMissingSignatureHeadersAreRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(`{}`))

	w := httptest.NewRecorder()
	newSignedRouter("s3cret").ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestEmptySecretDisablesVerification(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(`{"a":1}`))

	w := httptest.NewRecorder()
	newSignedRouter("").ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when verification disabled, got %d", w.Code)
	}
}
