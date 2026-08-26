package model

import (
	"strings"
	"time"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
)

// Ref — составной идентификатор дага: проект (docker-образ) и имя
// инстанса внутри него. От одного шаблона образа можно завести несколько
// дагов, поэтому одного имени мало.
type Ref struct {
	Project string
	Name    string
}

func NewRef(project, name string) Ref {
	return Ref{Project: project, Name: name}
}

// String — человекочитаемая форма «проект/даг»: логи, k8s-имена, снапшот
// скоупа в run_env. Обратный разбор — ParseRef.
func (r Ref) String() string {
	return r.Project + "/" + r.Name
}

// Empty — ссылка неполна (пустой проект или имя): ключом быть не может.
func (r Ref) Empty() bool {
	return r.Project == "" || r.Name == ""
}

// Scope — скоуп значений этого дага (переменные, секреты, настройки).
func (r Ref) Scope() commonModel.Scope {
	return commonModel.DagScope(r.Project, r.Name)
}

// ParseRef разбирает форму «проект/даг»; ok=false — строка не ссылка.
func ParseRef(v string) (Ref, bool) {
	project, name, found := strings.Cut(v, "/")
	if !found || project == "" || name == "" {
		return Ref{}, false
	}
	return Ref{Project: project, Name: name}, true
}

// Main — даг-инстанс: шаблон образа плюс собственные настройки. Поля
// Image/ImageDigest/AutoUpdate приходят из проекта, а SdkVersion/Tasks/
// Manifest/MaxActiveRuns — из шаблона (join при чтении).
// Schedule/Catchup/Paused/Pool задаются через админку, не манифестом.
type Main struct {
	Ref
	// Template — имя дага в образе, от которого заведён инстанс.
	Template string
	Schedule string
	Catchup  bool // наверстывать пропущенные тики расписания
	Paused   bool
	// Pool — пул слотов дага (задаётся только из админки, в манифесте его
	// нет): действует на все таски дага. Пусто — общий пул default.
	Pool       string
	NextRunAt  time.Time // zero — без расписания / не инициализировано
	CreatedAt  time.Time
	ModifiedAt time.Time // zero — не изменялся

	// ── из проекта ──
	Image       string
	ImageDigest string
	// AutoUpdate — poll-синк новой версии образа: свойство проекта, общее
	// для всех его дагов.
	AutoUpdate bool

	// ── из шаблона ──
	SdkVersion string
	// Лимит одновременно выполняющихся ранов дага; 0 — без лимита
	//.
	MaxActiveRuns int
	Tasks         []Task
	Manifest      []byte
	// TemplateOrphaned — шаблон пропал из образа при последней регистрации:
	// даг живёт на последнем известном манифесте, админка предупреждает.
	TemplateOrphaned bool

	// Последние раны (новые первыми, до 5) — не колонка: дособирается
	// usecase'ом для списка/карточки дага.
	LastRuns []LastRun
}

// LastRun — статус одного из последних ранов дага (статус-стрип админки);
// заполняется usecase'ом, в таблице dag не хранится.
type LastRun struct {
	RunId  string
	Status string
}

type Task struct {
	Name      string
	DependsOn []Dep
	// Политика ретраев и таймаут (гранулярность манифеста — секунды).
	Retries       int
	RetryDelaySec int
	TimeoutSec    int
	Resources     *TaskResources
	Priority      int // больше — раньше из очереди
	Secrets       []SecretRef
	Variables     []VariableRef
}

// TaskResourcesEntry — оверрайд ресурсов таска из админки: значения
// манифеста (кода дага) — рекомендуемые, непустое поле оверрайда
// приоритетнее при launch. Хранится в таблице task_resources.
type TaskResourcesEntry struct {
	Task       string
	Res        TaskResources
	ModifiedAt time.Time
}

// SecretRef — инъекция секрета control plane в env контейнера таска.
// Description — необязательная подсказка из кода дага для админки.
type SecretRef struct {
	Env         string
	Secret      string
	Description string
}

// VariableRef — инъекция переменной control plane в env контейнера таска
// (значение, в отличие от секрета, видно в админке).
type VariableRef struct {
	Env         string
	Variable    string
	Description string
}

// TaskResources — ресурсы контейнера попытки (kubernetes quantities).
type TaskResources struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
}

type Dep struct {
	Task     string
	Streamed bool
}

// Catalog — распарсенный вывод `describe`: все даги образа. Версия SDK
// общая на образ, поэтому живёт здесь, а не в манифесте дага.
type Catalog struct {
	SdkVersion string
	Dags       []CatalogDag
}

// CatalogDag — даг образа: либо манифест, либо ошибка валидации графа на
// стороне SDK (регистрацию остальных дагов она не отменяет).
type CatalogDag struct {
	Name     string
	Manifest *Manifest
	Raw      []byte // манифест как есть: снапшотится в шаблон и в ран
	Error    string
}

// Manifest — распарсенный манифест дага (одна запись каталога).
type Manifest struct {
	Name          string
	MaxActiveRuns int
	Tasks         []Task
}

// Edit — мутация дага (partial update).
type Edit struct {
	Template   *string
	Schedule   *string
	Catchup    *bool
	Paused     *bool
	Pool       *string
	ModifiedAt *time.Time
}

// ListReq — параметры выборки.
type ListReq struct {
	commonModel.ListParams

	Project  *string
	Template *string
	Paused   *bool
	// AutoUpdate — фильтр по флагу проекта (join): кандидаты dagsync.
	AutoUpdate *bool
}
