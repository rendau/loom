package loom

import (
	"time"

	"github.com/samber/lo"
)

// Manifest — самоописание дага. Его печатает команда `describe`; по нему
// server валидирует и регистрирует даг при регистрации docker-образа.
type Manifest struct {
	SDKVersion    string         `json:"sdk_version"`
	Name          string         `json:"name"`
	MaxActiveRuns int            `json:"max_active_runs,omitempty"`
	Tasks         []TaskManifest `json:"tasks"`
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
	Priority      int                `json:"priority,omitempty"`
	Secrets       []SecretManifest   `json:"secrets,omitempty"`
	Variables     []VariableManifest `json:"variables,omitempty"`
}

// SecretManifest — инъекция секрета control plane в env контейнера таска.
// Description необязателен: подсказка тому, кто заполняет значение в админке.
type SecretManifest struct {
	Env         string `json:"env"`
	Secret      string `json:"secret"`
	Description string `json:"description,omitempty"`
}

// VariableManifest — инъекция переменной control plane в env контейнера
// таска (значение, в отличие от секрета, видно в админке).
type VariableManifest struct {
	Env         string `json:"env"`
	Variable    string `json:"variable"`
	Description string `json:"description,omitempty"`
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
		SDKVersion:    Version,
		Name:          d.name,
		MaxActiveRuns: d.maxActiveRuns,
		Tasks: lo.Map(d.order, func(name string, _ int) TaskManifest {
			t := d.tasks[name]
			m := TaskManifest{
				Name:          t.name,
				Retries:       t.retries,
				RetryDelaySec: int(t.retryDelay / time.Second),
				TimeoutSec:    int(t.timeout / time.Second),
				Priority:      t.priority,
			}
			if len(t.deps) > 0 {
				m.DependsOn = lo.Map(t.deps, encodeDepManifest)
			}
			if len(t.secrets) > 0 {
				m.Secrets = lo.Map(t.secrets, func(s secretRef, _ int) SecretManifest {
					return SecretManifest{Env: s.env, Secret: s.secret, Description: s.desc}
				})
			}
			if len(t.variables) > 0 {
				m.Variables = lo.Map(t.variables, func(v variableRef, _ int) VariableManifest {
					return VariableManifest{Env: v.env, Variable: v.variable, Description: v.desc}
				})
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
