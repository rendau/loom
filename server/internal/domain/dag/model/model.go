package model

import (
	"time"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
)

// Main — зарегистрированный даг. Tasks/SdkVersion — распарсенный манифест,
// Manifest — его исходный JSON (снапшотится в ран при триггере).
type Main struct {
	Name        string
	Image       string
	ImageDigest string
	Schedule    string
	Paused      bool
	SdkVersion  string
	Tasks       []Task
	Manifest    []byte
	CreatedAt   time.Time
	ModifiedAt  time.Time // zero — не изменялся
}

type Task struct {
	Name      string
	DependsOn []Dep
}

type Dep struct {
	Task     string
	Streamed bool
}

// Manifest — распарсенный манифест дага (вывод `describe`).
type Manifest struct {
	SdkVersion string
	Name       string
	Schedule   string
	Tasks      []Task
}

// Edit — мутация дага (partial update).
type Edit struct {
	Image       *string
	ImageDigest *string
	Schedule    *string
	Paused      *bool
	Manifest    *[]byte
	ModifiedAt  *time.Time
}

// ListReq — параметры выборки.
type ListReq struct {
	commonModel.ListParams

	Paused *bool
}
