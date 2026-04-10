// Package auth provides lightweight JWT parsing for the sync-server.
// On a hackathon we use HMAC-SHA256 with a shared secret. In production
// this would be replaced with asymmetric key verification via JWKS.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Claims holds the decoded JWT payload fields relevant for awareness.
type Claims struct {
	UserID      string `json:"user_id"`
	Name        string `json:"name"`
	CursorColor string `json:"cursor_color,omitempty"`
}

// ErrInvalidToken is returned when the token cannot be parsed or verified.
var ErrInvalidToken = errors.New("invalid or malformed JWT token")

// ParseJWT decodes a JWT token string and verifies its HMAC-SHA256 signature
// using the provided secret. Returns the claims on success.
//
// If secret is empty, signature verification is skipped (dev mode).
// If the token matches the legacy "fake-jwt-token-for-{name}" format,
// it returns synthetic claims for backward compatibility.
func ParseJWT(tokenStr, secret string) (*Claims, error) {
	// --- Legacy mock token support ---
	const legacyPrefix = "fake-jwt-token-for-"
	if strings.HasPrefix(tokenStr, legacyPrefix) {
		name := strings.TrimPrefix(tokenStr, legacyPrefix)
		return &Claims{
			UserID:      name,
			Name:        name,
			CursorColor: generateColor(name),
		}, nil
	}

	// --- Standard JWT: header.payload.signature ---
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	// Verify signature (if secret is provided).
	if secret != "" {
		signingInput := parts[0] + "." + parts[1]
		expectedSig, err := computeHMAC(signingInput, secret)
		if err != nil {
			return nil, fmt.Errorf("hmac compute: %w", err)
		}
		actualSig, err := base64URLDecode(parts[2])
		if err != nil {
			return nil, fmt.Errorf("decode signature: %w", err)
		}
		if !hmac.Equal(expectedSig, actualSig) {
			return nil, ErrInvalidToken
		}
	}

	// Decode payload.
	payloadBytes, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	if claims.UserID == "" && claims.Name != "" {
		claims.UserID = claims.Name
	}
	if claims.Name == "" && claims.UserID != "" {
		claims.Name = claims.UserID
	}

	// Generate a deterministic cursor color if not provided in the token.
	if claims.CursorColor == "" {
		claims.CursorColor = generateColor(claims.UserID)
	}

	return &claims, nil
}

// computeHMAC returns the HMAC-SHA256 of message using the given key.
func computeHMAC(message, key string) ([]byte, error) {
	mac := hmac.New(sha256.New, []byte(key))
	_, err := mac.Write([]byte(message))
	if err != nil {
		return nil, err
	}
	return mac.Sum(nil), nil
}

// base64URLDecode decodes a base64url-encoded string (no padding).
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if necessary.
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}

// generateColor produces a deterministic hex color from a username.
// Uses a simple hash-based approach to ensure the same user always gets the same color.
func generateColor(name string) string {
	h := sha256.Sum256([]byte("cursor-color-" + name))
	// Use first 3 bytes for RGB, but clamp to avoid too-dark colors.
	r := int(h[0])%156 + 100 // 100-255
	g := int(h[1])%156 + 100
	b := int(h[2])%156 + 100
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}
