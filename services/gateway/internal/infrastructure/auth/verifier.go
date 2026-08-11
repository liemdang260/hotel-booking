package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
)

type Config struct {
	Issuer, Audience string
	Keys map[string]*rsa.PublicKey
	Now func() time.Time
}

type Verifier struct {
	config Config
}

func NewVerifier(config Config) (*Verifier, error) {
	if config.Issuer == "" || config.Audience == "" || len(config.Keys) == 0 {
		return nil, domain.ErrInvalidRequest
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Verifier{config: config}, nil
}

func (v *Verifier) Authenticate(_ context.Context, authorization string) (domain.Principal, error) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return domain.Principal{}, domain.ErrUnauthenticated
	}
	parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(authorization, prefix)), ".")
	if len(parts) != 3 {
		return domain.Principal{}, domain.ErrUnauthenticated
	}

	var header struct {
		Algorithm string `json:"alg"`
		Type string `json:"typ"`
		KeyID string `json:"kid"`
	}
	if !decodeJSON(parts[0], &header) || header.Algorithm != "RS256" || header.Type != "JWT" || header.KeyID == "" {
		return domain.Principal{}, domain.ErrUnauthenticated
	}
	key, ok := v.config.Keys[header.KeyID]
	if !ok || key == nil {
		return domain.Principal{}, domain.ErrUnauthenticated
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return domain.Principal{}, domain.ErrUnauthenticated
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil {
		return domain.Principal{}, domain.ErrUnauthenticated
	}

	var claims struct {
		Subject string `json:"sub"`
		Issuer string `json:"iss"`
		Audience json.RawMessage `json:"aud"`
		Roles []string `json:"roles"`
		IssuedAt int64 `json:"iat"`
		NotBefore int64 `json:"nbf"`
		ExpiresAt int64 `json:"exp"`
	}
	if !decodeJSON(parts[1], &claims) {
		return domain.Principal{}, domain.ErrUnauthenticated
	}
	now := v.config.Now().Unix()
	if claims.Subject == "" || claims.Issuer != v.config.Issuer || !hasAudience(claims.Audience, v.config.Audience) ||
		claims.ExpiresAt <= now || claims.IssuedAt > now+60 || claims.NotBefore > now {
		return domain.Principal{}, domain.ErrUnauthenticated
	}
	return domain.Principal{UserID: claims.Subject, Roles: append([]string(nil), claims.Roles...), SubjectType: domain.SubjectUser}, nil
}

func decodeJSON(segment string, target any) bool {
	value, err := base64.RawURLEncoding.DecodeString(segment)
	return err == nil && json.Unmarshal(value, target) == nil
}

func hasAudience(raw json.RawMessage, expected string) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == expected
	}
	var multiple []string
	if json.Unmarshal(raw, &multiple) != nil {
		return false
	}
	for _, audience := range multiple {
		if audience == expected {
			return true
		}
	}
	return false
}
