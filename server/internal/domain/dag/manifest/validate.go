package manifest

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/samber/lo"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/errs"
)

// maxTaskRetries — верхняя граница retries в манифесте: защита от опечаток
// вида retries=100000, превращающих упавший таск в вечный цикл попыток.
const maxTaskRetries = 100

// nameRe — допустимые имена проектов, дагов и тасков; согласовано с
// ограничениями artifact-сервера (streamstore ref) и лейблов kubernetes.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

// envNameRe — допустимые имена env-переменных для инъекции секретов.
var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// ValidName — проверка имени по общему правилу (даг, проект, таск, пул).
func ValidName(v string) bool {
	return nameRe.MatchString(v)
}

// ValidateCatalog проверяет каталог образа целиком: версию SDK и то, что
// в образе остался хотя бы один валидный даг. Манифесты отдельных дагов
// проверяет Validate — их ошибки не отменяют регистрацию остальных.
func ValidateCatalog(c *dagModel.Catalog) error {
	fail := func(desc string) error {
		return errs.ErrFull{Err: errs.InvalidManifest, Desc: desc}
	}

	if c == nil {
		return fail("каталог образа пуст")
	}
	if c.SdkVersion == "" {
		return fail("отсутствует sdk_version")
	}
	if len(c.Dags) == 0 {
		return fail("в образе нет дагов")
	}

	names := map[string]bool{}
	for _, d := range c.Dags {
		if !nameRe.MatchString(d.Name) {
			return fail(fmt.Sprintf("недопустимое имя дага %q", d.Name))
		}
		if names[d.Name] {
			return fail(fmt.Sprintf("дубль дага %q в образе", d.Name))
		}
		names[d.Name] = true
	}

	return nil
}

// Validate проверяет манифест дага: имена, целостность рёбер и
// ацикличность. Манифест приходит из чужого образа — не доверяем ему,
// даже если SDK валидировал даг на своей стороне.
func Validate(m *dagModel.Manifest) error {
	fail := func(desc string) error {
		return errs.ErrFull{Err: errs.InvalidManifest, Desc: desc}
	}

	if m == nil {
		return fail("манифест пуст")
	}
	if !nameRe.MatchString(m.Name) {
		return fail(fmt.Sprintf("недопустимое имя дага %q", m.Name))
	}
	if len(m.Tasks) == 0 {
		return fail("в даге нет тасков")
	}
	if m.MaxActiveRuns < 0 {
		return fail("отрицательный max_active_runs")
	}

	tasks := map[string]dagModel.Task{}
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
		// env-инъекции секретов и переменных делят одно пространство имён —
		// seenEnv общий
		seenEnv := map[string]bool{}
		checkEnv := func(env string) error {
			if !envNameRe.MatchString(env) {
				return fail(fmt.Sprintf("таск %q: недопустимое env-имя %q", t.Name, env))
			}
			if strings.HasPrefix(env, "LOOM_") {
				return fail(fmt.Sprintf("таск %q: env %q конфликтует с контрактом LOOM_*", t.Name, env))
			}
			if seenEnv[env] {
				return fail(fmt.Sprintf("таск %q: дубль env %q", t.Name, env))
			}
			seenEnv[env] = true
			return nil
		}
		for _, sec := range t.Secrets {
			if err := checkEnv(sec.Env); err != nil {
				return err
			}
			if !nameRe.MatchString(sec.Secret) {
				return fail(fmt.Sprintf("таск %q: недопустимое имя секрета %q", t.Name, sec.Secret))
			}
		}
		for _, v := range t.Variables {
			if err := checkEnv(v.Env); err != nil {
				return err
			}
			if !nameRe.MatchString(v.Variable) {
				return fail(fmt.Sprintf("таск %q: недопустимое имя переменной %q", t.Name, v.Variable))
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
	inDegree := lo.MapEntries(tasks, func(name string, t dagModel.Task) (string, int) {
		return name, len(t.DependsOn)
	})
	queue := lo.Keys(lo.PickBy(inDegree, func(_ string, deg int) bool { return deg == 0 }))
	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, t := range m.Tasks {
			if lo.ContainsBy(t.DependsOn, func(d dagModel.Dep) bool { return d.Task == cur }) {
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
func validateResources(task string, r *dagModel.TaskResources) error {
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
