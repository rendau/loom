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
             выполнить один таск в распределённом режиме (вызывает executor)
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
		fs.String("run-id", "", "id рана (распределённый режим)")
		fs.Int("attempt", 1, "номер попытки (распределённый режим)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}

		if *task != "" {
			// распределённый режим появится вместе с control-plane server
			fmt.Fprintln(stderr, "distributed run mode is not implemented yet")
			return 1
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

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
