package model

import (
	"time"

	domainModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

type Upsert struct {
	PKName string

	Image       *string
	ImageDigest *string
	Schedule    *string
	Paused      *bool
	AutoUpdate  *bool
	Manifest    *[]byte
	ModifiedAt  *time.Time
}

func (m *Upsert) CreateColumnMap() map[string]any {
	result := map[string]any{"name": m.PKName}
	if m.Image != nil {
		result["image"] = *m.Image
	}
	if m.ImageDigest != nil {
		result["image_digest"] = *m.ImageDigest
	}
	if m.Schedule != nil {
		result["schedule"] = *m.Schedule
	}
	if m.Paused != nil {
		result["paused"] = *m.Paused
	}
	if m.AutoUpdate != nil {
		result["auto_update"] = *m.AutoUpdate
	}
	if m.Manifest != nil {
		result["manifest"] = *m.Manifest
	}
	if m.ModifiedAt != nil {
		result["modified_at"] = *m.ModifiedAt
	}
	return result
}

func (m *Upsert) UpdateColumnMap() map[string]any {
	result := m.CreateColumnMap()
	delete(result, "name")
	return result
}

func (m *Upsert) PKColumnMap() map[string]any {
	return map[string]any{"name": m.PKName}
}

func (m *Upsert) ReturningColumnMap() map[string]any {
	return map[string]any{}
}

// DTO

func DecodeUpsert(v *domainModel.Edit) *Upsert {
	return &Upsert{
		Image:       v.Image,
		ImageDigest: v.ImageDigest,
		Schedule:    v.Schedule,
		Paused:      v.Paused,
		AutoUpdate:  v.AutoUpdate,
		Manifest:    v.Manifest,
		ModifiedAt:  v.ModifiedAt,
	}
}
