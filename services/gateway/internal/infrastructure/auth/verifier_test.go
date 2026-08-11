package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
)

func TestVerifierRequiresTrustedIssuerAudienceExpiryAlgorithmAndKeyID(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	verifier, err := NewVerifier(Config{
		Issuer: "hotel-booking-auth",
		Audience: "hotel-booking-api",
		Keys: map[string]*rsa.PublicKey{"key-1": &key.PublicKey},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	token := signToken(t, key, "key-1", map[string]any{
		"sub": "user-1", "iss": "hotel-booking-auth", "aud": []string{"hotel-booking-api"},
		"roles": []string{"customer"}, "iat": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Minute).Unix(),
	})
	principal, err := verifier.Authenticate(context.Background(), "Bearer "+token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.UserID != "user-1" || principal.SubjectType != domain.SubjectUser {
		t.Fatalf("principal = %+v", principal)
	}

	cases := []struct {
		name string
		keyID string
		claims map[string]any
	}{
		{"unknown key", "unknown", map[string]any{"sub":"user-1","iss":"hotel-booking-auth","aud":"hotel-booking-api","iat":now.Unix(),"exp":now.Add(time.Minute).Unix()}},
		{"wrong issuer", "key-1", map[string]any{"sub":"user-1","iss":"attacker","aud":"hotel-booking-api","iat":now.Unix(),"exp":now.Add(time.Minute).Unix()}},
		{"wrong audience", "key-1", map[string]any{"sub":"user-1","iss":"hotel-booking-auth","aud":"other","iat":now.Unix(),"exp":now.Add(time.Minute).Unix()}},
		{"expired", "key-1", map[string]any{"sub":"user-1","iss":"hotel-booking-auth","aud":"hotel-booking-api","iat":now.Add(-time.Hour).Unix(),"exp":now.Add(-time.Second).Unix()}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bad := signToken(t, key, test.keyID, test.claims)
			if _, err := verifier.Authenticate(context.Background(), "Bearer "+bad); err == nil {
				t.Fatal("untrusted token accepted")
			}
		})
	}
}

func signToken(t *testing.T, key *rsa.PrivateKey, keyID string, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg":"RS256","typ":"JWT","kid":keyID})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	input := base64.RawURLEncoding.EncodeToString(header)+"."+base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input+"."+base64.RawURLEncoding.EncodeToString(signature)
}
