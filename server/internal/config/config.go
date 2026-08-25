package config

import (
	"time"

	"github.com/caarlos0/env/v9"
	_ "github.com/joho/godotenv/autoload"
)

var Conf = struct {
	Debug    bool   `env:"DEBUG" envDefault:"false"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	GrpcPort       string `env:"GRPC_PORT" envDefault:"5050"`
	HttpPort       string `env:"HTTP_PORT" envDefault:"80"`
	HttpCors       bool   `env:"HTTP_CORS" envDefault:"false"`
	SystemHttpPort string `env:"SYSTEM_HTTP_PORT" envDefault:"3003"`

	// SPA админки: AdminDir — каталог собранной статики (nuxt generate;
	// каталога нет — админка не раздаётся), AdminPort — её отдельный порт.
	// AdminApiBaseUrl — базовый URL gateway API, каким его видит браузер;
	// уходит в /config.js (window.__APP_CONFIG__) — задаётся после билда
	// SPA, один образ на все окружения.
	AdminPort       string `env:"ADMIN_PORT" envDefault:"8081"`
	AdminDir        string `env:"ADMIN_DIR" envDefault:"./admin-ui"`
	AdminApiBaseUrl string `env:"ADMIN_API_BASE_URL" envDefault:"http://localhost:8082"`

	PgDsn string `env:"PG_DSN"`

	// ArtifactAddr — адрес artifact-сервера для самого control plane
	// (FinishAttempt-страховка, retention).
	ArtifactAddr string `env:"ARTIFACT_ADDR" envDefault:"127.0.0.1:5051"`

	// TaskArtifactAddr / TaskServerAddr — адреса artifact-сервера и control
	// plane, какими их видят поды тасков (cluster DNS); попадают в env
	// контейнера (LOOM_ARTIFACT_ADDR / LOOM_SERVER_ADDR).
	TaskArtifactAddr string `env:"TASK_ARTIFACT_ADDR" envDefault:"127.0.0.1:5051"`
	TaskServerAddr   string `env:"TASK_SERVER_ADDR" envDefault:"127.0.0.1:5052"`

	// DockerBin — бинарь container-CLI: регистрация дагов (pull/describe) и
	// docker-executor.
	DockerBin string `env:"DOCKER_BIN" envDefault:"docker"`

	// Executor: k8s — kubernetes Job'ы; docker — контейнеры на хосте через
	// docker CLI (один хост без кластера); none — не запускать
	// executor (dev-режим: только API).
	Executor string `env:"EXECUTOR" envDefault:"k8s"`

	// DockerNetwork — docker-сеть контейнеров тасков (пусто — дефолтная);
	// адреса planes для контейнеров задаются TASK_*_ADDR (например
	// host.docker.internal:5051).
	DockerNetwork string `env:"DOCKER_NETWORK"`
	// DockerPollTick — период поллинга завершений контейнеров docker-executor'ом.
	DockerPollTick time.Duration `env:"DOCKER_POLL_TICK" envDefault:"3s"`

	K8sNamespace  string `env:"K8S_NAMESPACE" envDefault:"default"`
	K8sKubeconfig string `env:"K8S_KUBECONFIG"` // пусто — in-cluster, иначе путь к kubeconfig
	// K8sDescribeTimeout — таймаут describe-Job'а регистрации дага; должен
	// покрывать и pull образа.
	K8sDescribeTimeout time.Duration `env:"K8S_DESCRIBE_TIMEOUT" envDefault:"5m"`
	// K8sImagePullSecret — имя dockerconfigjson-секрета в K8S_NAMESPACE для
	// pull приватных образов дагов: подставляется в imagePullSecrets подов
	// попыток и describe-Job'ов; пусто — без секрета (публичные образы).
	K8sImagePullSecret string `env:"K8S_IMAGE_PULL_SECRET"`
	// K8sMetricsTick — период семплинга памяти подов попыток через
	// metrics.k8s.io (нужен metrics-server); 0 — выключено.
	K8sMetricsTick time.Duration `env:"K8S_METRICS_TICK" envDefault:"15s"`

	// SchedTick — период планировщика; события executor'а будят его раньше.
	SchedTick time.Duration `env:"SCHED_TICK" envDefault:"2s"`
	// SchedCronTick — период проверки cron-расписаний дагов.
	SchedCronTick time.Duration `env:"SCHED_CRON_TICK" envDefault:"15s"`
	// SchedReconcileTick — период зомби-детекта (сверка попыток с Job'ами).
	SchedReconcileTick time.Duration `env:"SCHED_RECONCILE_TICK" envDefault:"30s"`
	// SchedZombieGrace — возраст попытки, до которого зомби-детект её не
	// трогает (отсекает гонку claim → Launch).
	SchedZombieGrace time.Duration `env:"SCHED_ZOMBIE_GRACE" envDefault:"60s"`
	// SchedClaimLimit — сколько queued-тасков забирать за один проход.
	SchedClaimLimit int64 `env:"SCHED_CLAIM_LIMIT" envDefault:"10"`

	// RetentionTick — период проходов очистки завершённых ранов; сами
	// лимиты (run_ttl, run_keep_last) — настройки в БД, правятся админкой.
	RetentionTick time.Duration `env:"RETENTION_TICK" envDefault:"1h"`

	// DagSyncTick — период авто-обновления дагов: digest-чек
	// тега в registry для дагов с auto_update; 0 — выключено.
	DagSyncTick time.Duration `env:"DAG_SYNC_TICK" envDefault:"5m"`

	// DagRegTick — период воркера очереди регистраций дагов (Enqueue будит
	// его раньше).
	DagRegTick time.Duration `env:"DAG_REG_TICK" envDefault:"2s"`
	// DagRegStale — возраст running-регистрации, после которого она
	// считается брошенной (инстанс умер посреди describe) и падает в failed;
	// должен покрывать pull + describe. TTL истории регистраций — настройка
	// dag_reg_ttl в БД.
	DagRegStale time.Duration `env:"DAG_REG_STALE" envDefault:"30m"`
	// RegistryAuthFile — путь к docker config.json с кредами registry для
	// digest-чека приватных образов; пусто — anonymous-доступ.
	RegistryAuthFile string `env:"REGISTRY_AUTH_FILE"`

	// SecretKey — парольная фраза шифрования секретов (AES-256-GCM, ключ —
	// SHA-256 от фразы); пусто — секреты хранятся открытым текстом (dev).
	SecretKey string `env:"SECRET_KEY"`
}{}

func init() {
	if err := env.Parse(&Conf); err != nil {
		panic(err)
	}
}
