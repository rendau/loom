// Package service — секреты для env-инъекции в поды тасков.
// Скоупы трёхуровневые (глобальный → проект → даг); более узкий
// перекрывает более широкий при резолве в Launch. Значение наружу отдаёт
// только GetValue — ролевые ограничения на нём живут в usecase.
package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"regexp"

	"github.com/samber/lo"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	"github.com/rendau/loom/server/internal/domain/secret/model"
	"github.com/rendau/loom/server/internal/errs"
)

// nameRe — допустимые имена секретов; согласовано с манифестами SDK.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

type Service struct {
	repoDb RepoDbI
	aead   cipher.AEAD // nil — шифрование выключено (пустой SECRET_KEY)
}

// New создаёт сервис секретов. Непустой keyPhrase включает шифрование
// значений AES-256-GCM; ключ выводится из фразы SHA-256. Пустой — значения
// хранятся открытым текстом (dev-режим).
func New(repoDb RepoDbI, keyPhrase string) (*Service, error) {
	s := &Service{repoDb: repoDb}

	if keyPhrase != "" {
		key := sha256.Sum256([]byte(keyPhrase))
		block, err := aes.NewCipher(key[:])
		if err != nil {
			return nil, fmt.Errorf("aes cipher: %w", err)
		}
		if s.aead, err = cipher.NewGCM(block); err != nil {
			return nil, fmt.Errorf("gcm: %w", err)
		}
	}

	return s, nil
}

// List — метаданные секретов; scope nil — все скоупы, иначе только
// указанный.
func (s *Service) List(ctx context.Context, scope *commonModel.Scope) ([]*model.Meta, error) {
	items, err := s.repoDb.ListMeta(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListMeta: %w", err)
	}
	return items, nil
}

// Set создаёт секрет или перезаписывает значение существующего.
func (s *Service) Set(ctx context.Context, scope commonModel.Scope, name string, value []byte) error {
	if !scope.Valid() {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: "некорректный скоуп"}
	}
	if !nameRe.MatchString(name) {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("недопустимое имя секрета %q", name)}
	}
	if len(value) == 0 {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: "пустое значение секрета"}
	}
	if len(value) > model.MaxValueSize {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("значение больше лимита %d байт", model.MaxValueSize)}
	}

	stored, err := s.encrypt(value)
	if err != nil {
		return err
	}
	if err = s.repoDb.Set(ctx, scope, name, stored); err != nil {
		return fmt.Errorf("repoDb.Set: %w", err)
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, scope commonModel.Scope, name string) error {
	found, err := s.repoDb.Delete(ctx, scope, name)
	if err != nil {
		return fmt.Errorf("repoDb.Delete: %w", err)
	}
	if !found {
		return errs.SecretNotFound
	}
	return nil
}

// Move переносит запись в другой скоуп, сохраняя значение: адрес меняется
// одним UPDATE, поэтому значение секрета не нужно вводить заново (у
// секрета клиент его и не знает).
func (s *Service) Move(ctx context.Context, from, to commonModel.Scope, name string) error {
	if !from.Valid() || !to.Valid() {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: "некорректный скоуп"}
	}
	if from == to {
		return nil // переносить некуда — не ошибка
	}

	occupied, err := s.repoDb.Exists(ctx, to, name)
	if err != nil {
		return fmt.Errorf("repoDb.Exists: %w", err)
	}
	if occupied {
		return errs.SecretExists
	}

	found, err := s.repoDb.Move(ctx, from, to, name)
	if err != nil {
		return fmt.Errorf("repoDb.Move: %w", err)
	}
	if !found {
		return errs.SecretNotFound
	}
	return nil
}

// GetValue — расшифрованное значение секрета точного скоупа (просмотр из
// админки; ролевые ограничения — в usecase).
func (s *Service) GetValue(ctx context.Context, scope commonModel.Scope, name string) ([]byte, error) {
	blob, found, err := s.repoDb.GetValue(ctx, scope, name)
	if err != nil {
		return nil, fmt.Errorf("repoDb.GetValue: %w", err)
	}
	if !found {
		return nil, errs.SecretNotFound
	}

	value, err := s.decrypt(blob)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret %q: %w", name, err)
	}
	return value, nil
}

// ResolveValues возвращает расшифрованные значения секретов для инъекции в
// env попытки дага (даг перекрывает проект, проект — глобальный); любой
// отсутствующий секрет — ошибка (попытка не должна стартовать с пустой
// переменной).
func (s *Service) ResolveValues(ctx context.Context, scope commonModel.Scope, names []string) (map[string]model.Resolved, error) {
	stored, err := s.repoDb.GetValues(ctx, scope, names)
	if err != nil {
		return nil, fmt.Errorf("repoDb.GetValues: %w", err)
	}

	missing := lo.Filter(names, func(n string, _ int) bool { _, ok := stored[n]; return !ok })
	if len(missing) > 0 {
		return nil, errs.ErrFull{Err: errs.SecretNotFound, Desc: fmt.Sprintf("секреты не найдены: %v", missing)}
	}

	result := make(map[string]model.Resolved, len(stored))
	for name, blob := range stored {
		value, err := s.decrypt(blob.Value)
		if err != nil {
			return nil, fmt.Errorf("decrypt secret %q: %w", name, err)
		}
		result[name] = model.Resolved{Value: value, Scope: blob.Scope}
	}
	return result, nil
}

// encrypt шифрует значение AES-256-GCM (nonce в префиксе); без ключа —
// как есть.
func (s *Service) encrypt(value []byte) ([]byte, error) {
	if s.aead == nil {
		return value, nil
	}

	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, value, nil), nil
}

func (s *Service) decrypt(blob []byte) ([]byte, error) {
	if s.aead == nil {
		return blob, nil
	}

	if len(blob) < s.aead.NonceSize() {
		return nil, fmt.Errorf("value is too short")
	}
	return s.aead.Open(nil, blob[:s.aead.NonceSize()], blob[s.aead.NonceSize():], nil)
}
