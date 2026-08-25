package model

import "time"

// MaxValueSize — лимит значения переменной.
const MaxValueSize = 64 * 1024

// Main — переменная для env-инъекции в поды тасков; в отличие от секрета
// значение хранится открыто и видно в админке.
type Main struct {
	DagName    string // '' — глобальный скоуп
	Name       string
	Value      string
	CreatedAt  time.Time
	ModifiedAt time.Time // zero — не изменялась
}

// Resolved — результат резолва для launch: значение и скоуп-источник
// (локальный перекрыл глобальный или нет) — скоуп уходит в снапшот run_env.
type Resolved struct {
	Value string
	Scope string // '' — глобальный, иначе имя дага
}
