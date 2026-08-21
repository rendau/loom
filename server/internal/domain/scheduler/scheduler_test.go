package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetryBackoff(t *testing.T) {
	// без delaySec — дефолт с удвоением на каждой следующей попытке
	assert.Equal(t, defaultRetryDelay, retryBackoff(0, 1))
	assert.Equal(t, 2*defaultRetryDelay, retryBackoff(0, 2))
	assert.Equal(t, 4*defaultRetryDelay, retryBackoff(0, 3))

	// явная база из манифеста
	assert.Equal(t, 5*time.Second, retryBackoff(5, 1))
	assert.Equal(t, 20*time.Second, retryBackoff(5, 3))

	// потолок backoff'а
	assert.Equal(t, maxRetryBackoff, retryBackoff(1800, 10))
}
