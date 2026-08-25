package model

import (
	"time"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
)

// Main — зарегистрированный даг. Tasks/SdkVersion — распарсенный манифест,
// Manifest — его исходный JSON (снапшотится в ран при триггере).
// Schedule/Catchup задаются через админку, не манифестом.
type Main struct {
	Name        string
	Image       string
	ImageDigest string
	Schedule    string
	Catchup     bool // наверстывать пропущенные тики расписания
	// Лимит одновременно выполняющихся ранов дага; 0 — без лимита
	//.
	MaxActiveRuns int
	Paused        bool
	// AutoUpdate — poll-синк новой версии образа: свойство
	// деплоя, не манифеста; хранится в БД.
	AutoUpdate bool
	SdkVersion string
	Tasks      []Task
	Manifest   []byte
	NextRunAt  time.Time // zero — без расписания / не инициализировано
	CreatedAt  time.Time
	ModifiedAt time.Time // zero — не изменялся
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
	Pool          string // пул слотов; пусто — "default"
	Priority      int    // больше — раньше из очереди
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

// SecretRef — инъекция секрета control plane в env контейнера таска
// .
type SecretRef struct {
	Env    string
	Secret string
}

// VariableRef — инъекция переменной control plane в env контейнера таска
// (значение, в отличие от секрета, видно в админке).
type VariableRef struct {
	Env      string
	Variable string
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

// Manifest — распарсенный манифест дага (вывод `describe`).
type Manifest struct {
	SdkVersion    string
	Name          string
	MaxActiveRuns int
	Tasks         []Task
}

// Edit — мутация дага (partial update).
type Edit struct {
	Image       *string
	ImageDigest *string
	Schedule    *string
	Catchup     *bool
	Paused      *bool
	AutoUpdate  *bool
	Manifest    *[]byte
	ModifiedAt  *time.Time
}

// ListReq — параметры выборки.
type ListReq struct {
	commonModel.ListParams

	Paused     *bool
	AutoUpdate *bool
}
