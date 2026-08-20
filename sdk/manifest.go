package loom

import (
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
}

type DepManifest struct {
	Task     string `json:"task"`
	Streamed bool   `json:"streamed,omitempty"`
}

func (d *DAG) Manifest() Manifest {
	return Manifest{
		SDKVersion: Version,
		Name:       d.name,
		Schedule:   d.schedule,
		Tasks: lo.Map(d.order, func(name string, _ int) TaskManifest {
			t := d.tasks[name]
			m := TaskManifest{Name: t.name}
			if len(t.deps) > 0 {
				m.DependsOn = lo.Map(t.deps, encodeDepManifest)
			}
			return m
		}),
	}
}

func encodeDepManifest(v taskDep, _ int) DepManifest {
	return DepManifest{Task: v.task.name, Streamed: v.streamed}
}
