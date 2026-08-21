package db

import (
	"context"
	"fmt"
	"time"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/secret/model"
)

type Repo struct {
	*commonRepoPg.Base
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{Base: base}
}

func (r *Repo) Set(ctx context.Context, name string, value []byte) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO secret (name, value) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET value = excluded.value, modified_at = now()`,
		name, value)
	if err != nil {
		return fmt.Errorf("Set: %w", err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, name string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `DELETE FROM secret WHERE name = $1`, name)
	if err != nil {
		return false, fmt.Errorf("Delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) ListMeta(ctx context.Context) ([]*model.Meta, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT name, created_at, modified_at FROM secret ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("ListMeta: %w", err)
	}
	defer rows.Close()

	var result []*model.Meta
	for rows.Next() {
		var m model.Meta
		var modifiedAt *time.Time
		if err = rows.Scan(&m.Name, &m.CreatedAt, &modifiedAt); err != nil {
			return nil, fmt.Errorf("ListMeta scan: %w", err)
		}
		if modifiedAt != nil {
			m.ModifiedAt = *modifiedAt
		}
		result = append(result, &m)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListMeta rows: %w", err)
	}
	return result, nil
}

// GetValues возвращает сырые (возможно зашифрованные) значения секретов по
// именам; отсутствующие имена в результат не попадают.
func (r *Repo) GetValues(ctx context.Context, names []string) (map[string][]byte, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT name, value FROM secret WHERE name = ANY($1)`, names)
	if err != nil {
		return nil, fmt.Errorf("GetValues: %w", err)
	}
	defer rows.Close()

	result := map[string][]byte{}
	for rows.Next() {
		var name string
		var value []byte
		if err = rows.Scan(&name, &value); err != nil {
			return nil, fmt.Errorf("GetValues scan: %w", err)
		}
		result[name] = value
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("GetValues rows: %w", err)
	}
	return result, nil
}
