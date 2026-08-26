package model

import (
	"database/sql"
	"time"

	domainModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

// Select читается из view dag_full: даг вместе с образом проекта и
// манифестом шаблона (см. миграцию 000005).
type Select struct {
	ProjectName      string
	Name             string
	Template         string
	Schedule         string
	Catchup          bool
	Paused           bool
	Pool             string
	NextRunAt        sql.NullTime
	CreatedAt        time.Time
	ModifiedAt       sql.NullTime
	Image            string
	ImageDigest      string
	AutoUpdate       bool
	SdkVersion       string
	Manifest         []byte
	TemplateOrphaned bool
}

func (m *Select) ListColumnMap() map[string]any {
	return map[string]any{
		"project_name":      &m.ProjectName,
		"name":              &m.Name,
		"template":          &m.Template,
		"schedule":          &m.Schedule,
		"catchup":           &m.Catchup,
		"paused":            &m.Paused,
		"pool":              &m.Pool,
		"next_run_at":       &m.NextRunAt,
		"created_at":        &m.CreatedAt,
		"modified_at":       &m.ModifiedAt,
		"image":             &m.Image,
		"image_digest":      &m.ImageDigest,
		"auto_update":       &m.AutoUpdate,
		"sdk_version":       &m.SdkVersion,
		"manifest":          &m.Manifest,
		"template_orphaned": &m.TemplateOrphaned,
	}
}

func (m *Select) PKColumnMap() map[string]any {
	return map[string]any{"project_name": m.ProjectName, "name": m.Name}
}

func (m *Select) DefaultSortColumns() []string {
	return []string{"project_name", "name"}
}

// DTO

func EncodeSelect(v *Select, _ int) *domainModel.Main {
	m := parseManifest(v.Manifest)

	return &domainModel.Main{
		Ref:              domainModel.NewRef(v.ProjectName, v.Name),
		Template:         v.Template,
		Schedule:         v.Schedule,
		Catchup:          v.Catchup,
		Paused:           v.Paused,
		Pool:             v.Pool,
		NextRunAt:        v.NextRunAt.Time,
		CreatedAt:        v.CreatedAt,
		ModifiedAt:       v.ModifiedAt.Time,
		Image:            v.Image,
		ImageDigest:      v.ImageDigest,
		AutoUpdate:       v.AutoUpdate,
		SdkVersion:       v.SdkVersion,
		MaxActiveRuns:    m.MaxActiveRuns,
		Tasks:            m.Tasks,
		Manifest:         v.Manifest,
		TemplateOrphaned: v.TemplateOrphaned,
	}
}
