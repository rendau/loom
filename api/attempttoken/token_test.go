package attempttoken

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var secret = []byte("test-secret")

func TestSignVerifyRoundtrip(t *testing.T) {
	claims := Claims{RunID: "r1", Task: "extract", Attempt: 2, ExpiresAt: time.Now().Add(time.Hour).Unix()}

	token, err := Sign(secret, claims)
	require.NoError(t, err)

	got, err := Verify(secret, token, time.Now())
	require.NoError(t, err)
	assert.Equal(t, claims, got)
}

func TestSignVerifyAdmin(t *testing.T) {
	token, err := Sign(secret, Claims{Admin: true, ExpiresAt: time.Now().Add(time.Minute).Unix()})
	require.NoError(t, err)

	got, err := Verify(secret, token, time.Now())
	require.NoError(t, err)
	assert.True(t, got.Admin)
}

func TestVerifyWrongSecret(t *testing.T) {
	token, err := Sign(secret, Claims{RunID: "r1", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	require.NoError(t, err)

	_, err = Verify([]byte("other-secret"), token, time.Now())
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestVerifyExpired(t *testing.T) {
	token, err := Sign(secret, Claims{RunID: "r1", ExpiresAt: time.Now().Add(-time.Second).Unix()})
	require.NoError(t, err)

	_, err = Verify(secret, token, time.Now())
	assert.ErrorIs(t, err, ErrExpired)
}

func TestVerifyTampered(t *testing.T) {
	token, err := Sign(secret, Claims{RunID: "r1", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	require.NoError(t, err)

	payload, sig, _ := strings.Cut(token, ".")
	for _, bad := range []string{"garbage", payload, payload + "x." + sig, "." + sig} {
		_, err = Verify(secret, bad, time.Now())
		assert.ErrorIs(t, err, ErrInvalid, "token %q", bad)
	}
}
