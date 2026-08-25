// Package artifact — доступ админки к артефактам ранов: control plane
// валидирует ран по БД и проксирует artifact-сервер (data plane наружу не
// выставляется). Читать может любой аутентифицированный — как логи тасков.
package artifact

import (
	"context"
	"fmt"
	"io"

	"github.com/rendau/loom/server/internal/domain/artifact/model"
	"github.com/rendau/loom/server/internal/errs"
)

// maxPreviewBytes — потолок превью содержимого (limit_bytes): для просмотра
// в админке; целиком артефакт качается без лимита стримом.
const maxPreviewBytes = 1 << 20

type Usecase struct {
	svc    ServiceI
	runSvc RunServiceI
}

func New(svc ServiceI, runSvc RunServiceI) *Usecase {
	return &Usecase{svc: svc, runSvc: runSvc}
}

// List — метаданные артефактов рана (по всем таскам и попыткам).
func (u *Usecase) List(ctx context.Context, runId string) ([]model.Info, error) {
	if runId == "" {
		return nil, errs.IdRequired
	}
	if _, _, err := u.runSvc.Get(ctx, runId, true); err != nil {
		return nil, fmt.Errorf("runSvc.Get: %w", err)
	}

	items, err := u.svc.ListRunArtifacts(ctx, runId)
	if err != nil {
		return nil, fmt.Errorf("svc.ListRunArtifacts: %w", err)
	}
	return items, nil
}

// StorageStats — занятость хранилища artifact-сервера (обзор админки).
func (u *Usecase) StorageStats(ctx context.Context) (model.StorageStats, error) {
	stats, err := u.svc.GetStorageStats(ctx)
	if err != nil {
		return model.StorageStats{}, fmt.Errorf("svc.GetStorageStats: %w", err)
	}
	return stats, nil
}

// Read стримит содержимое артефакта в w; limit > 0 — превью первых limit
// байт (обрезается потолком maxPreviewBytes).
func (u *Usecase) Read(ctx context.Context, ref model.Ref, limit int64, w io.Writer) error {
	if ref.RunId == "" || ref.Task == "" || ref.Name == "" || ref.Attempt < 1 {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: "требуется полный ref артефакта"}
	}
	if limit > maxPreviewBytes {
		limit = maxPreviewBytes
	}

	if err := u.svc.ReadArtifactTo(ctx, ref, 0, limit, w); err != nil {
		return fmt.Errorf("svc.ReadArtifactTo: %w", err)
	}
	return nil
}
