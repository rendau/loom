package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/samber/lo"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/domain/user/model"
)

type Repo struct {
	*commonRepoPg.Base
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{Base: base}
}

// ── пользователи ────────────────────────────────────────

func (r *Repo) Create(ctx context.Context, m *model.Main, passwordHash string) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO app_user (id, username, password_hash, role) VALUES ($1, $2, $3, $4)`,
		m.Id, m.Username, passwordHash, m.Role)
	if err != nil {
		return fmt.Errorf("Create: %w", err)
	}
	return nil
}

func (r *Repo) Update(ctx context.Context, id string, passwordHash, role *string) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE app_user
		SET password_hash = coalesce($2, password_hash),
		    role = coalesce($3, role),
		    modified_at = now()
		WHERE id = $1`, id, passwordHash, role)
	if err != nil {
		return fmt.Errorf("Update: %w", err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, id string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `DELETE FROM app_user WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("Delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) List(ctx context.Context) ([]*model.Main, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT id, username, role, created_at, modified_at FROM app_user ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	defer rows.Close()

	var result []*model.Main
	for rows.Next() {
		var m model.Main
		var modifiedAt *time.Time
		if err = rows.Scan(&m.Id, &m.Username, &m.Role, &m.CreatedAt, &modifiedAt); err != nil {
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

func (r *Repo) Get(ctx context.Context, id string) (*model.Main, bool, error) {
	return r.getBy(ctx, `WHERE id = $1`, id)
}

// GetByUsername отдаёт пользователя вместе с хэшем пароля (для Login).
func (r *Repo) GetByUsername(ctx context.Context, username string) (*model.Main, string, bool, error) {
	var m model.Main
	var passwordHash string
	var modifiedAt *time.Time

	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT id, username, role, password_hash, created_at, modified_at
		FROM app_user WHERE username = $1`, username).
		Scan(&m.Id, &m.Username, &m.Role, &passwordHash, &m.CreatedAt, &modifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("GetByUsername: %w", err)
	}
	if modifiedAt != nil {
		m.ModifiedAt = *modifiedAt
	}
	return &m, passwordHash, true, nil
}

func (r *Repo) getBy(ctx context.Context, where string, args ...any) (*model.Main, bool, error) {
	var m model.Main
	var modifiedAt *time.Time

	err := r.TxM.GetConnection(ctx).QueryRow(ctx,
		`SELECT id, username, role, created_at, modified_at FROM app_user `+where, args...).
		Scan(&m.Id, &m.Username, &m.Role, &m.CreatedAt, &modifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get user: %w", err)
	}
	if modifiedAt != nil {
		m.ModifiedAt = *modifiedAt
	}
	return &m, true, nil
}

func (r *Repo) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	if err := r.TxM.GetConnection(ctx).QueryRow(ctx, `SELECT count(*) FROM app_user`).Scan(&n); err != nil {
		return 0, fmt.Errorf("CountUsers: %w", err)
	}
	return n, nil
}

// LockUsers блокирует таблицу пользователей до конца транзакции: делает
// создание первого админа race-safe при нескольких инстансах.
func (r *Repo) LockUsers(ctx context.Context) error {
	if _, err := r.TxM.GetConnection(ctx).Exec(ctx, `LOCK TABLE app_user IN ACCESS EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("LockUsers: %w", err)
	}
	return nil
}

// ── назначенные даги ────────────────────────────────────

func (r *Repo) SetUserDags(ctx context.Context, userId string, dags []dagModel.Ref) error {
	conn := r.TxM.GetConnection(ctx)
	if _, err := conn.Exec(ctx, `DELETE FROM user_dag WHERE user_id = $1`, userId); err != nil {
		return fmt.Errorf("SetUserDags delete: %w", err)
	}
	if len(dags) == 0 {
		return nil
	}

	projects := lo.Map(dags, func(v dagModel.Ref, _ int) string { return v.Project })
	names := lo.Map(dags, func(v dagModel.Ref, _ int) string { return v.Name })

	_, err := conn.Exec(ctx, `
		INSERT INTO user_dag (user_id, project_name, dag_name)
		SELECT $1, * FROM unnest($2::text[], $3::text[]) ON CONFLICT DO NOTHING`,
		userId, projects, names)
	if err != nil {
		return fmt.Errorf("SetUserDags insert: %w", err)
	}
	return nil
}

func (r *Repo) ListUserDags(ctx context.Context, userId string) ([]dagModel.Ref, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT project_name, dag_name FROM user_dag
		WHERE user_id = $1 ORDER BY project_name, dag_name`, userId)
	if err != nil {
		return nil, fmt.Errorf("ListUserDags: %w", err)
	}
	defer rows.Close()

	var result []dagModel.Ref
	for rows.Next() {
		var ref dagModel.Ref
		if err = rows.Scan(&ref.Project, &ref.Name); err != nil {
			return nil, fmt.Errorf("ListUserDags scan: %w", err)
		}
		result = append(result, ref)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListUserDags rows: %w", err)
	}
	return result, nil
}

func (r *Repo) HasUserDag(ctx context.Context, userId string, ref dagModel.Ref) (bool, error) {
	var exists bool
	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_dag
			WHERE user_id = $1 AND project_name = $2 AND dag_name = $3)`,
		userId, ref.Project, ref.Name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("HasUserDag: %w", err)
	}
	return exists, nil
}

// ── права на проекты: даг проекта считается назначенным целиком ─────────

func (r *Repo) SetUserProjects(ctx context.Context, userId string, projects []string) error {
	conn := r.TxM.GetConnection(ctx)
	if _, err := conn.Exec(ctx, `DELETE FROM user_project WHERE user_id = $1`, userId); err != nil {
		return fmt.Errorf("SetUserProjects delete: %w", err)
	}
	if len(projects) == 0 {
		return nil
	}
	_, err := conn.Exec(ctx, `
		INSERT INTO user_project (user_id, project_name)
		SELECT $1, unnest($2::text[]) ON CONFLICT DO NOTHING`, userId, projects)
	if err != nil {
		return fmt.Errorf("SetUserProjects insert: %w", err)
	}
	return nil
}

func (r *Repo) ListUserProjects(ctx context.Context, userId string) ([]string, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx,
		`SELECT project_name FROM user_project WHERE user_id = $1 ORDER BY project_name`, userId)
	if err != nil {
		return nil, fmt.Errorf("ListUserProjects: %w", err)
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("ListUserProjects scan: %w", err)
		}
		result = append(result, name)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListUserProjects rows: %w", err)
	}
	return result, nil
}

func (r *Repo) HasUserProject(ctx context.Context, userId, project string) (bool, error) {
	var exists bool
	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM user_project WHERE user_id = $1 AND project_name = $2)`,
		userId, project).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("HasUserProject: %w", err)
	}
	return exists, nil
}

// ── сессии ──────────────────────────────────────────────

func (r *Repo) CreateSession(ctx context.Context, tokenHash, userId string, expiresAt time.Time) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO session (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash, userId, expiresAt)
	if err != nil {
		return fmt.Errorf("CreateSession: %w", err)
	}
	return nil
}

// GetSessionUser возвращает пользователя живой сессии по хэшу токена.
func (r *Repo) GetSessionUser(ctx context.Context, tokenHash string) (*model.Main, bool, error) {
	var m model.Main
	var modifiedAt *time.Time

	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT u.id, u.username, u.role, u.created_at, u.modified_at
		FROM session s JOIN app_user u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash).
		Scan(&m.Id, &m.Username, &m.Role, &m.CreatedAt, &modifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("GetSessionUser: %w", err)
	}
	if modifiedAt != nil {
		m.ModifiedAt = *modifiedAt
	}
	return &m, true, nil
}

func (r *Repo) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := r.TxM.GetConnection(ctx).Exec(ctx,
		`DELETE FROM session WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("DeleteSession: %w", err)
	}
	return nil
}

// DeleteExpiredSessions — чистка истёкших (retention-цикл).
func (r *Repo) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `DELETE FROM session WHERE expires_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("DeleteExpiredSessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
