// Package storage — статистика хранилища artifact-сервера: занято файлами
// каталогов данных/логов + ёмкость их файловых систем. «Занято» — полный
// обход каталога (du), поэтому результат кэшируется: обзор админки поллит
// статистику регулярно, а пересчитывать диск на каждый запрос незачем.
package storage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const cacheTTL = 30 * time.Second

type DirStats struct {
	UsedBytes  int64
	TotalBytes int64
	FreeBytes  int64
}

type Stats struct {
	Data DirStats
	Logs DirStats
}

type Service struct {
	dataDir string
	logDir  string

	mu        sync.Mutex
	cached    Stats
	fetchedAt time.Time
}

func New(dataDir, logDir string) *Service {
	return &Service{dataDir: dataDir, logDir: logDir}
}

func (s *Service) GetStats() (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.fetchedAt.IsZero() && time.Since(s.fetchedAt) < cacheTTL {
		return s.cached, nil
	}

	data, err := dirStats(s.dataDir)
	if err != nil {
		return Stats{}, fmt.Errorf("data dir stats: %w", err)
	}
	logs, err := dirStats(s.logDir)
	if err != nil {
		return Stats{}, fmt.Errorf("log dir stats: %w", err)
	}

	s.cached = Stats{Data: data, Logs: logs}
	s.fetchedAt = time.Now()

	return s.cached, nil
}

func dirStats(dir string) (DirStats, error) {
	var used int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			// файл/каталог исчез во время обхода (retention) — не ошибка
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			if info, infoErr := d.Info(); infoErr == nil {
				used += info.Size()
			}
		}
		return nil
	})
	if err != nil {
		return DirStats{}, fmt.Errorf("walk %s: %w", dir, err)
	}

	var st syscall.Statfs_t
	if err = syscall.Statfs(dir, &st); err != nil {
		return DirStats{}, fmt.Errorf("statfs %s: %w", dir, err)
	}

	return DirStats{
		UsedBytes:  used,
		TotalBytes: int64(st.Blocks) * int64(st.Bsize), //nolint:unconvert // Bsize: int64 на linux, uint32 на darwin
		FreeBytes:  int64(st.Bavail) * int64(st.Bsize), //nolint:unconvert
	}, nil
}
