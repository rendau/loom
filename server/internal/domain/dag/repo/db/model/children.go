package model

import (
	"github.com/rendau/loom/server/internal/domain/dag/manifest"
	domainModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

// parseManifest разбирает jsonb-колонку manifest; манифест валидировался
// при регистрации, поэтому ошибка парсинга здесь — деградация без паники.
func parseManifest(raw []byte) (sdkVersion string, tasks []domainModel.Task) {
	if len(raw) == 0 {
		return "", nil
	}
	m, err := manifest.Parse(raw)
	if err != nil {
		return "", nil
	}
	return m.SdkVersion, m.Tasks
}
