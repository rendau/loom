// Package dockerexecutor — executor попыток поверх docker CLI:
// 1 контейнер = 1 attempt на хосте control plane, без kubernetes. События
// жизненного цикла — поллингом `docker ps`/`docker inspect`: started
// публикуется сразу после успешного `docker run -d`, finished — когда
// контейнер завершился (exit code и OOMKilled — из inspect); обработанный
// контейнер удаляется. Дубли событий гасит идемпотентная финализация
// планировщика.
package dockerexecutor

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"
	"github.com/samber/lo"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
)

const (
	// labelManaged маркирует контейнеры loom (фильтр ps и ListAlive).
	labelManaged = "loom-executor"
	labelRun     = "loom.run"
	labelTask    = "loom.task"
	labelAttempt = "loom.attempt"

	eventsBuffer = 1024
	execTimeout  = 30 * time.Second
)

type Service struct {
	bin      string
	network  string
	pollTick time.Duration

	// peaks — зафиксированные пики памяти живых попыток: событие metrics
	// эмитится только при росте пика.
	peaksMu sync.Mutex
	peaks   map[runModel.AttemptRef]int64

	events    chan runModel.ExecEvent
	ctx       context.Context
	ctxCancel context.CancelFunc
	wg        sync.WaitGroup
}

func New(bin, network string, pollTick time.Duration) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	return &Service{
		bin:       bin,
		network:   network,
		pollTick:  pollTick,
		peaks:     map[runModel.AttemptRef]int64{},
		events:    make(chan runModel.ExecEvent, eventsBuffer),
		ctx:       ctx,
		ctxCancel: cancel,
	}
}

func (s *Service) Start() error {
	s.wg.Go(s.pollLoop)
	s.wg.Go(s.statsLoop)
	return nil
}

func (s *Service) Stop() {
	s.ctxCancel()
	s.wg.Wait()
}

func (s *Service) Events() <-chan runModel.ExecEvent {
	return s.events
}

// Launch запускает контейнер попытки. Идемпотентен: конфликт имени (контейнер
// этой попытки уже существует) — не ошибка.
func (s *Service) Launch(ctx context.Context, spec runModel.LaunchSpec) error {
	args := []string{"run", "-d",
		"--name", containerName(spec.Ref),
		"--label", labelManaged + "=1",
		"--label", labelRun + "=" + spec.Ref.RunId,
		"--label", labelTask + "=" + spec.Ref.Task,
		"--label", labelAttempt + "=" + strconv.Itoa(int(spec.Ref.Attempt)),
	}
	if s.network != "" {
		args = append(args, "--network", s.network)
	}
	args = append(args, resourceArgs(spec.Resources)...)
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, spec.Image, "run")

	if out, err := s.docker(ctx, args...); err != nil {
		if strings.Contains(out, "is already in use") {
			return nil // контейнер попытки уже создан (повторный Launch)
		}
		return fmt.Errorf("docker run: %w: %s", err, out)
	}

	// docker run -d возвращает после старта контейнера — событие started
	// публикуем сразу (аналог pod running у k8s-executor'а)
	s.emit(runModel.ExecEvent{Ref: spec.Ref, Type: runModel.ExecEventStarted})
	return nil
}

// Kill удаляет контейнер попытки.
func (s *Service) Kill(ctx context.Context, ref runModel.AttemptRef) error {
	if out, err := s.docker(ctx, "rm", "-f", containerName(ref)); err != nil {
		if strings.Contains(out, "No such container") {
			return nil
		}
		return fmt.Errorf("docker rm: %w: %s", err, out)
	}
	return nil
}

// ListAlive возвращает попытки, чьи контейнеры существуют (в любом статусе,
// включая завершённые до реапа) — источник правды зомби-детекта.
func (s *Service) ListAlive(ctx context.Context) ([]runModel.AttemptRef, error) {
	infos, err := s.listContainers(ctx, false)
	if err != nil {
		return nil, err
	}
	return lo.FilterMap(infos, func(c containerInfo, _ int) (runModel.AttemptRef, bool) {
		return c.ref()
	}), nil
}

// ── поллинг завершений ──────────────────────────────────

func (s *Service) pollLoop() {
	ticker := time.NewTicker(s.pollTick)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}

		if err := s.reapExited(s.ctx); err != nil && s.ctx.Err() == nil {
			slog.Error("docker executor poll", "error", err)
		}
	}
}

// reapExited публикует finished для завершившихся контейнеров и удаляет их;
// упавший rm повторится следующим проходом (дубль события схлопнется).
func (s *Service) reapExited(ctx context.Context) error {
	infos, err := s.listContainers(ctx, true)
	if err != nil {
		return err
	}

	for _, c := range infos {
		ref, ok := c.ref()
		if !ok {
			continue
		}

		exit := runModel.ExitInfo{
			Success:  c.State.ExitCode == 0,
			ExitCode: new(int32(c.State.ExitCode)),
		}
		switch {
		case c.State.OOMKilled:
			exit.Reason = "OOMKilled"
		case !exit.Success:
			exit.Reason = "Error"
		}
		s.emit(runModel.ExecEvent{Ref: ref, Type: runModel.ExecEventFinished, Exit: &exit})

		if out, rmErr := s.docker(ctx, "rm", c.Id); rmErr != nil {
			slog.Warn("docker executor rm", "container", c.Id, "error", rmErr, "output", out)
		}
	}

	return nil
}

// ── семплинг потребления памяти ─────────────────────────

// statsLoop периодически снимает `docker stats` живых контейнеров и эмитит
// metrics-событие при росте пика памяти попытки. Пик семплированный —
// короткие спайки между тиками теряются.
func (s *Service) statsLoop() {
	ticker := time.NewTicker(s.pollTick)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}

		if err := s.sampleStats(s.ctx); err != nil && s.ctx.Err() == nil {
			slog.Warn("docker executor stats", "error", err)
		}
	}
}

func (s *Service) sampleStats(ctx context.Context) error {
	infos, err := s.listContainers(ctx, false)
	if err != nil {
		return err
	}

	running := lo.Filter(infos, func(c containerInfo, _ int) bool { return c.State.Running })
	alive := map[runModel.AttemptRef]bool{}
	// docker stats печатает короткий id — маппим префиксом
	byShortId := map[string]runModel.AttemptRef{}
	for _, c := range running {
		ref, ok := c.ref()
		if !ok {
			continue
		}
		alive[ref] = true
		byShortId[c.Id[:min(12, len(c.Id))]] = ref
	}
	s.prunePeaks(alive)
	if len(byShortId) == 0 {
		return nil
	}

	args := append([]string{"stats", "--no-stream", "--format", "{{json .}}"}, lo.Keys(byShortId)...)
	out, err := s.docker(ctx, args...)
	if err != nil {
		// контейнер мог завершиться между ps и stats
		if strings.Contains(out, "No such container") {
			return nil
		}
		return fmt.Errorf("docker stats: %w: %s", err, out)
	}

	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Container string `json:"Container"`
			MemUsage  string `json:"MemUsage"`
		}
		if err = json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		ref, ok := byShortId[row.Container]
		if !ok {
			continue
		}
		if usage, uOk := parseMemUsage(row.MemUsage); uOk {
			s.notePeak(ref, usage)
		}
	}
	return nil
}

// notePeak поднимает пик попытки и эмитит metrics-событие только при росте.
func (s *Service) notePeak(ref runModel.AttemptRef, usage int64) {
	s.peaksMu.Lock()
	grew := usage > s.peaks[ref]
	if grew {
		s.peaks[ref] = usage
	}
	s.peaksMu.Unlock()

	if grew {
		s.emit(runModel.ExecEvent{Ref: ref, Type: runModel.ExecEventMetrics, PeakMemoryBytes: &usage})
	}
}

// prunePeaks выбрасывает пики исчезнувших попыток (контейнер завершён).
func (s *Service) prunePeaks(alive map[runModel.AttemptRef]bool) {
	s.peaksMu.Lock()
	defer s.peaksMu.Unlock()
	for ref := range s.peaks {
		if !alive[ref] {
			delete(s.peaks, ref)
		}
	}
}

// parseMemUsage разбирает «88.4MiB / 3.8GiB» из docker stats — берём usage
// до слэша; юниты go-units (двоичные KiB/MiB/... и десятичные kB/MB/...).
func parseMemUsage(memUsage string) (int64, bool) {
	usagePart, _, _ := strings.Cut(memUsage, "/")
	usagePart = strings.TrimSpace(usagePart)

	i := 0
	for i < len(usagePart) && (usagePart[i] >= '0' && usagePart[i] <= '9' || usagePart[i] == '.') {
		i++
	}
	value, err := strconv.ParseFloat(usagePart[:i], 64)
	if err != nil {
		return 0, false
	}

	mult, ok := map[string]float64{
		"B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
		"kB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
	}[strings.TrimSpace(usagePart[i:])]
	if !ok {
		return 0, false
	}
	return int64(value * mult), true
}

// ── docker CLI ──────────────────────────────────────────

type containerInfo struct {
	Id    string `json:"Id"`
	State struct {
		Running   bool  `json:"Running"`
		ExitCode  int32 `json:"ExitCode"`
		OOMKilled bool  `json:"OOMKilled"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

func (c containerInfo) ref() (runModel.AttemptRef, bool) {
	attempt, err := strconv.Atoi(c.Config.Labels[labelAttempt])
	ref := runModel.AttemptRef{
		RunId:   c.Config.Labels[labelRun],
		Task:    c.Config.Labels[labelTask],
		Attempt: int32(attempt),
	}
	if err != nil || ref.RunId == "" || ref.Task == "" {
		return runModel.AttemptRef{}, false
	}
	return ref, true
}

// listContainers возвращает loom-контейнеры (onlyExited — только
// завершившиеся) с exit-информацией и лейблами.
func (s *Service) listContainers(ctx context.Context, onlyExited bool) ([]containerInfo, error) {
	args := []string{"ps", "-a", "-q", "--filter", "label=" + labelManaged + "=1"}
	if onlyExited {
		args = append(args, "--filter", "status=exited")
	}
	out, err := s.docker(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w: %s", err, out)
	}

	ids := strings.Fields(out)
	if len(ids) == 0 {
		return nil, nil
	}

	out, err = s.docker(ctx, append([]string{"inspect"}, ids...)...)
	if err != nil {
		// контейнер мог исчезнуть между ps и inspect — не ошибка прохода
		if strings.Contains(out, "No such object") {
			return nil, nil
		}
		return nil, fmt.Errorf("docker inspect: %w: %s", err, out)
	}

	var infos []containerInfo
	if err = json.Unmarshal([]byte(out), &infos); err != nil {
		return nil, fmt.Errorf("parse docker inspect: %w", err)
	}
	return infos, nil
}

func (s *Service) docker(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, s.bin, args...).CombinedOutput()
	return string(out), err
}

func (s *Service) emit(ev runModel.ExecEvent) {
	select {
	case s.events <- ev:
	default:
		slog.Error("docker executor: events buffer overflow, event dropped",
			"run_id", ev.Ref.RunId, "task", ev.Ref.Task, "attempt", ev.Ref.Attempt)
	}
}

func containerName(ref runModel.AttemptRef) string {
	return fmt.Sprintf("loom-%s-%s-%d", ref.RunId, ref.Task, ref.Attempt)
}

// resourceArgs маппит ресурсы манифеста на флаги docker: у docker нет
// requests, применяются только limits (quantities валидированы при
// регистрации дага).
func resourceArgs(r *dagModel.TaskResources) []string {
	if r == nil {
		return nil
	}

	var args []string
	if r.CPULimit != "" {
		if q, err := k8sResource.ParseQuantity(r.CPULimit); err == nil {
			args = append(args, "--cpus", strconv.FormatFloat(q.AsApproximateFloat64(), 'f', -1, 64))
		}
	}
	if r.MemoryLimit != "" {
		if q, err := k8sResource.ParseQuantity(r.MemoryLimit); err == nil {
			args = append(args, "--memory", strconv.FormatInt(q.Value(), 10)+"b")
		}
	}
	return args
}
