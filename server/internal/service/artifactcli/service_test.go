package artifactcli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// быстрый цикл: те же правила, но паузы и бюджет в миллисекундах
func testLoop(bounded bool) *reconnectLoop {
	return &reconnectLoop{
		bounded:  bounded,
		budget:   30 * time.Millisecond,
		minDelay: time.Millisecond,
		maxDelay: 4 * time.Millisecond,
		delay:    time.Millisecond,
	}
}

// follow-чтение обязано пережить сколь угодно долгий рестарт
// artifact-сервера — предела ожидания у него нет.
func TestReconnectLoopUnboundedNeverGivesUp(t *testing.T) {
	loop := testLoop(false)
	ctx := context.Background()

	for range 100 {
		require.NoError(t, loop.wait(ctx, false))
	}
}

// разовое чтение (лог без follow, скачивание артефакта) сдаётся по бюджету,
// иначе админка бесконечно висит на пустой панели вместо ошибки
func TestReconnectLoopBoundedGivesUpAfterBudget(t *testing.T) {
	loop := testLoop(true)
	ctx := context.Background()

	var err error
	for range 1000 {
		if err = loop.wait(ctx, false); err != nil {
			break
		}
	}

	require.ErrorIs(t, err, errReconnectGiveUp)
}

// прогресс начинает серию заново: долгое чтение с редкими обрывами
// не должно упираться в бюджет
func TestReconnectLoopProgressResetsBudget(t *testing.T) {
	loop := testLoop(true)
	ctx := context.Background()

	for range 10 {
		// каждый раз читатель успел получить данные до обрыва
		require.NoError(t, loop.wait(ctx, true))
		time.Sleep(5 * time.Millisecond)
	}
}

// backoff растёт до maxDelay и сбрасывается прогрессом
func TestReconnectLoopBackoff(t *testing.T) {
	loop := testLoop(false)
	ctx := context.Background()

	for range 10 {
		require.NoError(t, loop.wait(ctx, false))
	}
	require.Equal(t, loop.maxDelay, loop.delay)

	require.NoError(t, loop.wait(ctx, true))
	require.Equal(t, 2*loop.minDelay, loop.delay)
}

// отменённый контекст прерывает ожидание, а не бюджет
func TestReconnectLoopContextCancel(t *testing.T) {
	loop := testLoop(false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, loop.wait(ctx, false), context.Canceled)
}
