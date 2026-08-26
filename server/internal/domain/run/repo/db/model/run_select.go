package model

import (
	"database/sql"
	"time"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	domainModel "github.com/rendau/loom/server/internal/domain/run/model"
)

type RunSelect struct {
	Id          string
	ProjectName string
	DagName     string
	Template    string
	Image       string
	ImageDigest string
	Trigger     string
	Status      string
	Manifest    []byte
	Params      []byte
	LogicalDate time.Time
	CreatedAt   time.Time
	FinishedAt  sql.NullTime
}

func (m *RunSelect) ListColumnMap() map[string]any {
	return map[string]any{
		"id":           &m.Id,
		"project_name": &m.ProjectName,
		"dag_name":     &m.DagName,
		"template":     &m.Template,
		"image":        &m.Image,
		"image_digest": &m.ImageDigest,
		"trigger":      &m.Trigger,
		"status":       &m.Status,
		"manifest":     &m.Manifest,
		"params":       &m.Params,
		"logical_date": &m.LogicalDate,
		"created_at":   &m.CreatedAt,
		"finished_at":  &m.FinishedAt,
	}
}

func (m *RunSelect) PKColumnMap() map[string]any {
	return map[string]any{"id": m.Id}
}

func (m *RunSelect) DefaultSortColumns() []string {
	return []string{"created_at DESC"}
}

// DTO

func EncodeRunSelect(v *RunSelect, _ int) *domainModel.Main {
	return &domainModel.Main{
		Id:          v.Id,
		Dag:         dagModel.NewRef(v.ProjectName, v.DagName),
		Template:    v.Template,
		Image:       v.Image,
		ImageDigest: v.ImageDigest,
		Trigger:     v.Trigger,
		Status:      v.Status,
		Manifest:    v.Manifest,
		Params:      v.Params,
		LogicalDate: v.LogicalDate,
		CreatedAt:   v.CreatedAt,
		FinishedAt:  v.FinishedAt.Time,
	}
}
