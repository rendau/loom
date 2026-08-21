package loom

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	json "github.com/goccy/go-json"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/rendau/loom/api/server_v1"
)

// Version — версия SDK. Попадает в манифест; server проверяет по ней
// совместимость при регистрации дага.
const Version = "0.1.0"

// EnvDescribeID — env-контракт describe-Job'а (регистрация дага через k8s,
// решение №29): непустой id включает отправку манифеста (или ошибки
// валидации дага) на control plane по адресу EnvServerAddr — describe_id
// одноразовый, им регистрация сопоставляет ответ Job'а. Печать манифеста в
// stdout при этом сохраняется (диагностика через kubectl logs).
const EnvDescribeID = "LOOM_DESCRIBE_ID"

// pushManifestTimeout — на дозвон до control plane из describe-Job'а.
const pushManifestTimeout = 30 * time.Second

const usage = `usage: <dag-binary> <command>

commands:
  describe   напечатать JSON-манифест дага (регистрация и валидация на
             server); с env LOOM_DESCRIBE_ID + LOOM_SERVER_ADDR манифест
             дополнительно отправляется на control plane (describe-Job
             регистрации через kubernetes)
  run        выполнить даг целиком в локальном режиме (in-process)
  run --task=<name> --run-id=<id> --attempt=<n>
             выполнить один таск в распределённом режиме (вызывает executor);
             параметры также берутся из env: LOOM_ARTIFACT_ADDR (обязателен),
             LOOM_SERVER_ADDR, LOOM_RUN_ID, LOOM_TASK, LOOM_ATTEMPT,
             LOOM_DEP_ATTEMPTS, LOOM_TOKEN; флаги приоритетнее env

exit codes: 0 — успех, 1 — таск упал, 2 — некорректный вызов/конфигурация
`

// Main — входная точка бинарника дага; вызывается последней строкой main().
// Один и тот же бинарник отдаёт манифест (describe), выполняет даг локально
// (run) и отрабатывает один таск в распределённом режиме (run --task).
func Main(d *DAG) {
	os.Exit(runCLI(d, os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(d *DAG, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	validateErr := d.Validate()

	switch args[0] {
	case "describe":
		return runDescribe(d, validateErr, stdout, stderr)

	case "run":
		if validateErr != nil {
			fmt.Fprintf(stderr, "invalid dag: %v\n", validateErr)
			return 2
		}
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		fs.SetOutput(stderr)
		task := fs.String("task", "", "имя таска (распределённый режим)")
		runID := fs.String("run-id", "", "id рана (распределённый режим)")
		attempt := fs.Int("attempt", 0, "номер попытки (распределённый режим; 0 — из env, иначе 1)")
		params := fs.String("params", "", "параметры рана, JSON-объект (локальный режим)")
		if err := fs.Parse(args[1:]); err != nil {
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
		if err := d.RunLocal(ctx, opts...); err != nil {
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

// runDescribe печатает манифест в stdout, а при непустом EnvDescribeID ещё
// и отправляет его на control plane (push-режим describe-Job'а, решение
// №29). Ошибка валидации дага в push-режиме тоже уходит на server — иначе
// регистрация ждала бы таймаута и разбирала логи пода.
func runDescribe(d *DAG, validateErr error, stdout, stderr io.Writer) int {
	describeID := os.Getenv(EnvDescribeID)

	if validateErr != nil {
		if describeID != "" {
			if err := pushManifest(describeID, nil, validateErr.Error()); err != nil {
				fmt.Fprintf(stderr, "push manifest: %v\n", err)
			}
		}
		fmt.Fprintf(stderr, "invalid dag: %v\n", validateErr)
		return 2
	}

	manifest, err := json.MarshalIndent(d.Manifest(), "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode manifest: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(manifest))

	if describeID == "" {
		return 0
	}
	if err = pushManifest(describeID, manifest, ""); err != nil {
		fmt.Fprintf(stderr, "push manifest: %v\n", err)
		return 1
	}
	return 0
}

// pushManifest отправляет манифест (или ошибку валидации) на control plane
// вызовом DagService.PushDagManifest.
func pushManifest(describeID string, manifest []byte, errMsg string) error {
	addr := os.Getenv(EnvServerAddr)
	if addr == "" {
		return fmt.Errorf("%s is required when %s is set", EnvServerAddr, EnvDescribeID)
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial control plane %q: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), pushManifestTimeout)
	defer cancel()

	_, err = pb.NewDagServiceClient(conn).PushDagManifest(ctx, &pb.DagPushManifestReq{
		DescribeId: describeID,
		Manifest:   manifest,
		Error:      errMsg,
	})
	if err != nil {
		return fmt.Errorf("push dag manifest: %w", err)
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
