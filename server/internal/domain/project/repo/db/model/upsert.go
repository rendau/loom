package model

import (
	"time"

	domainModel "github.com/rendau/loom/server/internal/domain/project/model"
)

type Upsert struct {
	PKName string

	Image          *string
	ImageDigest    *string
	ImageSizeBytes *int64
	AutoUpdate     *bool
	ModifiedAt     *time.Time
}

func (m *Upsert) CreateColumnMap() map[string]any {
	result := map[string]any{"name": m.PKName}
	if m.Image != nil {
		result["image"] = *m.Image
	}
	if m.ImageDigest != nil {
		result["image_digest"] = *m.ImageDigest
	}
	if m.ImageSizeBytes != nil {
		result["image_size_bytes"] = *m.ImageSizeBytes
	}
	if m.AutoUpdate != nil {
		result["auto_update"] = *m.AutoUpdate
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
		Image:          v.Image,
		ImageDigest:    v.ImageDigest,
		ImageSizeBytes: v.ImageSizeBytes,
		AutoUpdate:     v.AutoUpdate,
		ModifiedAt:     v.ModifiedAt,
	}
}
