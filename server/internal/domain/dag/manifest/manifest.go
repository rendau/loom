// Package manifest — разбор JSON-манифеста дага (вывод `describe` SDK).
// Единственное место на server, знающее этот формат: им пользуются repo
// дага и рана, планировщик и регистрация образа.
package manifest

import (
	"fmt"

	json "github.com/goccy/go-json"
	"github.com/samber/lo"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

type manifestDTO struct {
	SdkVersion    string         `json:"sdk_version"`
	Name          string         `json:"name"`
	Schedule      string         `json:"schedule"`
	Catchup       bool           `json:"catchup"`
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
	Pool          string             `json:"pool"`
	Priority      int                `json:"priority"`
	Secrets       []manifestSecret   `json:"secrets"`
}

type manifestSecret struct {
	Env    string `json:"env"`
	Secret string `json:"secret"`
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

func Parse(raw []byte) (*dagModel.Manifest, error) {
	var m manifestDTO
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	return &dagModel.Manifest{
		SdkVersion:    m.SdkVersion,
		Name:          m.Name,
		Schedule:      m.Schedule,
		Catchup:       m.Catchup,
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
		Pool:          v.Pool,
		Priority:      v.Priority,
		Secrets: lo.Map(v.Secrets, func(s manifestSecret, _ int) dagModel.SecretRef {
			return dagModel.SecretRef{Env: s.Env, Secret: s.Secret}
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
