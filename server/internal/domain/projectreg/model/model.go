package model

import "time"

// Статусы регистрации.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// Источники регистрации.
const (
	SourceManual = "manual" // регистрация из админки
	SourceAuto   = "auto"   // перерегистрация dagsync (digest тега изменился)
)

// Main — запись очереди регистраций проектов: один образ = один проект,
// его даги (шаблоны) обновляются целиком за одну обработку.
type Main struct {
	Id          string
	ProjectName string
	Image       string
	Source      string
	// AutoUpdate — желаемое авто-обновление; применяется только при
	// создании проекта, nil — не задано.
	AutoUpdate *bool
	// CreateDags — заводить даги-инстансы по новым шаблонам образа.
	CreateDags bool
	Status     string
	Error      string
	// Result — итог по дагам образа: у каждого либо ошибка манифеста, либо
	// признак того, что от него заведён инстанс.
	Result     []DagResult
	CreatedAt  time.Time
	StartedAt  time.Time // zero — обработка не началась
	FinishedAt time.Time // zero — не завершена
}

// DagResult — судьба одного дага образа при регистрации.
type DagResult struct {
	Name    string `json:"name"`
	Error   string `json:"error,omitempty"`
	Created bool   `json:"created,omitempty"` // заведён новый даг-инстанс
}

// EnqueueSpec — постановка регистрации в очередь.
type EnqueueSpec struct {
	ProjectName string
	Image       string
	Source      string
	AutoUpdate  *bool
	CreateDags  bool
}

// ListReq — выборка регистраций (без пагинации: записи транзиентны,
// старые чистятся по TTL).
type ListReq struct {
	ProjectName *string
	OnlyActive  bool  // только pending | running
	Limit       int64 // 0 — дефолт репозитория
}
