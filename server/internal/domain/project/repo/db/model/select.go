package model

import (
	"database/sql"
	"time"

	domainModel "github.com/rendau/loom/server/internal/domain/project/model"
)

type Select struct {
	Name           string
	Image          string
	ImageDigest    string
	ImageSizeBytes int64
	AutoUpdate     bool
	CreatedAt      time.Time
	ModifiedAt     sql.NullTime
}

func (m *Select) ListColumnMap() map[string]any {
	return map[string]any{
		"name":             &m.Name,
		"image":            &m.Image,
		"image_digest":     &m.ImageDigest,
		"image_size_bytes": &m.ImageSizeBytes,
		"auto_update":      &m.AutoUpdate,
		"created_at":       &m.CreatedAt,
		"modified_at":      &m.ModifiedAt,
	}
}

func (m *Select) PKColumnMap() map[string]any {
	return map[string]any{"name": m.Name}
}

func (m *Select) DefaultSortColumns() []string {
	return []string{"name"}
}

// DTO

func EncodeSelect(v *Select, _ int) *domainModel.Main {
	return &domainModel.Main{
		Name:           v.Name,
		Image:          v.Image,
		ImageDigest:    v.ImageDigest,
		ImageSizeBytes: v.ImageSizeBytes,
		AutoUpdate:     v.AutoUpdate,
		CreatedAt:      v.CreatedAt,
		ModifiedAt:     v.ModifiedAt.Time,
	}
}
