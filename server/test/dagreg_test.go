package test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mechta-market/mobone/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	dagregModel "github.com/rendau/loom/server/internal/domain/dagreg/model"
	dagregDb "github.com/rendau/loom/server/internal/domain/dagreg/repo/db"
	dagregService "github.com/rendau/loom/server/internal/domain/dagreg/service"
	settingDb "github.com/rendau/loom/server/internal/domain/setting/repo/db"
	settingService "github.com/rendau/loom/server/internal/domain/setting/service"
)

// fakeProcessor — обработчик очереди регистраций для тестов воркера.
type fakeProcessor struct {
	mu      sync.Mutex
	dagName string
	err     error
	calls   int
}

func (f *fakeProcessor) Process(_ context.Context, _ *dagregModel.Main) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.dagName, f.err
}

func newDagregEnv(t *testing.T) (*dagregDb.Repo, func(tick time.Duration) *dagregService.Service) {
	t.Helper()
	e := newEnv(t)

	txm := mobone.NewTransactionManager(e.pool)
	base := commonRepoPg.NewBase(e.pool, txm)
	repo := dagregDb.New(base)
	settings := settingService.New(settingDb.New(base))
	return repo, func(tick time.Duration) *dagregService.Service {
		return dagregService.New(repo, settings, tick, time.Hour)
	}
}

// Очередь регистраций: постановка, claim (running), финализация со статусом
// и дописанным именем дага; дедуп auto-постановок; FailStale добивает
// брошенные running-записи.
func TestDagRegistrationQueue(t *testing.T) {
	ctx := context.Background()
	repo, newSvc := newDagregEnv(t)
	svc := newSvc(time.Hour) // воркер не стартуем — ручной claim

	reg, err := svc.Enqueue(ctx, dagregModel.EnqueueSpec{
		Image:    "registry/demo:latest",
		Source:   dagregModel.SourceManual,
		Schedule: new("@daily"),
		Paused:   new(true),
	})
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, dagregModel.StatusPending, reg.Status)

	// claim переводит в running и отдаёт запись; второй claim пуст
	claimed, err := repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, reg.Id, claimed[0].Id)
	assert.Equal(t, dagregModel.StatusRunning, claimed[0].Status)
	require.NotNil(t, claimed[0].Schedule)
	assert.Equal(t, "@daily", *claimed[0].Schedule)

	again, err := repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, again)

	// финализация: статус, имя дага
	require.NoError(t, repo.Finish(ctx, reg.Id, dagregModel.StatusSuccess, "", "demo"))
	got, err := svc.Get(ctx, reg.Id)
	require.NoError(t, err)
	assert.Equal(t, dagregModel.StatusSuccess, got.Status)
	assert.Equal(t, "demo", got.DagName)
	assert.False(t, got.FinishedAt.IsZero())

	// дедуп auto: активная регистрация того же образа гасит новую
	first, err := svc.Enqueue(ctx, dagregModel.EnqueueSpec{
		Image: "registry/auto:latest", Source: dagregModel.SourceAuto, DagName: "auto-dag",
	})
	require.NoError(t, err)
	require.NotNil(t, first)

	dup, err := svc.Enqueue(ctx, dagregModel.EnqueueSpec{
		Image: "registry/auto:latest", Source: dagregModel.SourceAuto, DagName: "auto-dag",
	})
	require.NoError(t, err)
	assert.Nil(t, dup, "активная auto-регистрация того же образа не дублируется")

	// FailStale: claim'нутая запись старше порога падает в failed
	claimed, err = repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	n, err := repo.FailStale(ctx, time.Now().Add(time.Second))
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	got, err = svc.Get(ctx, first.Id)
	require.NoError(t, err)
	assert.Equal(t, dagregModel.StatusFailed, got.Status)
	assert.NotEmpty(t, got.Error)

	// выборки: по дагу и только активные
	byDag, err := svc.List(ctx, &dagregModel.ListReq{DagName: new("auto-dag")})
	require.NoError(t, err)
	require.Len(t, byDag, 1)

	active, err := svc.List(ctx, &dagregModel.ListReq{OnlyActive: true})
	require.NoError(t, err)
	assert.Empty(t, active)
}

// Воркер очереди: успешная обработка и ошибка обработчика доводят запись до
// терминального статуса без внешних пинков (Nudge после Enqueue).
func TestDagRegistrationWorker(t *testing.T) {
	ctx := context.Background()
	_, newSvc := newDagregEnv(t)

	t.Run("success", func(t *testing.T) {
		svc := newSvc(20 * time.Millisecond)
		processor := &fakeProcessor{dagName: "worker-dag"}
		svc.Start(processor)
		t.Cleanup(svc.Stop)

		reg, err := svc.Enqueue(ctx, dagregModel.EnqueueSpec{
			Image: "registry/worker:latest", Source: dagregModel.SourceManual,
		})
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			got, gErr := svc.Get(ctx, reg.Id)
			require.NoError(t, gErr)
			return got.Status == dagregModel.StatusSuccess
		}, waitTimeout, 10*time.Millisecond)

		got, err := svc.Get(ctx, reg.Id)
		require.NoError(t, err)
		assert.Equal(t, "worker-dag", got.DagName)
	})

	t.Run("processor error → failed", func(t *testing.T) {
		svc := newSvc(20 * time.Millisecond)
		processor := &fakeProcessor{err: fmt.Errorf("инспекция образа: boom")}
		svc.Start(processor)
		t.Cleanup(svc.Stop)

		reg, err := svc.Enqueue(ctx, dagregModel.EnqueueSpec{
			Image: "registry/broken:latest", Source: dagregModel.SourceManual,
		})
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			got, gErr := svc.Get(ctx, reg.Id)
			require.NoError(t, gErr)
			return got.Status == dagregModel.StatusFailed
		}, waitTimeout, 10*time.Millisecond)

		got, err := svc.Get(ctx, reg.Id)
		require.NoError(t, err)
		assert.Contains(t, got.Error, "boom")
	})
}
