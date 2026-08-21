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
	AutoUpdate  bool
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
		"auto_update":  &m.AutoUpdate,
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
	m := parseManifest(v.Manifest)

	return &domainModel.Main{
		Name:          v.Name,
		Image:         v.Image,
		ImageDigest:   v.ImageDigest,
		Schedule:      v.Schedule,
		Catchup:       m.Catchup,
		MaxActiveRuns: m.MaxActiveRuns,
		Paused:        v.Paused,
		AutoUpdate:    v.AutoUpdate,
		SdkVersion:    m.SdkVersion,
		Tasks:         m.Tasks,
		Manifest:      v.Manifest,
		NextRunAt:     v.NextRunAt.Time,
		CreatedAt:     v.CreatedAt,
		ModifiedAt:    v.ModifiedAt.Time,
	}
}
