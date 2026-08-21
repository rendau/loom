package model

import (
	"database/sql"
	"time"

	domainModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

type Select struct {
	Name        string
	Image       string
	ImageDigest string
	Schedule    string
	Paused      bool
	Manifest    []byte
	NextRunAt   sql.NullTime
	CreatedAt   time.Time
	ModifiedAt  sql.NullTime
}

func (m *Select) ListColumnMap() map[string]any {
	return map[string]any{
		"name":         &m.Name,
		"image":        &m.Image,
		"image_digest": &m.ImageDigest,
		"schedule":     &m.Schedule,
		"paused":       &m.Paused,
		"manifest":     &m.Manifest,
		"next_run_at":  &m.NextRunAt,
		"created_at":   &m.CreatedAt,
		"modified_at":  &m.ModifiedAt,
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
	sdkVersion, tasks := parseManifest(v.Manifest)

	return &domainModel.Main{
		Name:        v.Name,
		Image:       v.Image,
		ImageDigest: v.ImageDigest,
		Schedule:    v.Schedule,
		Paused:      v.Paused,
		SdkVersion:  sdkVersion,
		Tasks:       tasks,
		Manifest:    v.Manifest,
		NextRunAt:   v.NextRunAt.Time,
		CreatedAt:   v.CreatedAt,
		ModifiedAt:  v.ModifiedAt.Time,
	}
}
