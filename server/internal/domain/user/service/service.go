// Package service — пользователи админки, сессии и права на даги.
// Аутентификация: пароль (bcrypt) → opaque-токен сессии; в БД хранится
// только sha256 токена, поэтому дамп базы войти не позволяет, а logout и
// удаление пользователя гасят сессию мгновенно.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/rendau/loom/server/internal/domain/user/model"
	"github.com/rendau/loom/server/internal/errs"
)

// usernameRe — допустимые логины.
var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._@-]{1,63}$`)

type Service struct {
	repoDb RepoDbI
	txm    TxManagerI
}

func New(repoDb RepoDbI, txm TxManagerI) *Service {
	return &Service{repoDb: repoDb, txm: txm}
}

// UsersExist — заведён ли хоть один пользователь (экран первичной настройки).
func (s *Service) UsersExist(ctx context.Context) (bool, error) {
	n, err := s.repoDb.CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("repoDb.CountUsers: %w", err)
	}
	return n > 0, nil
}

// CreateFirstAdmin создаёт первого администратора; работает только пока
// пользователей нет. Блокировка таблицы делает операцию race-safe между
// инстансами control plane.
func (s *Service) CreateFirstAdmin(ctx context.Context, username, password string) (*model.Main, error) {
	var result *model.Main
	err := s.txm.TxFn(ctx, func(ctx context.Context) error {
		if err := s.repoDb.LockUsers(ctx); err != nil {
			return err
		}
		n, err := s.repoDb.CountUsers(ctx)
		if err != nil {
			return fmt.Errorf("repoDb.CountUsers: %w", err)
		}
		if n > 0 {
			return errs.ErrFull{Err: errs.InvalidRequest, Desc: "пользователи уже созданы"}
		}

		result, err = s.create(ctx, model.CreateSpec{
			Username: username,
			Password: password,
			Role:     model.RoleAdmin,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) Create(ctx context.Context, spec model.CreateSpec) (*model.Main, error) {
	var result *model.Main
	err := s.txm.TxFn(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.create(ctx, spec)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) create(ctx context.Context, spec model.CreateSpec) (*model.Main, error) {
	if !usernameRe.MatchString(spec.Username) {
		return nil, errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("недопустимый логин %q", spec.Username)}
	}
	if err := validatePassword(spec.Password); err != nil {
		return nil, err
	}
	if spec.Role != model.RoleAdmin && spec.Role != model.RoleUser {
		return nil, errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("недопустимая роль %q", spec.Role)}
	}

	if _, _, found, err := s.repoDb.GetByUsername(ctx, spec.Username); err != nil {
		return nil, fmt.Errorf("repoDb.GetByUsername: %w", err)
	} else if found {
		return nil, errs.UserExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(spec.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	m := &model.Main{Id: newUserId(), Username: spec.Username, Role: spec.Role}
	if err = s.repoDb.Create(ctx, m, string(hash)); err != nil {
		return nil, fmt.Errorf("repoDb.Create: %w", err)
	}
	if len(spec.DagNames) > 0 && spec.Role != model.RoleAdmin {
		if err = s.repoDb.SetUserDags(ctx, m.Id, spec.DagNames); err != nil {
			return nil, fmt.Errorf("repoDb.SetUserDags: %w", err)
		}
		m.DagNames = spec.DagNames
	}

	created, _, err := s.repoDb.Get(ctx, m.Id)
	if err != nil {
		return nil, fmt.Errorf("repoDb.Get: %w", err)
	}
	created.DagNames = m.DagNames
	return created, nil
}

func (s *Service) Update(ctx context.Context, id string, spec model.UpdateSpec) error {
	return s.txm.TxFn(ctx, func(ctx context.Context) error {
		user, found, err := s.repoDb.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("repoDb.Get: %w", err)
		}
		if !found {
			return errs.UserNotFound
		}

		var hash *string
		if spec.Password != nil {
			if err = validatePassword(*spec.Password); err != nil {
				return err
			}
			raw, hErr := bcrypt.GenerateFromPassword([]byte(*spec.Password), bcrypt.DefaultCost)
			if hErr != nil {
				return fmt.Errorf("hash password: %w", hErr)
			}
			hash = new(string(raw))
		}
		if spec.Role != nil && *spec.Role != model.RoleAdmin && *spec.Role != model.RoleUser {
			return errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("недопустимая роль %q", *spec.Role)}
		}

		if err = s.repoDb.Update(ctx, id, hash, spec.Role); err != nil {
			return fmt.Errorf("repoDb.Update: %w", err)
		}

		// admin'у назначения дагов не нужны — ему доступны все
		role := user.Role
		if spec.Role != nil {
			role = *spec.Role
		}
		if spec.SetDagNames || role == model.RoleAdmin {
			dagNames := spec.DagNames
			if role == model.RoleAdmin {
				dagNames = nil
			}
			if err = s.repoDb.SetUserDags(ctx, id, dagNames); err != nil {
				return fmt.Errorf("repoDb.SetUserDags: %w", err)
			}
		}
		return nil
	})
}

func (s *Service) Delete(ctx context.Context, id string) error {
	found, err := s.repoDb.Delete(ctx, id)
	if err != nil {
		return fmt.Errorf("repoDb.Delete: %w", err)
	}
	if !found {
		return errs.UserNotFound
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]*model.Main, error) {
	users, err := s.repoDb.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("repoDb.List: %w", err)
	}
	for _, u := range users {
		if u.Role == model.RoleAdmin {
			continue
		}
		if u.DagNames, err = s.repoDb.ListUserDags(ctx, u.Id); err != nil {
			return nil, fmt.Errorf("repoDb.ListUserDags: %w", err)
		}
	}
	return users, nil
}

func (s *Service) Get(ctx context.Context, id string) (*model.Main, error) {
	user, found, err := s.repoDb.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("repoDb.Get: %w", err)
	}
	if !found {
		return nil, errs.UserNotFound
	}
	if user.Role != model.RoleAdmin {
		if user.DagNames, err = s.repoDb.ListUserDags(ctx, user.Id); err != nil {
			return nil, fmt.Errorf("repoDb.ListUserDags: %w", err)
		}
	}
	return user, nil
}

// ── сессии ──────────────────────────────────────────────

// Login проверяет пароль и заводит сессию; возвращает сырой токен (в БД
// уходит только его хэш).
func (s *Service) Login(ctx context.Context, username, password string) (string, *model.Main, time.Time, error) {
	user, hash, found, err := s.repoDb.GetByUsername(ctx, username)
	if err != nil {
		return "", nil, time.Time{}, fmt.Errorf("repoDb.GetByUsername: %w", err)
	}
	if !found {
		// сверяем с фиктивным хэшем: время ответа не выдаёт существование логина
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$"+"0123456789012345678901"+
			"0123456789012345678901234567890"), []byte(password))
		return "", nil, time.Time{}, errs.InvalidCredentials
	}
	if err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", nil, time.Time{}, errs.InvalidCredentials
	}

	token, err := newToken()
	if err != nil {
		return "", nil, time.Time{}, err
	}
	expiresAt := time.Now().Add(model.SessionTTL)
	if err = s.repoDb.CreateSession(ctx, hashToken(token), user.Id, expiresAt); err != nil {
		return "", nil, time.Time{}, fmt.Errorf("repoDb.CreateSession: %w", err)
	}

	if user.Role != model.RoleAdmin {
		if user.DagNames, err = s.repoDb.ListUserDags(ctx, user.Id); err != nil {
			return "", nil, time.Time{}, fmt.Errorf("repoDb.ListUserDags: %w", err)
		}
	}
	return token, user, expiresAt, nil
}

// Authenticate находит пользователя по токену сессии; ошибка — токен
// неизвестен или истёк.
func (s *Service) Authenticate(ctx context.Context, token string) (model.AuthInfo, error) {
	user, found, err := s.repoDb.GetSessionUser(ctx, hashToken(token))
	if err != nil {
		return model.AuthInfo{}, fmt.Errorf("repoDb.GetSessionUser: %w", err)
	}
	if !found {
		return model.AuthInfo{}, errs.NotAuthorized
	}
	return model.AuthInfo{UserId: user.Id, Username: user.Username, Role: user.Role}, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if err := s.repoDb.DeleteSession(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("repoDb.DeleteSession: %w", err)
	}
	return nil
}

// CleanupSessions удаляет истёкшие сессии (retention-цикл).
func (s *Service) CleanupSessions(ctx context.Context) (int64, error) {
	n, err := s.repoDb.DeleteExpiredSessions(ctx)
	if err != nil {
		return 0, fmt.Errorf("repoDb.DeleteExpiredSessions: %w", err)
	}
	return n, nil
}

// ── права на даг ────────────────────────────────────────

// CanManageDag — может ли вызывающий менять этот даг: admin (и внутренние
// вызовы без auth) — любой, обычный пользователь — только назначенный.
func (s *Service) CanManageDag(ctx context.Context, info model.AuthInfo, dagName string) (bool, error) {
	if info.IsAdmin() {
		return true, nil
	}
	if dagName == "" {
		return false, nil // глобальный скоуп — только admin
	}
	ok, err := s.repoDb.HasUserDag(ctx, info.UserId, dagName)
	if err != nil {
		return false, fmt.Errorf("repoDb.HasUserDag: %w", err)
	}
	return ok, nil
}

func validatePassword(password string) error {
	if len(password) < model.MinPasswordLen {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("пароль короче %d символов", model.MinPasswordLen)}
	}
	if len(password) > model.MaxPasswordLen {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("пароль длиннее %d байт (ограничение bcrypt)", model.MaxPasswordLen)}
	}
	return nil
}

func newUserId() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return "usr-" + hex.EncodeToString(buf)
}

// newToken — 32 случайных байта: сам токен уходит клиенту, в БД — sha256.
func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
