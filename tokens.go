package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// Verified against the public H5 crypto helper: SHA256(UTF8).toString(Hex).
func encodePassword(password string) (string, error) {
	h := sha256.Sum256([]byte(password))
	return hex.EncodeToString(h[:]), nil
}

// A JWT payload is used only as a refresh scheduling hint. This does NOT verify
// a token or grant authentication: all authority comes from upstream responses.
func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var data struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(data.Exp, 0).UTC()
}
