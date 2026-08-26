package loom

import (
	"errors"
	"fmt"
	"time"

	"github.com/samber/lo"
)

// Catalog — самоописание docker-образа: все даги, объявленные в бинарнике
// (один образ может нести несколько шаблонов дагов). Его печатает команда
// `describe`; по нему server регистрирует проект и его шаблоны.
type Catalog struct {
	SDKVersion string       `json:"sdk_version"`
	Dags       []CatalogDag `json:"dags"`
}

// CatalogDag — один даг образа: либо манифест, либо ошибка валидации
// графа. Ошибка одного дага не отменяет регистрацию остальных — server
// заводит шаблоны по валидным и показывает ошибки по остальным.
type CatalogDag struct {
	Name     string    `json:"name"`
	Manifest *Manifest `json:"manifest,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// Manifest — самоописание дага: по нему server валидирует и регистрирует
// шаблон дага. Версия SDK общая на образ и живёт в Catalog.
type Manifest struct {
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

// buildCatalog собирает каталог образа. Ошибка возвращается только на
// уровне каталога (пустой набор, дубли имён) — такой образ нельзя
// зарегистрировать вообще; ошибка валидации отдельного дага уезжает в его
// CatalogDag.Error, не мешая остальным.
func buildCatalog(dags []*DAG) (Catalog, error) {
	if len(dags) == 0 {
		return Catalog{}, errors.New("no dags declared")
	}

	seen := make(map[string]bool, len(dags))
	c := Catalog{SDKVersion: Version, Dags: make([]CatalogDag, 0, len(dags))}

	for _, d := range dags {
		if seen[d.name] {
			return Catalog{}, fmt.Errorf("duplicate dag name %q", d.name)
		}
		seen[d.name] = true

		item := CatalogDag{Name: d.name}
		if err := d.Validate(); err != nil {
			item.Error = err.Error()
		} else {
			item.Manifest = new(d.Manifest())
		}
		c.Dags = append(c.Dags, item)
	}

	return c, nil
}

func (d *DAG) Manifest() Manifest {
	return Manifest{
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
