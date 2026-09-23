package scheduler

import (
	"github.com/samber/lo"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
)

// plan — решения планировщика по одному рану на основе графа и текущих
// статусов тасков.
type plan struct {
	Promote []string // pending → queued: зависимости удовлетворены
	Demote  []string // queued → pending: стримовый отправитель выбыл в up_for_retry
	// Каскад upstream_failed — по исходному статусу (PromoteTasks guarded по
	// fromStatus): терминальное падение зависимости накрывает и таски,
	// успевшие уйти в очередь или в ожидание ретрая, — их попытки
	// гарантированно упали бы на чтении мёртвого стрима.
	UpstreamFailed       []string // из pending
	UpstreamFailedQueued []string // из queued
	UpstreamFailedRetry  []string // из up_for_retry
	RunDone              bool
	RunStatus            string
}

// buildPlan — чистая функция раскрутки графа.
//
// Готовность pending-таска: обычное ребро (After) ждёт success отправителя;
// стримовое (AfterStreamed) — ко-старт, достаточно
// starting/running/success. Падение любой зависимости каскадно валит
// потомков в upstream_failed: pending-таски — не запускаясь вовсе,
// queued и up_for_retry — не плодя новых заведомо провальных попыток.
// Queued-получатель стримового ребра, чей отправитель ушёл в up_for_retry
// (ещё не терминален), демотируется обратно в pending — ко-старт
// переиграется с новой попыткой отправителя. Ран завершён, когда все таски
// терминальны.
func buildPlan(tasks []dagModel.Task, statuses map[string]string) plan {
	result := plan{}

	// каскад upstream_failed до неподвижной точки: падение зависимостей
	// распространяется по цепочке тасков за один проход планировщика
	st := lo.Assign(statuses)
	for {
		changed := false
		for _, t := range tasks {
			cur := st[t.Name]
			if cur != runModel.TaskStatusPending && cur != runModel.TaskStatusQueued && cur != runModel.TaskStatusUpForRetry {
				continue
			}
			failed := lo.ContainsBy(t.DependsOn, func(d dagModel.Dep) bool {
				return st[d.Task] == runModel.TaskStatusFailed || st[d.Task] == runModel.TaskStatusUpstreamFailed
			})
			if !failed {
				continue
			}
			st[t.Name] = runModel.TaskStatusUpstreamFailed
			switch cur {
			case runModel.TaskStatusPending:
				result.UpstreamFailed = append(result.UpstreamFailed, t.Name)
			case runModel.TaskStatusQueued:
				result.UpstreamFailedQueued = append(result.UpstreamFailedQueued, t.Name)
			case runModel.TaskStatusUpForRetry:
				result.UpstreamFailedRetry = append(result.UpstreamFailedRetry, t.Name)
			}
			changed = true
		}
		if !changed {
			break
		}
	}

	// демоция ко-старта: очередь покидает таск, чья стримовая зависимость
	// больше не удовлетворена (отправитель в up_for_retry) — запуск сейчас
	// прочитал бы абортированный стрим упавшей попытки
	for _, t := range tasks {
		if st[t.Name] != runModel.TaskStatusQueued {
			continue
		}
		ready := lo.EveryBy(t.DependsOn, func(d dagModel.Dep) bool {
			return depSatisfied(d, st[d.Task])
		})
		if !ready {
			st[t.Name] = runModel.TaskStatusPending
			result.Demote = append(result.Demote, t.Name)
		}
	}

	for _, t := range tasks {
		if st[t.Name] != runModel.TaskStatusPending {
			continue
		}
		ready := lo.EveryBy(t.DependsOn, func(d dagModel.Dep) bool {
			return depSatisfied(d, st[d.Task])
		})
		if ready {
			result.Promote = append(result.Promote, t.Name)
		}
	}

	if len(result.Promote) == 0 {
		result.RunDone = true
		result.RunStatus = runModel.RunStatusSuccess
		for _, t := range tasks {
			switch st[t.Name] {
			case runModel.TaskStatusSuccess:
			case runModel.TaskStatusFailed, runModel.TaskStatusUpstreamFailed:
				result.RunStatus = runModel.RunStatusFailed
			default:
				result.RunDone = false
			}
		}
		if !result.RunDone {
			result.RunStatus = ""
		}
	}

	return result
}

func depSatisfied(d dagModel.Dep, depStatus string) bool {
	if depStatus == runModel.TaskStatusSuccess {
		return true
	}
	if d.Streamed {
		// ко-старт: отправитель запущен (или уже успел завершиться успехом)
		return depStatus == runModel.TaskStatusStarting || depStatus == runModel.TaskStatusRunning
	}
	return false
}
