package model

import (
	"github.com/rendau/loom/server/internal/domain/dag/manifest"
	domainModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

// parseManifest разбирает jsonb-колонку manifest; манифест валидировался
// при регистрации, поэтому ошибка парсинга здесь — деградация без паники
// (пустой манифест).
func parseManifest(raw []byte) *domainModel.Manifest {
	if len(raw) == 0 {
		return &domainModel.Manifest{}
	}
	m, err := manifest.Parse(raw)
	if err != nil {
		return &domainModel.Manifest{}
	}
	return m
}
