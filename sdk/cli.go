package loom

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	json "github.com/goccy/go-json"
)

// Version — версия SDK. Попадает в манифест; server проверяет по ней
// совместимость при регистрации дага.
const Version = "0.1.0"

const usage = `usage: <dag-binary> <command>

commands:
  describe   напечатать JSON-манифест дага (регистрация и валидация на server)
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

	if err := d.Validate(); err != nil {
		fmt.Fprintf(stderr, "invalid dag: %v\n", err)
		return 2
	}

	switch args[0] {
	case "describe":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(d.Manifest()); err != nil {
			fmt.Fprintf(stderr, "encode manifest: %v\n", err)
			return 1
		}
		return 0

	case "run":
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		fs.SetOutput(stderr)
		task := fs.String("task", "", "имя таска (распределённый режим)")
		runID := fs.String("run-id", "", "id рана (распределённый режим)")
		attempt := fs.Int("attempt", 0, "номер попытки (распределённый режим; 0 — из env, иначе 1)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		if *task != "" || os.Getenv(EnvTask) != "" {
			return runDistributedTask(ctx, d, *task, *runID, *attempt, stderr)
		}

		if err := d.RunLocal(ctx); err != nil {
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
