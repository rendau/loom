// Package model — модели артефактов на стороне control plane: данные живут
// на artifact-сервере (data plane), сюда попадают через artifactcli для
// админки (листинг, скачивание, статистика хранилища).
package model

import "time"

// Ref адресует артефакт (скоуп — попытка таска).
type Ref struct {
	RunId   string
	Task    string
	Attempt int32
	Name    string
}

// Info — метаданные артефакта для листинга.
type Info struct {
	Task     string
	Attempt  int32
	Name     string
	State    string // writing | committed | aborted
	Size     int64
	Modified time.Time // zero — неизвестно
}

// StorageDir — статистика каталога artifact-сервера.
type StorageDir struct {
	UsedBytes  int64
	TotalBytes int64
	FreeBytes  int64
}

// StorageStats — занятость хранилища artifact-сервера.
type StorageStats struct {
	Data StorageDir // артефакты (DATA_DIR)
	Logs StorageDir // логи тасков (LOG_DIR)
}
