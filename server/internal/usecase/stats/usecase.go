package stats

import (
	"context"
	"fmt"

	"github.com/rendau/loom/server/internal/domain/stats/model"
)

// Usecase — сводка дашборда; доступна любому аутентифицированному
// пользователю (только чтение агрегатов).
type Usecase struct {
	svc ServiceI
}

func New(svc ServiceI) *Usecase {
	return &Usecase{svc: svc}
}

func (u *Usecase) Dashboard(ctx context.Context) (*model.Dashboard, error) {
	result, err := u.svc.Dashboard(ctx)
	if err != nil {
		return nil, fmt.Errorf("svc.Dashboard: %w", err)
	}
	return result, nil
}
