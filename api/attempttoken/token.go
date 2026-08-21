// Package attempttoken — attempt-токены data plane: control plane выдаёт
// токен поду таска (env LOOM_TOKEN) и подписывает свои вызовы, artifact-сервер
// и лог-приёмник проверяют scope. Токен — часть контракта между модулями,
// поэтому живёт в api.
//
// Формат: base64url(json(claims)) + "." + base64url(hmac-sha256(secret,
// payload)). Стейта на сервере нет — проверка только по общему секрету.
package attempttoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

// MetadataKey — ключ grpc-metadata, в котором клиенты передают токен.
const MetadataKey = "loom-token"

var (
	ErrInvalid = errors.New("invalid_token")
	ErrExpired = errors.New("token_expired")
)

// Claims — scope токена. Обычный токен скоупится на попытку таска;
// Admin=true — служебный токен control plane с полным доступом.
type Claims struct {
	RunID     string `json:"run_id,omitempty"`
	Task      string `json:"task,omitempty"`
	Attempt   int32  `json:"attempt,omitempty"`
	Admin     bool   `json:"admin,omitempty"`
	ExpiresAt int64  `json:"exp"` // unix-секунды
}

func Sign(secret []byte, claims Claims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode claims: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + sign(secret, encoded), nil
}

// Verify проверяет подпись и срок действия токена.
func Verify(secret []byte, token string, now time.Time) (Claims, error) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok {
		return Claims{}, fmt.Errorf("%w: malformed", ErrInvalid)
	}
	if !hmac.Equal([]byte(sign(secret, payload)), []byte(sig)) {
		return Claims{}, fmt.Errorf("%w: bad signature", ErrInvalid)
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	var claims Claims
	if err = json.Unmarshal(raw, &claims); err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	if now.Unix() >= claims.ExpiresAt {
		return Claims{}, ErrExpired
	}

	return claims, nil
}

func sign(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
