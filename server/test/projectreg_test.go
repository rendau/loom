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
	projectregModel "github.com/rendau/loom/server/internal/domain/projectreg/model"
	projectregDb "github.com/rendau/loom/server/internal/domain/projectreg/repo/db"
	projectregService "github.com/rendau/loom/server/internal/domain/projectreg/service"
	settingDb "github.com/rendau/loom/server/internal/domain/setting/repo/db"
	settingService "github.com/rendau/loom/server/internal/domain/setting/service"
)

// fakeProcessor — обработчик очереди регистраций для тестов воркера.
type fakeProcessor struct {
	mu     sync.Mutex
	result []projectregModel.DagResult
	err    error
	calls  int
}

func (f *fakeProcessor) Process(_ context.Context, _ *projectregModel.Main) ([]projectregModel.DagResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result, f.err
}

func newProjectregEnv(t *testing.T) (*projectregDb.Repo, func(tick time.Duration) *projectregService.Service) {
	t.Helper()
	e := newEnv(t)

	txm := mobone.NewTransactionManager(e.pool)
	base := commonRepoPg.NewBase(e.pool, txm)
	repo := projectregDb.New(base)
	settings := settingService.New(settingDb.New(base))
	return repo, func(tick time.Duration) *projectregService.Service {
		return projectregService.New(repo, settings, tick, time.Hour)
	}
}

// Очередь регистраций: постановка, claim (running), финализация со статусом
// и дописанным именем дага; дедуп auto-постановок; FailStale добивает
// брошенные running-записи.
func TestProjectRegistrationQueue(t *testing.T) {
	ctx := context.Background()
	repo, newSvc := newProjectregEnv(t)
	svc := newSvc(time.Hour) // воркер не стартуем — ручной claim

	reg, err := svc.Enqueue(ctx, projectregModel.EnqueueSpec{
		ProjectName: "demo",
		Image:       "registry/demo:latest",
		Source:      projectregModel.SourceManual,
		AutoUpdate:  new(true),
		CreateDags:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, projectregModel.StatusPending, reg.Status)

	// claim переводит в running и отдаёт запись; второй claim пуст
	claimed, err := repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, reg.Id, claimed[0].Id)
	assert.Equal(t, projectregModel.StatusRunning, claimed[0].Status)
	assert.True(t, claimed[0].CreateDags)
	require.NotNil(t, claimed[0].AutoUpdate)
	assert.True(t, *claimed[0].AutoUpdate)

	again, err := repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, again)

	// финализация: статус и итог по дагам образа
	result := []projectregModel.DagResult{
		{Name: "etl", Created: true},
		{Name: "broken", Error: "в даге нет тасков"},
	}
	require.NoError(t, repo.Finish(ctx, reg.Id, projectregModel.StatusSuccess, "", result))
	got, err := svc.Get(ctx, reg.Id)
	require.NoError(t, err)
	assert.Equal(t, projectregModel.StatusSuccess, got.Status)
	assert.Equal(t, result, got.Result)
	assert.False(t, got.FinishedAt.IsZero())

	// дедуп auto: активная регистрация того же проекта гасит новую
	first, err := svc.Enqueue(ctx, projectregModel.EnqueueSpec{
		ProjectName: "auto-project", Image: "registry/auto:latest",
		Source: projectregModel.SourceAuto,
	})
	require.NoError(t, err)
	require.NotNil(t, first)

	dup, err := svc.Enqueue(ctx, projectregModel.EnqueueSpec{
		ProjectName: "auto-project", Image: "registry/auto:latest",
		Source: projectregModel.SourceAuto,
	})
	require.NoError(t, err)
	assert.Nil(t, dup, "активная auto-регистрация того же проекта не дублируется")

	// FailStale: claim'нутая запись старше порога падает в failed
	claimed, err = repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	n, err := repo.FailStale(ctx, time.Now().Add(time.Second))
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	got, err = svc.Get(ctx, first.Id)
	require.NoError(t, err)
	assert.Equal(t, projectregModel.StatusFailed, got.Status)
	assert.NotEmpty(t, got.Error)

	// выборки: по проекту и только активные
	byProject, err := svc.List(ctx, &projectregModel.ListReq{ProjectName: new("auto-project")})
	require.NoError(t, err)
	require.Len(t, byProject, 1)

	active, err := svc.List(ctx, &projectregModel.ListReq{OnlyActive: true})
	require.NoError(t, err)
	assert.Empty(t, active)
}

// Воркер очереди: успешная обработка и ошибка обработчика доводят запись до
// терминального статуса без внешних пинков (Nudge после Enqueue).
func TestProjectRegistrationWorker(t *testing.T) {
	ctx := context.Background()
	_, newSvc := newProjectregEnv(t)

	t.Run("success", func(t *testing.T) {
		svc := newSvc(20 * time.Millisecond)
		processor := &fakeProcessor{result: []projectregModel.DagResult{{Name: "worker-dag", Created: true}}}
		svc.Start(processor)
		t.Cleanup(svc.Stop)

		reg, err := svc.Enqueue(ctx, projectregModel.EnqueueSpec{
			ProjectName: "worker", Image: "registry/worker:latest",
			Source: projectregModel.SourceManual,
		})
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			got, gErr := svc.Get(ctx, reg.Id)
			require.NoError(t, gErr)
			return got.Status == projectregModel.StatusSuccess
		}, waitTimeout, 10*time.Millisecond)

		got, err := svc.Get(ctx, reg.Id)
		require.NoError(t, err)
		require.Len(t, got.Result, 1)
		assert.Equal(t, "worker-dag", got.Result[0].Name)
		assert.True(t, got.Result[0].Created)
	})

	t.Run("processor error → failed", func(t *testing.T) {
		svc := newSvc(20 * time.Millisecond)
		processor := &fakeProcessor{err: fmt.Errorf("инспекция образа: boom")}
		svc.Start(processor)
		t.Cleanup(svc.Stop)

		reg, err := svc.Enqueue(ctx, projectregModel.EnqueueSpec{
			ProjectName: "broken", Image: "registry/broken:latest",
			Source: projectregModel.SourceManual,
		})
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			got, gErr := svc.Get(ctx, reg.Id)
			require.NoError(t, gErr)
			return got.Status == projectregModel.StatusFailed
		}, waitTimeout, 10*time.Millisecond)

		got, err := svc.Get(ctx, reg.Id)
		require.NoError(t, err)
		assert.Contains(t, got.Error, "boom")
	})
}
