package util

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"

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

// CronNext возвращает ближайшее время срабатывания cron-выражения после
// after. Формат — стандартные 5 полей плюс дескрипторы (@daily и т.п.);
// времена считаются в UTC-независимой локали времени after.
func CronNext(expr string, after time.Time) (time.Time, error) {
	schedule, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cron %q: %w", expr, err)
	}
	return schedule.Next(after), nil
}
