package tasklog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendau/loom/server/internal/domain/tasklog/model"
	"github.com/rendau/loom/server/internal/errs"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(t.TempDir())
	require.NoError(t, err)
	return svc
}

func key(attempt int32) model.AttemptKey {
	return model.AttemptKey{RunId: "r1", Task: "extract", Attempt: attempt}
}

func entry(line string) model.Entry {
	return model.Entry{TsUnixMs: time.Now().UnixMilli(), Source: model.SourceLog, Line: line}
}

func readAll(t *testing.T, svc *Service, k model.AttemptKey, follow bool) []model.Entry {
	t.Helper()
	var got []model.Entry
	err := svc.Read(context.Background(), k, follow, func(entries []model.Entry) error {
		got = append(got, entries...)
		return nil
	})
	require.NoError(t, err)
	return got
}

func TestAppendFinishRead(t *testing.T) {
	svc := newTestService(t)
	k := key(1)

	require.NoError(t, svc.Append(k, []model.Entry{entry("line 1"), entry("line 2")}))
	require.NoError(t, svc.Finish(k, []model.Entry{{
		TsUnixMs: time.Now().UnixMilli(),
		Source:   model.SourceServer,
		Line:     "attempt succeeded",
	}}))

	got := readAll(t, svc, k, false)
	require.Len(t, got, 3)
	assert.Equal(t, "line 1", got[0].Line)
	assert.Equal(t, model.SourceLog, got[0].Source)
	assert.Equal(t, "attempt succeeded", got[2].Line)
	assert.Equal(t, model.SourceServer, got[2].Source)
}

func TestFollowReadLivesUntilFinish(t *testing.T) {
	svc := newTestService(t)
	k := key(1)

	require.NoError(t, svc.Append(k, []model.Entry{entry("early")}))

	type result struct {
		entries []model.Entry
		err     error
	}
	done := make(chan result, 1)
	go func() {
		var got []model.Entry
		err := svc.Read(context.Background(), k, true, func(entries []model.Entry) error {
			got = append(got, entries...)
			return nil
		})
		done <- result{got, err}
	}()

	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("follow-чтение завершилось до Finish")
	default:
	}

	require.NoError(t, svc.Append(k, []model.Entry{entry("late")}))
	require.NoError(t, svc.Finish(k, nil))

	select {
	case r := <-done:
		require.NoError(t, r.err)
		require.Len(t, r.entries, 2)
		assert.Equal(t, "early", r.entries[0].Line)
		assert.Equal(t, "late", r.entries[1].Line)
	case <-time.After(2 * time.Second):
		t.Fatal("follow-чтение не завершилось после Finish")
	}
}

func TestFinishWithoutPushCreatesLog(t *testing.T) {
	svc := newTestService(t)
	k := key(1)

	// SDK умер, ни одной строки не пришло — планировщик всё равно пишет
	// причину смерти и закрывает лог
	require.NoError(t, svc.Finish(k, []model.Entry{{
		TsUnixMs: time.Now().UnixMilli(),
		Source:   model.SourceServer,
		Line:     "attempt failed, reason: OOMKilled",
	}}))

	got := readAll(t, svc, k, false)
	require.Len(t, got, 1)
	assert.Equal(t, model.SourceServer, got[0].Source)
}

func TestFinishIdempotent(t *testing.T) {
	svc := newTestService(t)
	k := key(1)

	require.NoError(t, svc.Append(k, []model.Entry{entry("x")}))
	require.NoError(t, svc.Finish(k, nil))
	require.NoError(t, svc.Finish(k, []model.Entry{entry("ignored")}))

	got := readAll(t, svc, k, false)
	require.Len(t, got, 1)
}

func TestAppendAfterFinishRejected(t *testing.T) {
	svc := newTestService(t)
	k := key(1)

	require.NoError(t, svc.Finish(k, nil))

	err := svc.Append(k, []model.Entry{entry("late push")})
	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.LogAlreadyPushed))
}

func TestReadUnknownLog(t *testing.T) {
	svc := newTestService(t)

	err := svc.Read(context.Background(), key(9), false, func([]model.Entry) error { return nil })
	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ObjectNotFound))
}

func TestLogRecoveryAfterRestart(t *testing.T) {
	// рестарт server посреди попытки: новый Service на том же каталоге
	// возобновляет оборванный лог-стрим — запись продолжается с места
	// обрыва, а не отваливается в aborted (решение фазы 5)
	dir := t.TempDir()

	svc, err := New(dir)
	require.NoError(t, err)
	require.NoError(t, svc.Append(key(1), []model.Entry{entry("before restart")}))

	svc2, err := New(dir)
	require.NoError(t, err)
	require.NoError(t, svc2.Append(key(1), []model.Entry{entry("after restart")}))
	require.NoError(t, svc2.Finish(key(1), []model.Entry{entry("exit line")}))

	got := readAll(t, svc2, key(1), false)
	require.Len(t, got, 3)
	assert.Equal(t, "before restart", got[0].Line)
	assert.Equal(t, "after restart", got[1].Line)
	assert.Equal(t, "exit line", got[2].Line)
}
