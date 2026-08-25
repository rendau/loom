package model

import "time"

// Имена известных настроек. Список фиксирован: значения сидируются
// миграцией, произвольные имена сервис отклоняет.
const (
	// RunTTL — TTL завершённых ранов (артефакты, логи, записи БД); 0 —
	// по времени не чистить.
	RunTTL = "run_ttl"
	// RunKeepLast — хранить N последних завершённых ранов дага; 0 — не
	// ограничивать. Работает вместе с run_ttl: ран удаляется, если нарушает
	// любой из лимитов.
	RunKeepLast = "run_keep_last"
	// K8sJobTTL — ttlSecondsAfterFinished Job'ов попыток; 0 — не удалять.
	K8sJobTTL = "k8s_job_ttl"
	// DagRegTTL — TTL завершённых записей истории регистраций; 0 — вечно.
	// Только глобальная.
	DagRegTTL = "dag_reg_ttl"
)

// Типы значений настроек.
const (
	KindDuration = "duration" // Go-нотация: "720h", "90m", "0"
	KindInt      = "int"
)

// Def — описание известной настройки: тип значения, глобальный дефолт
// (страховка на случай отсутствия строки в БД) и допустимость уточнения
// на даге.
type Def struct {
	Name    string
	Kind    string
	Default string
	PerDag  bool
}

// Defs — реестр известных настроек.
var Defs = map[string]Def{
	RunTTL:      {Name: RunTTL, Kind: KindDuration, Default: "720h", PerDag: true},
	RunKeepLast: {Name: RunKeepLast, Kind: KindInt, Default: "0", PerDag: true},
	K8sJobTTL:   {Name: K8sJobTTL, Kind: KindDuration, Default: "1h", PerDag: true},
	DagRegTTL:   {Name: DagRegTTL, Kind: KindDuration, Default: "720h", PerDag: false},
}

// Main — сохранённое значение настройки в скоупе.
type Main struct {
	DagName    string // '' — глобальный скоуп
	Name       string
	Value      string
	ModifiedAt time.Time
}

// Effective — результат резолва настроек для скоупа дага: значение дага
// перекрывает глобальное, отсутствие обоих закрывает дефолт из Defs.
type Effective struct {
	RunTTL      time.Duration
	RunKeepLast int64
	K8sJobTTL   time.Duration
	DagRegTTL   time.Duration
}
