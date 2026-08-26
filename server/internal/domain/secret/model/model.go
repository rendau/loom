package model

import (
	"time"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
)

// MaxValueSize — лимит значения секрета.
const MaxValueSize = 64 * 1024

// Meta — метаданные секрета; значение отдаётся только через GetValue
// (RBAC на уровне usecase).
type Meta struct {
	Scope      commonModel.Scope
	Name       string
	CreatedAt  time.Time
	ModifiedAt time.Time // zero — не изменялся
}

// Resolved — результат резолва для launch: значение и скоуп-источник.
// В снапшот run_env уходит только скоуп — значения секретов не хранятся.
type Resolved struct {
	Value []byte
	Scope commonModel.Scope
}
