// multi-dag — пример образа с несколькими дагами: один бинарник объявляет
// два дага, каждый из них становится шаблоном проекта на control plane.
// Инстансов одного шаблона может быть сколько угодно — различаются они
// переменными и секретами дага (задаются в админке).
//
//	go run ./multi-dag describe             # JSON-каталог образа: оба дага
//	go run ./multi-dag run --dag=orders_etl # локальный запуск одного дага
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"

	loom "github.com/rendau/loom/sdk"
)

func main() {
	loom.Main(newOrdersETL(), newNsiSync())
}

// newOrdersETL — обычный даг из двух тасков.
func newOrdersETL() *loom.DAG {
	dag := loom.New("orders_etl")

	extract := dag.Task("extract", func(_ context.Context, rt *loom.Runtime) error {
		out, err := rt.Output("orders")
		if err != nil {
			return err
		}
		for i := 1; i <= 100; i++ {
			if _, err = fmt.Fprintf(out, "order-%d\n", i); err != nil {
				return err
			}
		}
		return nil
	})

	dag.Task("load", func(_ context.Context, rt *loom.Runtime) error {
		in, err := rt.Input("extract", "orders")
		if err != nil {
			return err
		}
		defer in.Close()

		count := 0
		scanner := bufio.NewScanner(in)
		for scanner.Scan() {
			count++
		}
		if err = scanner.Err(); err != nil {
			return err
		}

		rt.Log().Info("loaded", "count", count)
		return nil
	}, loom.After(extract))

	return dag
}

// newNsiSync — даг, который параметризуется переменными: от инстанса к
// инстансу меняются только их значения (SHOP у каждого свой), код общий.
func newNsiSync() *loom.DAG {
	dag := loom.New("nsi_sync")

	dag.Task("sync", func(_ context.Context, rt *loom.Runtime) error {
		shop := os.Getenv("SHOP")
		if shop == "" {
			return fmt.Errorf("SHOP is required")
		}

		rt.Log().Info("syncing nsi", "shop", shop)
		return nil
	}, loom.Variable("SHOP", "shop", "код магазина, для которого синхронизируем НСИ"))

	return dag
}
