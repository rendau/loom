// Package manifest — разбор JSON-каталога образа и манифестов дагов
// (вывод `describe` SDK). Единственное место на server, знающее этот
// формат: им пользуются repo шаблона и рана, планировщик и регистрация
// образа.
package manifest

import (
	"encoding/json"
	"fmt"

	"github.com/samber/lo"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

// catalogDTO — вывод `describe`: даги образа и общая на образ версия SDK.
type catalogDTO struct {
	SdkVersion string          `json:"sdk_version"`
	Dags       []catalogDagDTO `json:"dags"`
}

type catalogDagDTO struct {
	Name     string           `json:"name"`
	Manifest *json.RawMessage `json:"manifest"`
	Error    string           `json:"error"`
}

type manifestDTO struct {
	Name          string         `json:"name"`
	MaxActiveRuns int            `json:"max_active_runs"`
	Tasks         []manifestTask `json:"tasks"`
}

type manifestTask struct {
	Name          string             `json:"name"`
	DependsOn     []manifestDep      `json:"depends_on"`
	Retries       int                `json:"retries"`
	RetryDelaySec int                `json:"retry_delay_sec"`
	TimeoutSec    int                `json:"timeout_sec"`
	Resources     *manifestResources `json:"resources"`
	Priority      int                `json:"priority"`
	Secrets       []manifestSecret   `json:"secrets"`
	Variables     []manifestVariable `json:"variables"`
}

type manifestSecret struct {
	Env         string `json:"env"`
	Secret      string `json:"secret"`
	Description string `json:"description"`
}

type manifestVariable struct {
	Env         string `json:"env"`
	Variable    string `json:"variable"`
	Description string `json:"description"`
}

type manifestDep struct {
	Task     string `json:"task"`
	Streamed bool   `json:"streamed"`
}

type manifestResources struct {
	CPURequest    string `json:"cpu_request"`
	CPULimit      string `json:"cpu_limit"`
	MemoryRequest string `json:"memory_request"`
	MemoryLimit   string `json:"memory_limit"`
}

// ParseCatalog разбирает вывод `describe`. Манифест каждого дага
// сохраняется и в исходном виде (Raw): он снапшотится в шаблон и в ран.
func ParseCatalog(raw []byte) (*dagModel.Catalog, error) {
	var c catalogDTO
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("unmarshal catalog: %w", err)
	}

	result := &dagModel.Catalog{
		SdkVersion: c.SdkVersion,
		Dags:       make([]dagModel.CatalogDag, 0, len(c.Dags)),
	}

	for _, d := range c.Dags {
		item := dagModel.CatalogDag{Name: d.Name, Error: d.Error}

		if d.Manifest != nil {
			m, err := Parse(*d.Manifest)
			if err != nil {
				return nil, fmt.Errorf("dag %q: %w", d.Name, err)
			}
			item.Manifest = m
			item.Raw = *d.Manifest
		}

		result.Dags = append(result.Dags, item)
	}

	return result, nil
}

func Parse(raw []byte) (*dagModel.Manifest, error) {
	var m manifestDTO
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	return &dagModel.Manifest{
		Name:          m.Name,
		MaxActiveRuns: m.MaxActiveRuns,
		Tasks:         lo.Map(m.Tasks, encodeManifestTask),
	}, nil
}

func encodeManifestTask(v manifestTask, _ int) dagModel.Task {
	result := dagModel.Task{
		Name:          v.Name,
		DependsOn:     lo.Map(v.DependsOn, encodeManifestDep),
		Retries:       v.Retries,
		RetryDelaySec: v.RetryDelaySec,
		TimeoutSec:    v.TimeoutSec,
		Priority:      v.Priority,
		Secrets: lo.Map(v.Secrets, func(s manifestSecret, _ int) dagModel.SecretRef {
			return dagModel.SecretRef{Env: s.Env, Secret: s.Secret, Description: s.Description}
		}),
		Variables: lo.Map(v.Variables, func(vr manifestVariable, _ int) dagModel.VariableRef {
			return dagModel.VariableRef{Env: vr.Env, Variable: vr.Variable, Description: vr.Description}
		}),
	}
	if v.Resources != nil {
		result.Resources = &dagModel.TaskResources{
			CPURequest:    v.Resources.CPURequest,
			CPULimit:      v.Resources.CPULimit,
			MemoryRequest: v.Resources.MemoryRequest,
			MemoryLimit:   v.Resources.MemoryLimit,
		}
	}
	return result
}

func encodeManifestDep(v manifestDep, _ int) dagModel.Dep {
	return dagModel.Dep{Task: v.Task, Streamed: v.Streamed}
}
