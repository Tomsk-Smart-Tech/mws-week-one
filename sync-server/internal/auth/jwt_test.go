package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// --- Group: Legacy Token Parsing ---

func TestParseLegacyToken(t *testing.T) {
	claims, err := ParseJWT("fake-jwt-token-for-denis", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.UserID != "denis" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "denis")
	}
	if claims.Name != "denis" {
		t.Errorf("Name = %q, want %q", claims.Name, "denis")
	}
	if claims.CursorColor == "" {
		t.Error("CursorColor is empty, want non-empty hex color")
	}
	if !strings.HasPrefix(claims.CursorColor, "#") || len(claims.CursorColor) != 7 {
		t.Errorf("CursorColor = %q, want format #rrggbb", claims.CursorColor)
	}
}

func TestLegacyTokenDeterministicColor(t *testing.T) {
	c1, _ := ParseJWT("fake-jwt-token-for-alice", "")
	c2, _ := ParseJWT("fake-jwt-token-for-alice", "")
	c3, _ := ParseJWT("fake-jwt-token-for-bob", "")

	if c1.CursorColor != c2.CursorColor {
		t.Errorf("same user should get same color: %q != %q", c1.CursorColor, c2.CursorColor)
	}
	if c1.CursorColor == c3.CursorColor {
		t.Errorf("different users should get different colors: both got %q", c1.CursorColor)
	}
}

// --- Group: Real JWT Parsing ---

func makeTestJWT(payload map[string]string, secret string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadBytes, _ := json.Marshal(payload)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signingInput := header + "." + payloadB64
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sig
}

func TestParseRealJWT_ValidSignature(t *testing.T) {
	secret := "test-secret-key-123"
	token := makeTestJWT(map[string]string{
		"user_id": "user-42",
		"name":    "Кирилл",
	}, secret)

	claims, err := ParseJWT(token, secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.UserID != "user-42" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-42")
	}
	if claims.Name != "Кирилл" {
		t.Errorf("Name = %q, want %q", claims.Name, "Кирилл")
	}
	if claims.CursorColor == "" {
		t.Error("CursorColor should be auto-generated when not in payload")
	}
}

func TestParseRealJWT_WithCursorColor(t *testing.T) {
	secret := "my-secret"
	token := makeTestJWT(map[string]string{
		"user_id":      "u1",
		"name":         "Test",
		"cursor_color": "#ff0000",
	}, secret)

	claims, err := ParseJWT(token, secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.CursorColor != "#ff0000" {
		t.Errorf("CursorColor = %q, want %q", claims.CursorColor, "#ff0000")
	}
}

func TestParseRealJWT_InvalidSignature(t *testing.T) {
	token := makeTestJWT(map[string]string{"user_id": "u1", "name": "Test"}, "correct-secret")

	_, err := ParseJWT(token, "wrong-secret")
	if err == nil {
		t.Fatal("expected error for invalid signature, got nil")
	}
}

func TestParseRealJWT_DevMode_NoVerification(t *testing.T) {
	token := makeTestJWT(map[string]string{"user_id": "u1", "name": "Dev"}, "any-secret")

	// Empty secret = dev mode, no signature check.
	claims, err := ParseJWT(token, "")
	if err != nil {
		t.Fatalf("dev mode should skip verification: %v", err)
	}
	if claims.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "u1")
	}
}

// --- Group: Error Cases ---

func TestParseJWT_EmptyToken(t *testing.T) {
	_, err := ParseJWT("", "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestParseJWT_MalformedToken(t *testing.T) {
	_, err := ParseJWT("not-a-jwt", "secret")
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestParseJWT_TwoPartsOnly(t *testing.T) {
	_, err := ParseJWT("header.payload", "secret")
	if err == nil {
		t.Fatal("expected error for two-part token")
	}
}

// --- Group: Field Fallbacks ---

func TestParseJWT_UserIDFallsBackToName(t *testing.T) {
	token := makeTestJWT(map[string]string{"name": "OnlyName"}, "s")
	claims, err := ParseJWT(token, "s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.UserID != "OnlyName" {
		t.Errorf("UserID should fallback to Name: got %q", claims.UserID)
	}
}

func TestParseJWT_NameFallsBackToUserID(t *testing.T) {
	token := makeTestJWT(map[string]string{"user_id": "OnlyID"}, "s")
	claims, err := ParseJWT(token, "s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.Name != "OnlyID" {
		t.Errorf("Name should fallback to UserID: got %q", claims.Name)
	}
}
