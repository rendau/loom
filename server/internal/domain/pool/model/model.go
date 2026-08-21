package model

import "time"

// DefaultPool — пул тасков, не указавших Pool в манифесте; создаётся сидом
// миграции.
const DefaultPool = "default"

// MaxSlots — верхняя граница слотов пула: защита от опечатки.
const MaxSlots = 10_000

// Main — пул слотов параллелизма (решение №26): таски всех дагов конкурируют
// за слоты своего пула; slots = 0 ставит пул на паузу.
type Main struct {
	Name       string
	Slots      int
	CreatedAt  time.Time
	ModifiedAt time.Time // zero — не изменялся
}
