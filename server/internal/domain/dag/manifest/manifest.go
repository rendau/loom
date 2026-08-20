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
	SdkVersion string         `json:"sdk_version"`
	Name       string         `json:"name"`
	Schedule   string         `json:"schedule"`
	Tasks      []manifestTask `json:"tasks"`
}

type manifestTask struct {
	Name      string        `json:"name"`
	DependsOn []manifestDep `json:"depends_on"`
}

type manifestDep struct {
	Task     string `json:"task"`
	Streamed bool   `json:"streamed"`
}

func Parse(raw []byte) (*dagModel.Manifest, error) {
	var m manifestDTO
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	return &dagModel.Manifest{
		SdkVersion: m.SdkVersion,
		Name:       m.Name,
		Schedule:   m.Schedule,
		Tasks:      lo.Map(m.Tasks, encodeManifestTask),
	}, nil
}

func encodeManifestTask(v manifestTask, _ int) dagModel.Task {
	return dagModel.Task{
		Name:      v.Name,
		DependsOn: lo.Map(v.DependsOn, encodeManifestDep),
	}
}

func encodeManifestDep(v manifestDep, _ int) dagModel.Dep {
	return dagModel.Dep{Task: v.Task, Streamed: v.Streamed}
}
