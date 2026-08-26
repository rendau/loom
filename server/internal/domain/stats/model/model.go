package model

import (
	"time"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

// Dashboard — сводка главной страницы админки.
type Dashboard struct {
	ActiveRuns     int64
	DagCount       int64
	PausedDagCount int64
	Last24h        Window
	Last7d         Window
	Upcoming       []Upcoming
	Pools          []PoolUsage
	RecentFailures []Failure
	Activity       []Day
	ActivityHours  []Hour
	DagDurations   []DagDuration
}

// Window — исходы завершённых ранов за период.
type Window struct {
	Success int64
	Failed  int64
}

// Upcoming — ближайший запуск дага по расписанию.
type Upcoming struct {
	Dag       dagModel.Ref
	NextRunAt time.Time
	Schedule  string
}

// PoolUsage — занятость пула слотов.
type PoolUsage struct {
	Name  string
	Slots int64
	Busy  int64
}

// Failure — недавно провалившийся ран; Task/ExitReason — первый упавший
// таск и исход его последней попытки («что именно сломалось» на обзоре).
type Failure struct {
	RunId      string
	Dag        dagModel.Ref
	FinishedAt time.Time
	Task       string
	ExitReason string
}

// TaskStat — агрегат по таску за последние N завершённых ранов дага
// («жирные таски»): длительности текущих попыток и пики памяти. Память
// приблизительна и может отсутствовать (nil) у коротких попыток.
type TaskStat struct {
	Task               string
	Runs               int64
	AvgDurationSec     float64
	MaxDurationSec     float64
	AvgPeakMemoryBytes *int64
	MaxPeakMemoryBytes *int64
}

// Hour — раны за час. У молодой инсталляции вся история умещается в
// сутки, и график по дням вырождается в один столбик — тогда админка
// показывает этот ряд.
type Hour struct {
	Hour    time.Time
	Success int64
	Failed  int64
	Running int64
}

// Day — раны за календарный день (UTC).
type Day struct {
	Date    string // YYYY-MM-DD
	Success int64
	Failed  int64
	Running int64
}

// DagDuration — длительности ранов дага за период.
type DagDuration struct {
	Dag    dagModel.Ref
	AvgSec float64
	MaxSec float64
	Runs   int64
}
