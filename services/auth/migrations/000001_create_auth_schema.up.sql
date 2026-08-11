CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE auth_users(
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 email VARCHAR(320) NOT NULL,
 password_hash TEXT NOT NULL,
 status VARCHAR(16) NOT NULL CHECK(status IN('ACTIVE','DISABLED')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX auth_users_email_ci_unique ON auth_users(lower(email));
CREATE TABLE auth_refresh_tokens(
 id UUID PRIMARY KEY,
 user_id UUID NOT NULL REFERENCES auth_users(id) ON DELETE RESTRICT,
 family_id UUID NOT NULL,
 token_hash CHAR(64) NOT NULL UNIQUE,
 expires_at TIMESTAMPTZ NOT NULL,
 revoked_at TIMESTAMPTZ,
 rotated_from UUID REFERENCES auth_refresh_tokens(id) ON DELETE RESTRICT,
 rotated_to UUID REFERENCES auth_refresh_tokens(id) ON DELETE RESTRICT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(rotated_to IS NULL OR revoked_at IS NOT NULL)
);
CREATE INDEX auth_refresh_tokens_family_active ON auth_refresh_tokens(family_id,expires_at) WHERE revoked_at IS NULL;
COMMENT ON COLUMN auth_refresh_tokens.token_hash IS 'SHA-256 hash of opaque random token; plaintext is never persisted.';
