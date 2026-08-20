package model

import (
	"database/sql"
	"time"

	domainModel "github.com/rendau/loom/server/internal/domain/run/model"
)

type RunSelect struct {
	Id          string
	DagName     string
	Image       string
	ImageDigest string
	Trigger     string
	Status      string
	Manifest    []byte
	CreatedAt   time.Time
	FinishedAt  sql.NullTime
}

func (m *RunSelect) ListColumnMap() map[string]any {
	return map[string]any{
		"id":           &m.Id,
		"dag_name":     &m.DagName,
		"image":        &m.Image,
		"image_digest": &m.ImageDigest,
		"trigger":      &m.Trigger,
		"status":       &m.Status,
		"manifest":     &m.Manifest,
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
		DagName:     v.DagName,
		Image:       v.Image,
		ImageDigest: v.ImageDigest,
		Trigger:     v.Trigger,
		Status:      v.Status,
		Manifest:    v.Manifest,
		CreatedAt:   v.CreatedAt,
		FinishedAt:  v.FinishedAt.Time,
	}
}
