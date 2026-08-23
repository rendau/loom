package model

import "time"

// MaxValueSize — лимит значения секрета.
const MaxValueSize = 64 * 1024

// Meta — метаданные секрета; значение наружу не отдаётся (write-only API).
type Meta struct {
	Name       string
	CreatedAt  time.Time
	ModifiedAt time.Time // zero — не изменялся
}
