// Package apikey issues and verifies per-workspace ingestion credentials.
//
// A key is a single opaque token of the form:
//
//	trc_<hex-encoded random bytes>
//
// Only the token's SHA-256 hash is persisted; the plaintext is returned to the
// caller once at creation and cannot be recovered afterwards. Verification
// hashes the presented token and looks the row up by hash, so the lookup is
// exact and constant work regardless of how many keys exist — there is no
// per-candidate secret comparison to time-attack.
package apikey

import "github.com/tracium/api/internal/token"

// TokenPrefix marks Tracium ingest keys so they are recognisable in logs and
// secret scanners, and so a token pasted into the wrong field is obvious.
const TokenPrefix = "trc_"

// displayPrefixLen is how much of the token is retained, in clear, as the
// human-facing identifier (e.g. "trc_9f3a1b2c"). Long enough to disambiguate a
// handful of keys, short enough to reveal nothing useful to an attacker.
const displayPrefixLen = len(TokenPrefix) + 8

// generated is a freshly minted key: the plaintext token to hand back to the
// caller once, plus the derived values that are safe to store.
type generated struct {
	Token  string // full plaintext — returned to the caller exactly once
	Prefix string // non-secret display slice, persisted
	Hash   string // SHA-256 of Token, persisted; the lookup key
}

// generate mints a new token from the crypto RNG. An RNG failure is surfaced
// rather than papered over — a low-entropy key must never be issued.
func generate() (generated, error) {
	t, err := token.New(TokenPrefix)
	if err != nil {
		return generated{}, err
	}
	return generated{Token: t, Prefix: t[:displayPrefixLen], Hash: token.Hash(t)}, nil
}
