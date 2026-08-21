package loom

import (
	"time"

	"github.com/samber/lo"
)

// Manifest — самоописание дага. Его печатает команда `describe`; по нему
// server валидирует и регистрирует даг при регистрации docker-образа.
type Manifest struct {
	SDKVersion string         `json:"sdk_version"`
	Name       string         `json:"name"`
	Schedule   string         `json:"schedule,omitempty"`
	Tasks      []TaskManifest `json:"tasks"`
}

type TaskManifest struct {
	Name      string        `json:"name"`
	DependsOn []DepManifest `json:"depends_on,omitempty"`
	// Политика ретраев и таймаут — с гранулярностью в секундах: их
	// интерпретирует control plane, суб-секундные значения ему не нужны.
	Retries       int                `json:"retries,omitempty"`
	RetryDelaySec int                `json:"retry_delay_sec,omitempty"`
	TimeoutSec    int                `json:"timeout_sec,omitempty"`
	Resources     *ResourcesManifest `json:"resources,omitempty"`
}

type DepManifest struct {
	Task     string `json:"task"`
	Streamed bool   `json:"streamed,omitempty"`
}

type ResourcesManifest struct {
	CPURequest    string `json:"cpu_request,omitempty"`
	CPULimit      string `json:"cpu_limit,omitempty"`
	MemoryRequest string `json:"memory_request,omitempty"`
	MemoryLimit   string `json:"memory_limit,omitempty"`
}

func (d *DAG) Manifest() Manifest {
	return Manifest{
		SDKVersion: Version,
		Name:       d.name,
		Schedule:   d.schedule,
		Tasks: lo.Map(d.order, func(name string, _ int) TaskManifest {
			t := d.tasks[name]
			m := TaskManifest{
				Name:          t.name,
				Retries:       t.retries,
				RetryDelaySec: int(t.retryDelay / time.Second),
				TimeoutSec:    int(t.timeout / time.Second),
			}
			if len(t.deps) > 0 {
				m.DependsOn = lo.Map(t.deps, encodeDepManifest)
			}
			if t.resources != (ResourceSpec{}) {
				m.Resources = &ResourcesManifest{
					CPURequest:    t.resources.CPURequest,
					CPULimit:      t.resources.CPULimit,
					MemoryRequest: t.resources.MemoryRequest,
					MemoryLimit:   t.resources.MemoryLimit,
				}
			}
			return m
		}),
	}
}

func encodeDepManifest(v taskDep, _ int) DepManifest {
	return DepManifest{Task: v.task.name, Streamed: v.streamed}
}
