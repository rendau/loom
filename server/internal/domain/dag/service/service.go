package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/samber/lo"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"

	"github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/errs"
	"github.com/rendau/loom/server/internal/util"
)

// maxTaskRetries — верхняя граница retries в манифесте: защита от опечаток
// вида retries=100000, превращающих упавший таск в вечный цикл попыток.
const maxTaskRetries = 100

// nameRe — допустимые имена дагов и тасков; согласовано с ограничениями
// artifact-сервера (streamstore ref) и лейблов kubernetes.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

// envNameRe — допустимые имена env-переменных для инъекции секретов.
var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

type Service struct {
	repoDb RepoDbI
}

func New(repoDb RepoDbI) *Service {
	return &Service{repoDb: repoDb}
}

func (s *Service) List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error) {
	items, tCount, err := s.repoDb.List(ctx, pars)
	if err != nil {
		return nil, 0, fmt.Errorf("repoDb.List: %w", err)
	}
	return items, tCount, nil
}

func (s *Service) Get(ctx context.Context, name string, errNE bool) (*model.Main, bool, error) {
	result, found, err := s.repoDb.Get(ctx, name)
	if err != nil {
		return nil, false, fmt.Errorf("repoDb.Get: %w", err)
	}
	if !found {
		if errNE {
			return nil, false, errs.DagNotFound
		}
		return nil, false, nil
	}
	return result, found, nil
}

// Register сохраняет даг по манифесту, полученному из образа: валидирует
// манифест и создаёт/обновляет запись (перерегистрация = новая версия
// образа). Paused при перерегистрации не трогается; autoUpdate
// обновляется только при явном значении — nil сохраняет текущее (в
// частности, авто-перерегистрация dagsync флаг не трогает). next_run_at
// пересчитывается от текущего момента, кроме catchup-дага с тем же
// расписанием — его тики наверстает cron-цикл.
func (s *Service) Register(ctx context.Context, image, imageDigest string, rawManifest []byte, m *model.Manifest, autoUpdate *bool) (*model.Main, error) {
	if err := ValidateManifest(m); err != nil {
		return nil, err
	}

	old, oldFound, err := s.Get(ctx, m.Name, false)
	if err != nil {
		return nil, err
	}

	err = s.repoDb.UpdateOrCreate(ctx, m.Name, &model.Edit{
		Image:       &image,
		ImageDigest: &imageDigest,
		Schedule:    &m.Schedule,
		AutoUpdate:  autoUpdate,
		Manifest:    &rawManifest,
		ModifiedAt:  new(time.Now()),
	})
	if err != nil {
		return nil, fmt.Errorf("repoDb.UpdateOrCreate: %w", err)
	}

	keepNextRun := m.Catchup && m.Schedule != "" &&
		oldFound && old.Schedule == m.Schedule && !old.NextRunAt.IsZero()
	if !keepNextRun {
		if err = s.resetNextRun(ctx, m.Name, m.Schedule); err != nil {
			return nil, err
		}
	}

	result, _, err := s.Get(ctx, m.Name, true)
	return result, err
}

func (s *Service) SetPaused(ctx context.Context, name string, paused bool) error {
	dag, _, err := s.Get(ctx, name, true)
	if err != nil {
		return err
	}

	err = s.repoDb.Update(ctx, name, &model.Edit{
		Paused:     &paused,
		ModifiedAt: new(time.Now()),
	})
	if err != nil {
		return fmt.Errorf("repoDb.Update: %w", err)
	}

	// снятие с паузы: расписание продолжается со следующего срабатывания,
	// пропущенные за время паузы запуски не навёрстываются — кроме
	// catchup-дага: его next_run_at не трогаем, тики наверстает cron-цикл
	if !paused && !(dag.Catchup && !dag.NextRunAt.IsZero()) {
		if err = s.resetNextRun(ctx, name, dag.Schedule); err != nil {
			return err
		}
	}
	return nil
}

// SetAutoUpdate включает/выключает poll-синк новой версии образа дага
//.
func (s *Service) SetAutoUpdate(ctx context.Context, name string, autoUpdate bool) error {
	if _, _, err := s.Get(ctx, name, true); err != nil {
		return err
	}

	err := s.repoDb.Update(ctx, name, &model.Edit{
		AutoUpdate: &autoUpdate,
		ModifiedAt: new(time.Now()),
	})
	if err != nil {
		return fmt.Errorf("repoDb.Update: %w", err)
	}
	return nil
}

// ListAutoUpdate возвращает даги с включённым авто-обновлением — кандидатов
// dagsync-цикла.
func (s *Service) ListAutoUpdate(ctx context.Context) ([]*model.Main, error) {
	items, _, err := s.repoDb.List(ctx, &model.ListReq{AutoUpdate: new(true)})
	if err != nil {
		return nil, fmt.Errorf("repoDb.List: %w", err)
	}
	return items, nil
}

// resetNextRun пересчитывает next_run_at дага от текущего момента; пустое
// расписание сбрасывает его в null.
func (s *Service) resetNextRun(ctx context.Context, name, schedule string) error {
	var next *time.Time
	if schedule != "" {
		t, err := util.CronNext(schedule, time.Now())
		if err != nil {
			return errs.ErrFull{Err: errs.InvalidManifest, Desc: err.Error()}
		}
		next = &t
	}

	if err := s.repoDb.SetNextRun(ctx, name, next); err != nil {
		return fmt.Errorf("repoDb.SetNextRun: %w", err)
	}
	return nil
}

// ── операции cron-планировщика ──────────────────────────

// ListDueSchedules возвращает даги с расписанием, чей next_run_at наступил
// или не инициализирован (null при непустом schedule).
func (s *Service) ListDueSchedules(ctx context.Context) ([]*model.Main, error) {
	names, err := s.repoDb.ListDueNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListDueNames: %w", err)
	}

	result := make([]*model.Main, 0, len(names))
	for _, name := range names {
		dag, found, err := s.Get(ctx, name, false)
		if err != nil {
			return nil, err
		}
		if found {
			result = append(result, dag)
		}
	}
	return result, nil
}

// AdvanceNextRun двигает next_run_at дага вперёд compare-and-swap'ом:
// false — значение уже сдвинул кто-то другой (гонка инстансов), триггерить
// этот тик не нужно.
func (s *Service) AdvanceNextRun(ctx context.Context, name string, from, to time.Time) (bool, error) {
	ok, err := s.repoDb.AdvanceNextRun(ctx, name, from, to)
	if err != nil {
		return false, fmt.Errorf("repoDb.AdvanceNextRun: %w", err)
	}
	return ok, nil
}

func (s *Service) Delete(ctx context.Context, name string) error {
	if _, _, err := s.Get(ctx, name, true); err != nil {
		return err
	}

	if err := s.repoDb.Delete(ctx, name); err != nil {
		return fmt.Errorf("repoDb.Delete: %w", err)
	}
	return nil
}

// ValidateManifest проверяет манифест дага: имена, целостность рёбер и
// ацикличность. Манифест приходит из чужого образа — не доверяем ему,
// даже если SDK валидировал даг на своей стороне.
func ValidateManifest(m *model.Manifest) error {
	fail := func(desc string) error {
		return errs.ErrFull{Err: errs.InvalidManifest, Desc: desc}
	}

	if m == nil {
		return fail("манифест пуст")
	}
	if m.SdkVersion == "" {
		return fail("отсутствует sdk_version")
	}
	if !nameRe.MatchString(m.Name) {
		return fail(fmt.Sprintf("недопустимое имя дага %q", m.Name))
	}
	if m.Schedule != "" {
		if _, err := util.CronNext(m.Schedule, time.Now()); err != nil {
			return fail(fmt.Sprintf("некорректное расписание %q: %v", m.Schedule, err))
		}
	}
	if len(m.Tasks) == 0 {
		return fail("в даге нет тасков")
	}
	if m.MaxActiveRuns < 0 {
		return fail("отрицательный max_active_runs")
	}

	tasks := map[string]model.Task{}
	for _, t := range m.Tasks {
		if !nameRe.MatchString(t.Name) {
			return fail(fmt.Sprintf("недопустимое имя таска %q", t.Name))
		}
		if _, ok := tasks[t.Name]; ok {
			return fail(fmt.Sprintf("дубль таска %q", t.Name))
		}
		if t.Retries < 0 || t.Retries > maxTaskRetries {
			return fail(fmt.Sprintf("таск %q: retries %d вне диапазона [0, %d]", t.Name, t.Retries, maxTaskRetries))
		}
		if t.RetryDelaySec < 0 {
			return fail(fmt.Sprintf("таск %q: отрицательный retry_delay_sec", t.Name))
		}
		if t.TimeoutSec < 0 {
			return fail(fmt.Sprintf("таск %q: отрицательный timeout_sec", t.Name))
		}
		if err := validateResources(t.Name, t.Resources); err != nil {
			return err
		}
		if t.Pool != "" && !nameRe.MatchString(t.Pool) {
			return fail(fmt.Sprintf("таск %q: недопустимое имя пула %q", t.Name, t.Pool))
		}
		seenEnv := map[string]bool{}
		for _, sec := range t.Secrets {
			if !envNameRe.MatchString(sec.Env) {
				return fail(fmt.Sprintf("таск %q: недопустимое env-имя секрета %q", t.Name, sec.Env))
			}
			if strings.HasPrefix(sec.Env, "LOOM_") {
				return fail(fmt.Sprintf("таск %q: env %q конфликтует с контрактом LOOM_*", t.Name, sec.Env))
			}
			if seenEnv[sec.Env] {
				return fail(fmt.Sprintf("таск %q: дубль env %q", t.Name, sec.Env))
			}
			seenEnv[sec.Env] = true
			if !nameRe.MatchString(sec.Secret) {
				return fail(fmt.Sprintf("таск %q: недопустимое имя секрета %q", t.Name, sec.Secret))
			}
		}
		tasks[t.Name] = t
	}

	for _, t := range m.Tasks {
		seen := map[string]bool{}
		for _, dep := range t.DependsOn {
			if dep.Task == t.Name {
				return fail(fmt.Sprintf("таск %q зависит от самого себя", t.Name))
			}
			if _, ok := tasks[dep.Task]; !ok {
				return fail(fmt.Sprintf("таск %q зависит от неизвестного таска %q", t.Name, dep.Task))
			}
			if seen[dep.Task] {
				return fail(fmt.Sprintf("таск %q: дубль зависимости %q", t.Name, dep.Task))
			}
			seen[dep.Task] = true
		}
	}

	// ацикличность — алгоритм Кана
	inDegree := lo.MapEntries(tasks, func(name string, t model.Task) (string, int) {
		return name, len(t.DependsOn)
	})
	queue := lo.Keys(lo.PickBy(inDegree, func(_ string, deg int) bool { return deg == 0 }))
	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, t := range m.Tasks {
			if lo.ContainsBy(t.DependsOn, func(d model.Dep) bool { return d.Task == cur }) {
				inDegree[t.Name]--
				if inDegree[t.Name] == 0 {
					queue = append(queue, t.Name)
				}
			}
		}
	}
	if visited != len(tasks) {
		return fail("граф тасков содержит цикл")
	}

	return nil
}

// validateResources проверяет kubernetes quantities ресурсов таска —
// некорректное значение обнаружится при регистрации, а не при запуске пода.
func validateResources(task string, r *model.TaskResources) error {
	if r == nil {
		return nil
	}

	for _, q := range []struct{ name, value string }{
		{"cpu_request", r.CPURequest},
		{"cpu_limit", r.CPULimit},
		{"memory_request", r.MemoryRequest},
		{"memory_limit", r.MemoryLimit},
	} {
		if q.value == "" {
			continue
		}
		if _, err := k8sResource.ParseQuantity(q.value); err != nil {
			return errs.ErrFull{Err: errs.InvalidManifest,
				Desc: fmt.Sprintf("таск %q: некорректное значение %s=%q", task, q.name, q.value)}
		}
	}
	return nil
}
