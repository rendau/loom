package model

import (
	"time"

	domainModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

// Upsert пишется в таблицу dag: образ и манифест сюда не входят — они
// живут в project и dag_template.
type Upsert struct {
	PKProject string
	PKName    string

	Template   *string
	Schedule   *string
	Catchup    *bool
	Paused     *bool
	Pool       *string
	ModifiedAt *time.Time
}

func (m *Upsert) CreateColumnMap() map[string]any {
	result := map[string]any{"project_name": m.PKProject, "name": m.PKName}
	if m.Template != nil {
		result["template"] = *m.Template
	}
	if m.Schedule != nil {
		result["schedule"] = *m.Schedule
	}
	if m.Catchup != nil {
		result["catchup"] = *m.Catchup
	}
	if m.Paused != nil {
		result["paused"] = *m.Paused
	}
	if m.Pool != nil {
		result["pool"] = *m.Pool
	}
	if m.ModifiedAt != nil {
		result["modified_at"] = *m.ModifiedAt
	}
	return result
}

func (m *Upsert) UpdateColumnMap() map[string]any {
	result := m.CreateColumnMap()
	delete(result, "project_name")
	delete(result, "name")
	return result
}

func (m *Upsert) PKColumnMap() map[string]any {
	return map[string]any{"project_name": m.PKProject, "name": m.PKName}
}

func (m *Upsert) ReturningColumnMap() map[string]any {
	return map[string]any{}
}

// DTO

func DecodeUpsert(v *domainModel.Edit) *Upsert {
	return &Upsert{
		Template:   v.Template,
		Schedule:   v.Schedule,
		Catchup:    v.Catchup,
		Paused:     v.Paused,
		Pool:       v.Pool,
		ModifiedAt: v.ModifiedAt,
	}
}
