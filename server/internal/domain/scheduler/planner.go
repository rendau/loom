package scheduler

import (
	"github.com/samber/lo"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
)

// plan — решения планировщика по одному рану на основе графа и текущих
// статусов тасков.
type plan struct {
	Promote        []string // pending → queued: зависимости удовлетворены
	UpstreamFailed []string // pending → upstream_failed: зависимость упала
	RunDone        bool
	RunStatus      string
}

// buildPlan — чистая функция раскрутки графа.
//
// Готовность pending-таска: обычное ребро (After) ждёт success отправителя;
// стримовое (AfterStreamed) — ко-старт, достаточно starting/running/success
// (решение №6). Падение любой зависимости каскадно валит pending-потомков
// в upstream_failed. Ран завершён, когда все таски терминальны.
func buildPlan(tasks []dagModel.Task, statuses map[string]string) plan {
	result := plan{}

	// каскад upstream_failed до неподвижной точки: падение зависимостей
	// распространяется по цепочке pending-тасков за один проход планировщика
	st := lo.Assign(statuses)
	for {
		changed := false
		for _, t := range tasks {
			if st[t.Name] != runModel.TaskStatusPending {
				continue
			}
			failed := lo.ContainsBy(t.DependsOn, func(d dagModel.Dep) bool {
				return st[d.Task] == runModel.TaskStatusFailed || st[d.Task] == runModel.TaskStatusUpstreamFailed
			})
			if failed {
				st[t.Name] = runModel.TaskStatusUpstreamFailed
				result.UpstreamFailed = append(result.UpstreamFailed, t.Name)
				changed = true
			}
		}
		if !changed {
			break
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
