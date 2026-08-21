package model

import (
	"time"

	domainModel "github.com/rendau/loom/server/internal/domain/run/model"
)

type RunUpsert struct {
	PKId string

	DagName     *string
	Image       *string
	ImageDigest *string
	Trigger     *string
	Status      *string
	Manifest    *[]byte
	Params      *[]byte
	LogicalDate *time.Time
	FinishedAt  *time.Time
}

func (m *RunUpsert) CreateColumnMap() map[string]any {
	result := map[string]any{"id": m.PKId}
	if m.DagName != nil {
		result["dag_name"] = *m.DagName
	}
	if m.Image != nil {
		result["image"] = *m.Image
	}
	if m.ImageDigest != nil {
		result["image_digest"] = *m.ImageDigest
	}
	if m.Trigger != nil {
		result["trigger"] = *m.Trigger
	}
	if m.Status != nil {
		result["status"] = *m.Status
	}
	if m.Manifest != nil {
		result["manifest"] = *m.Manifest
	}
	if m.Params != nil {
		// nil-значение внутри указателя — явный NULL (ран без параметров)
		if len(*m.Params) == 0 {
			result["params"] = nil
		} else {
			result["params"] = *m.Params
		}
	}
	if m.LogicalDate != nil {
		result["logical_date"] = *m.LogicalDate
	}
	if m.FinishedAt != nil {
		result["finished_at"] = *m.FinishedAt
	}
	return result
}

func (m *RunUpsert) UpdateColumnMap() map[string]any {
	result := m.CreateColumnMap()
	delete(result, "id")
	return result
}

func (m *RunUpsert) PKColumnMap() map[string]any {
	return map[string]any{"id": m.PKId}
}

func (m *RunUpsert) ReturningColumnMap() map[string]any {
	return map[string]any{}
}

// DTO

// DecodeRunCreate собирает модель вставки нового рана из доменной Main.
func DecodeRunCreate(v *domainModel.Main) *RunUpsert {
	return &RunUpsert{
		PKId:        v.Id,
		DagName:     &v.DagName,
		Image:       &v.Image,
		ImageDigest: &v.ImageDigest,
		Trigger:     &v.Trigger,
		Status:      &v.Status,
		Manifest:    &v.Manifest,
		Params:      &v.Params,
		LogicalDate: &v.LogicalDate,
	}
}

func DecodeRunUpsert(v *domainModel.Edit) *RunUpsert {
	return &RunUpsert{
		Status:     v.Status,
		FinishedAt: v.FinishedAt,
	}
}
