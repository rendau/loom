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

// Main — запись очереди регистраций дагов.
type Main struct {
	Id     string
	Image  string
	Source string
	// Желаемые настройки: применяются только если даг создаётся впервые;
	// nil — не задано.
	Schedule   *string
	Catchup    *bool
	Paused     *bool
	AutoUpdate *bool
	Pool       *string
	Status     string
	Error      string
	DagName    string // manual: пусто до успешного describe
	CreatedAt  time.Time
	StartedAt  time.Time // zero — обработка не началась
	FinishedAt time.Time // zero — не завершена
}

// EnqueueSpec — постановка регистрации в очередь.
type EnqueueSpec struct {
	Image      string
	Source     string
	DagName    string // auto: имя дага известно сразу
	Schedule   *string
	Catchup    *bool
	Paused     *bool
	AutoUpdate *bool
	Pool       *string
}

// ListReq — выборка регистраций (без пагинации: записи транзиентны,
// старые чистятся по TTL).
type ListReq struct {
	DagName    *string
	OnlyActive bool  // только pending | running
	Limit      int64 // 0 — дефолт репозитория
}
