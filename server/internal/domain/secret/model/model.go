package model

import "time"

// MaxValueSize — лимит значения секрета.
const MaxValueSize = 64 * 1024

// Meta — метаданные секрета; значение отдаётся только через GetValue
// (RBAC на уровне usecase).
type Meta struct {
	DagName    string // '' — глобальный скоуп
	Name       string
	CreatedAt  time.Time
	ModifiedAt time.Time // zero — не изменялся
}
