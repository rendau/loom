package model

import (
	"time"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

// Main — проект: один docker-образ и его даги. Образ регистрируется целиком
// (`describe` отдаёт каталог), поэтому digest и авто-обновление живут здесь,
// а не на каждом даге.
type Main struct {
	Name        string
	Image       string // url образа, как задан при регистрации (тег)
	ImageDigest string // закреплённый digest (repo@sha256:...)
	// Размер образа в registry (config + слои, сжатыми) — 0, если registry
	// не удалось опросить при регистрации.
	ImageSizeBytes int64
	AutoUpdate     bool // poll-синк новой версии образа
	CreatedAt      time.Time
	ModifiedAt     time.Time // zero — не изменялся

	// Шаблоны образа — не колонка: дособираются usecase'ом для карточки
	// проекта.
	Templates []Template
	// Число заведённых дагов — дособирается для списка проектов.
	DagCount int
}

// Template — даг, объявленный в образе. От него заводят даги-инстансы;
// манифест хранится один раз здесь, а не копией у каждого инстанса.
type Template struct {
	Project    string
	Name       string
	SdkVersion string
	// Orphaned — шаблон пропал из образа при последней регистрации: его
	// инстансы продолжают работать на последнем известном манифесте.
	Orphaned      bool
	MaxActiveRuns int
	Tasks         []dagModel.Task
	Manifest      []byte
	CreatedAt     time.Time
	ModifiedAt    time.Time
	// Число дагов, заведённых от шаблона — дособирается для карточки.
	DagCount int
}

// Edit — мутация проекта (partial update).
type Edit struct {
	Image          *string
	ImageDigest    *string
	ImageSizeBytes *int64
	AutoUpdate     *bool
	ModifiedAt     *time.Time
}

// ListReq — параметры выборки.
type ListReq struct {
	commonModel.ListParams

	AutoUpdate *bool
}

// RegisterSpec — вход регистрации: образ и результат его инспекции.
type RegisterSpec struct {
	Name        string
	Image       string
	ImageDigest string
	// Размер образа в registry; 0 — не удалось опросить, прежнее значение
	// проекта сохраняется (registry мог быть недоступен разово).
	ImageSizeBytes int64
	// AutoUpdate nil — сохранить текущее значение флага: авто-
	// перерегистрация его не трогает.
	AutoUpdate *bool
}

// TemplateEdit — результат разбора каталога образа для одного дага:
// либо манифест, либо ошибка его валидации.
type TemplateEdit struct {
	Name       string
	SdkVersion string
	Manifest   []byte
	Error      string
}
