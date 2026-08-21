package db

import (
	"context"
	"fmt"
	"time"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/pool/model"
)

type Repo struct {
	*commonRepoPg.Base
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{Base: base}
}

func (r *Repo) List(ctx context.Context) ([]*model.Main, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT name, slots, created_at, modified_at FROM pool ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	defer rows.Close()

	var result []*model.Main
	for rows.Next() {
		var m model.Main
		var modifiedAt *time.Time
		if err = rows.Scan(&m.Name, &m.Slots, &m.CreatedAt, &modifiedAt); err != nil {
			return nil, fmt.Errorf("List scan: %w", err)
		}
		if modifiedAt != nil {
			m.ModifiedAt = *modifiedAt
		}
		result = append(result, &m)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("List rows: %w", err)
	}
	return result, nil
}

// Set создаёт пул или обновляет число слотов существующего.
func (r *Repo) Set(ctx context.Context, name string, slots int) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO pool (name, slots) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET slots = excluded.slots, modified_at = now()`,
		name, slots)
	if err != nil {
		return fmt.Errorf("Set: %w", err)
	}
	return nil
}

// ListMissing возвращает имена из names, которых нет в таблице пулов.
func (r *Repo) ListMissing(ctx context.Context, names []string) ([]string, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT n FROM unnest($1::text[]) AS n
		WHERE NOT EXISTS (SELECT 1 FROM pool WHERE pool.name = n)`, names)
	if err != nil {
		return nil, fmt.Errorf("ListMissing: %w", err)
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("ListMissing scan: %w", err)
		}
		result = append(result, name)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListMissing rows: %w", err)
	}
	return result, nil
}
