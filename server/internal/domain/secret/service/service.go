// Package service — секреты для env-инъекции в поды тасков.
// API write-only: значение можно записать и удалить, прочитать наружу
// нельзя — расшифровывает только сам control plane при Launch попытки.
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

func (s *Service) List(ctx context.Context) ([]*model.Meta, error) {
	items, err := s.repoDb.ListMeta(ctx)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListMeta: %w", err)
	}
	return items, nil
}

// Set создаёт секрет или перезаписывает значение существующего.
func (s *Service) Set(ctx context.Context, name string, value []byte) error {
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
	if err = s.repoDb.Set(ctx, name, stored); err != nil {
		return fmt.Errorf("repoDb.Set: %w", err)
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, name string) error {
	found, err := s.repoDb.Delete(ctx, name)
	if err != nil {
		return fmt.Errorf("repoDb.Delete: %w", err)
	}
	if !found {
		return errs.SecretNotFound
	}
	return nil
}

// ResolveValues возвращает расшифрованные значения секретов для инъекции в
// env попытки; любой отсутствующий секрет — ошибка (попытка не должна
// стартовать с пустой переменной).
func (s *Service) ResolveValues(ctx context.Context, names []string) (map[string][]byte, error) {
	stored, err := s.repoDb.GetValues(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("repoDb.GetValues: %w", err)
	}

	missing := lo.Filter(names, func(n string, _ int) bool { _, ok := stored[n]; return !ok })
	if len(missing) > 0 {
		return nil, errs.ErrFull{Err: errs.SecretNotFound, Desc: fmt.Sprintf("секреты не найдены: %v", missing)}
	}

	result := make(map[string][]byte, len(stored))
	for name, blob := range stored {
		value, err := s.decrypt(blob)
		if err != nil {
			return nil, fmt.Errorf("decrypt secret %q: %w", name, err)
		}
		result[name] = value
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
