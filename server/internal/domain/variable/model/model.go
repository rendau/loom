package model

import (
	"time"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
)

// MaxValueSize — лимит значения переменной.
const MaxValueSize = 64 * 1024

// Main — переменная для env-инъекции в поды тасков; в отличие от секрета
// значение хранится открыто и видно в админке.
type Main struct {
	Scope      commonModel.Scope
	Name       string
	Value      string
	CreatedAt  time.Time
	ModifiedAt time.Time // zero — не изменялась
}

// Resolved — результат резолва для launch: значение и скоуп-источник
// (какой уровень победил) — скоуп уходит в снапшот run_env.
type Resolved struct {
	Value string
	Scope commonModel.Scope
}
