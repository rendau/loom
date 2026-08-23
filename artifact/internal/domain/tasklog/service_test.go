package tasklog

import (
	"context"
	"fmt"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendau/loom/artifact/internal/domain/tasklog/model"
)

func testKey() model.AttemptKey {
	return model.AttemptKey{RunId: "run-1", Task: "worker", Attempt: 1}
}

func entries(from, n int) []model.Entry {
	return lo.RepeatBy(n, func(i int) model.Entry {
		return model.Entry{TsUnixMs: int64(from + i), Source: model.SourceLog, Line: fmt.Sprintf("line-%d", from+i)}
	})
}

func readAll(t *testing.T, svc *Service, key model.AttemptKey, afterSeq int64) []model.Entry {
	t.Helper()
	var got []model.Entry
	err := svc.Read(context.Background(), key, afterSeq, false, func(batch []model.Entry) error {
		got = append(got, batch...)
		return nil
	})
	require.NoError(t, err)
	return got
}

// TestAppendSeqDedup — повторная досылка после реконнекта не дублирует
// строки: перекрывающийся префикс батча пропускается, разрыв — ошибка.
func TestAppendSeqDedup(t *testing.T) {
	svc, err := New(t.TempDir())
	require.NoError(t, err)
	key := testKey()

	next, err := svc.NextSeq(key)
	require.NoError(t, err)
	assert.EqualValues(t, 0, next)

	next, err = svc.Append(key, 0, entries(0, 3))
	require.NoError(t, err)
	assert.EqualValues(t, 3, next)

	// досылка с перекрытием: строки 1-2 уже есть, добавятся только 3-4
	next, err = svc.Append(key, 1, entries(1, 4))
	require.NoError(t, err)
	assert.EqualValues(t, 5, next)

	// полностью подтверждённый батч — no-op
	next, err = svc.Append(key, 0, entries(0, 2))
	require.NoError(t, err)
	assert.EqualValues(t, 5, next)

	// разрыв нумерации — нарушение протокола
	_, err = svc.Append(key, 7, entries(7, 1))
	require.ErrorIs(t, err, ErrSeqGap)

	require.NoError(t, svc.Finish(key, entries(100, 1)))

	got := readAll(t, svc, key, 0)
	require.Len(t, got, 6)
	for i, e := range got[:5] {
		assert.Equal(t, fmt.Sprintf("line-%d", i), e.Line)
	}
	assert.Equal(t, "line-100", got[5].Line)

	// чтение с позиции (реконнект читателя)
	tail := readAll(t, svc, key, 4)
	require.Len(t, tail, 2)
	assert.Equal(t, "line-4", tail[0].Line)
}

// TestRecoverAfterRestart — рестарт сервера: новый Service над тем же
// каталогом продолжает лог с места обрыва, NextSeq отдаёт честную позицию.
func TestRecoverAfterRestart(t *testing.T) {
	dir := t.TempDir()

	svc, err := New(dir)
	require.NoError(t, err)
	key := testKey()

	_, err = svc.Append(key, 0, entries(0, 4))
	require.NoError(t, err)

	// «рестарт»: новый сервис над тем же каталогом
	svc2, err := New(dir)
	require.NoError(t, err)

	next, err := svc2.NextSeq(key)
	require.NoError(t, err)
	assert.EqualValues(t, 4, next)

	// клиент досылает неподтверждённый хвост с перекрытием
	next, err = svc2.Append(key, 2, entries(2, 4))
	require.NoError(t, err)
	assert.EqualValues(t, 6, next)

	require.NoError(t, svc2.Finish(key, nil))

	got := readAll(t, svc2, key, 0)
	require.Len(t, got, 6)
	for i, e := range got {
		assert.Equal(t, fmt.Sprintf("line-%d", i), e.Line)
	}

	// финализированный лог не принимает поздний push
	_, err = svc2.Append(key, 6, entries(6, 1))
	require.Error(t, err)
}
