package util

import (
	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	"github.com/rendau/loom/server/internal/errs"
)

const MaxPageSize = 1000

// RequirePageSize валидирует пагинацию списочных запросов: без OnlyCount
// требуется page_size в (0, max].
func RequirePageSize(pars commonModel.ListParams, max int64) error {
	if max <= 0 {
		max = MaxPageSize
	}
	if pars.OnlyCount {
		return nil
	}
	if pars.PageSize <= 0 || pars.PageSize > max {
		return errs.IncorrectPageSize
	}
	return nil
}
