// demo-etl — пример дага на loom SDK: extract → (stream) → transform → load.
//
//	go run ./demo-etl describe   # JSON-каталог образа
//	go run ./demo-etl run        # локальный запуск целиком
package main

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"

	loom "github.com/rendau/loom/sdk"
)

func main() {
	dag := loom.New("demo_etl")

	extract := dag.Task("extract", func(_ context.Context, rt *loom.Runtime) error {
		out, err := rt.Output("orders")
		if err != nil {
			return err
		}

		for i := 1; i <= 1000; i++ {
			if _, err = fmt.Fprintf(out, "order-%d;%d\n", i, i%97+1); err != nil {
				return err
			}
		}

		rt.Log().Info("extracted", "count", 1000)
		// Close опционален: commit всех выходов происходит при успехе таска
		return nil
	})

	transform := dag.Task("transform", func(_ context.Context, rt *loom.Runtime) error {
		in, err := rt.Input("extract", "orders")
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := rt.Output("totals")
		if err != nil {
			return err
		}

		// AfterStreamed: читаем стрим по мере записи extract'ом
		scanner := bufio.NewScanner(in)
		for scanner.Scan() {
			id, amountRaw, ok := strings.Cut(scanner.Text(), ";")
			if !ok {
				return fmt.Errorf("bad line: %q", scanner.Text())
			}
			amount, err := strconv.Atoi(amountRaw)
			if err != nil {
				return err
			}
			if _, err = fmt.Fprintf(out, "%s;%d\n", id, amount*2); err != nil {
				return err
			}
		}
		if err = scanner.Err(); err != nil {
			return err
		}

		return out.Close() // Close допустим, но это не commit — только «больше не пишу»
	}, loom.AfterStreamed(extract))

	dag.Task("load", func(_ context.Context, rt *loom.Runtime) error {
		in, err := rt.Input("transform", "totals")
		if err != nil {
			return err
		}
		defer in.Close()

		var count, sum int
		scanner := bufio.NewScanner(in)
		for scanner.Scan() {
			_, amountRaw, _ := strings.Cut(scanner.Text(), ";")
			amount, err := strconv.Atoi(amountRaw)
			if err != nil {
				return err
			}
			count++
			sum += amount
		}
		if err = scanner.Err(); err != nil {
			return err
		}

		rt.Log().Info("loaded", "count", count, "sum", sum)
		return nil
	}, loom.After(transform))

	loom.Main(dag)
}
