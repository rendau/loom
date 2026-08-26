package loom

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/samber/lo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/rendau/loom/api/server_v1"
)

// Version — версия SDK. Попадает в каталог образа; server проверяет по ней
// совместимость при регистрации проекта.
const Version = "0.4.0"

// EnvDescribeID — env-контракт describe-Job'а (регистрация проекта через
// k8s): непустой id включает отправку каталога образа (или ошибки
// каталога) на control plane по адресу EnvServerAddr — describe_id
// одноразовый, им регистрация сопоставляет ответ Job'а. Печать каталога в
// stdout при этом сохраняется (диагностика через kubectl logs).
const EnvDescribeID = "LOOM_DESCRIBE_ID"

// pushCatalogTimeout — на дозвон до control plane из describe-Job'а.
const pushCatalogTimeout = 30 * time.Second

const usage = `usage: <dag-binary> <command>

commands:
  describe   напечатать JSON-каталог образа: манифесты всех дагов бинарника
             (регистрация и валидация на server); с env LOOM_DESCRIBE_ID +
             LOOM_SERVER_ADDR каталог дополнительно отправляется на control
             plane (describe-Job регистрации через kubernetes)
  run [--dag=<name>] [--params='{...}']
             выполнить даг целиком в локальном режиме (in-process); --dag
             обязателен, если бинарник несёт несколько дагов
  run [--dag=<name>] --task=<name> --run-id=<id> --attempt=<n>
             выполнить один таск в распределённом режиме (вызывает executor);
             параметры также берутся из env: LOOM_ARTIFACT_ADDR (обязателен),
             LOOM_SERVER_ADDR, LOOM_DAG, LOOM_RUN_ID, LOOM_TASK, LOOM_ATTEMPT,
             LOOM_DEP_ATTEMPTS; флаги приоритетнее env

exit codes: 0 — успех, 1 — таск упал, 2 — некорректный вызов/конфигурация
`

// Main — входная точка бинарника дага; вызывается последней строкой main().
// Один и тот же бинарник отдаёт каталог своих дагов (describe), выполняет
// даг локально (run) и отрабатывает один таск в распределённом режиме
// (run --task). Дагов в образе может быть несколько — каждый становится
// шаблоном проекта на control plane.
func Main(dags ...*DAG) {
	os.Exit(runCLI(dags, os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(dags []*DAG, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "describe":
		return runDescribe(dags, stdout, stderr)

	case "run":
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		fs.SetOutput(stderr)
		dagName := fs.String("dag", "", "имя дага в образе (по умолчанию единственный)")
		task := fs.String("task", "", "имя таска (распределённый режим)")
		runID := fs.String("run-id", "", "id рана (распределённый режим)")
		attempt := fs.Int("attempt", 0, "номер попытки (распределённый режим; 0 — из env, иначе 1)")
		params := fs.String("params", "", "параметры рана, JSON-объект (локальный режим)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}

		if *dagName == "" {
			*dagName = os.Getenv(EnvDag)
		}

		d, err := selectDag(dags, *dagName)
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return 2
		}
		if err = d.Validate(); err != nil {
			fmt.Fprintf(stderr, "invalid dag %q: %v\n", d.name, err)
			return 2
		}

		if *params != "" && !json.Valid([]byte(*params)) {
			fmt.Fprintf(stderr, "invalid --params: not a valid JSON\n")
			return 2
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		if *task != "" || os.Getenv(EnvTask) != "" {
			return runDistributedTask(ctx, d, *task, *runID, *attempt, stderr)
		}

		var opts []LocalOption
		if *params != "" {
			opts = append(opts, LocalParams([]byte(*params)))
		}
		if err = d.RunLocal(ctx, opts...); err != nil {
			fmt.Fprintf(stderr, "run failed: %v\n", err)
			return 1
		}
		return 0

	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}

// selectDag выбирает даг образа по имени; пустое имя допустимо, только
// если даг в образе один.
func selectDag(dags []*DAG, name string) (*DAG, error) {
	names := lo.Map(dags, func(d *DAG, _ int) string { return d.name })

	switch {
	case len(dags) == 0:
		return nil, errors.New("no dags declared")

	case name == "":
		if len(dags) == 1 {
			return dags[0], nil
		}
		return nil, fmt.Errorf("dag name is required: image carries %d dags (%s)",
			len(dags), strings.Join(names, ", "))

	default:
		d, ok := lo.Find(dags, func(d *DAG) bool { return d.name == name })
		if !ok {
			return nil, fmt.Errorf("unknown dag %q: image carries %s", name, strings.Join(names, ", "))
		}
		return d, nil
	}
}

// runDescribe печатает каталог образа в stdout, а при непустом
// EnvDescribeID ещё и отправляет его на control plane (push-режим
// describe-Job'а). Ошибки валидации отдельных дагов едут внутри каталога:
// остальные даги образа регистрируются как обычно. Сводка уезжает
// отдельным полем error, только если валидных дагов не осталось ни одного —
// такую регистрацию server завалит целиком, не разбирая каталог.
func runDescribe(dags []*DAG, stdout, stderr io.Writer) int {
	describeID := os.Getenv(EnvDescribeID)

	catalog, err := buildCatalog(dags)
	if err != nil {
		if describeID != "" {
			if pushErr := pushCatalog(describeID, nil, err.Error()); pushErr != nil {
				fmt.Fprintf(stderr, "push catalog: %v\n", pushErr)
			}
		}
		fmt.Fprintf(stderr, "invalid dag catalog: %v\n", err)
		return 2
	}

	raw, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode catalog: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(raw))

	broken := lo.Filter(catalog.Dags, func(v CatalogDag, _ int) bool { return v.Error != "" })
	for _, v := range broken {
		fmt.Fprintf(stderr, "invalid dag %q: %s\n", v.Name, v.Error)
	}

	// ни одного валидного дага — регистрировать нечего
	allBroken := len(broken) == len(catalog.Dags)

	if describeID != "" {
		errMsg := ""
		if allBroken {
			errMsg = strings.Join(lo.Map(broken, func(v CatalogDag, _ int) string {
				return fmt.Sprintf("dag %q: %s", v.Name, v.Error)
			}), "; ")
		}
		if err = pushCatalog(describeID, raw, errMsg); err != nil {
			fmt.Fprintf(stderr, "push catalog: %v\n", err)
			return 1
		}
	}

	if allBroken {
		return 2
	}
	return 0
}

// pushCatalog отправляет каталог образа (или ошибку) на control plane
// вызовом ProjectService.PushDagCatalog.
func pushCatalog(describeID string, catalog []byte, errMsg string) error {
	addr := os.Getenv(EnvServerAddr)
	if addr == "" {
		return fmt.Errorf("%s is required when %s is set", EnvServerAddr, EnvDescribeID)
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial control plane %q: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), pushCatalogTimeout)
	defer cancel()

	_, err = pb.NewProjectServiceClient(conn).PushDagCatalog(ctx, &pb.DagPushCatalogReq{
		DescribeId: describeID,
		Catalog:    catalog,
		Error:      errMsg,
	})
	if err != nil {
		return fmt.Errorf("push dag catalog: %w", err)
	}
	return nil
}

// runDistributedTask собирает спеку из env-контракта executor'а (флаги
// приоритетнее) и выполняет один таск.
func runDistributedTask(ctx context.Context, d *DAG, task, runID string, attempt int, stderr io.Writer) int {
	spec, err := taskRunSpecFromEnv()
	if err != nil {
		fmt.Fprintf(stderr, "invalid env: %v\n", err)
		return 2
	}

	if task != "" {
		spec.Task = task
	}
	if runID != "" {
		spec.RunID = runID
	}
	if attempt > 0 {
		spec.Attempt = attempt
	}
	if spec.Attempt == 0 {
		spec.Attempt = 1
	}
	spec.CaptureOutput = true

	if err = spec.validate(); err != nil {
		fmt.Fprintf(stderr, "invalid task run spec: %v\n", err)
		return 2
	}
	if _, ok := d.tasks[spec.Task]; !ok {
		fmt.Fprintf(stderr, "unknown task %q\n", spec.Task)
		return 2
	}

	if err = d.RunTask(ctx, spec); err != nil {
		fmt.Fprintf(stderr, "task failed: %v\n", err)
		return 1
	}
	return 0
}
