package loom

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendau/loom/sdk/streamstore"
)

func TestRunLocalStreamed(t *testing.T) {
	d := New("test_dag")

	produce := d.Task("produce", func(_ context.Context, rt *Runtime) error {
		out, err := rt.Output("nums")
		if err != nil {
			return err
		}
		for i := 1; i <= 100; i++ {
			if _, err = fmt.Fprintf(out, "%d\n", i); err != nil {
				return err
			}
		}
		return nil
	})

	var sum int
	d.Task("consume", func(_ context.Context, rt *Runtime) error {
		in, err := rt.Input("produce", "nums")
		if err != nil {
			return err
		}
		defer in.Close()

		scanner := bufio.NewScanner(in)
		for scanner.Scan() {
			n, err := strconv.Atoi(scanner.Text())
			if err != nil {
				return err
			}
			sum += n
		}
		return scanner.Err()
	}, AfterStreamed(produce))

	require.NoError(t, d.RunLocal(context.Background(), LocalDir(t.TempDir())))
	assert.Equal(t, 5050, sum)
}

func TestRunLocalRegularEdge(t *testing.T) {
	d := New("test_dag")

	produce := d.Task("produce", func(_ context.Context, rt *Runtime) error {
		out, err := rt.Output("data")
		if err != nil {
			return err
		}
		_, err = out.Write([]byte("payload"))
		return err // Close не нужен — commit сделает Runtime при успехе
	})

	var got string
	d.Task("consume", func(_ context.Context, rt *Runtime) error {
		in, err := rt.Input("produce", "data")
		if err != nil {
			return err
		}
		defer in.Close()

		b, err := io.ReadAll(in)
		got = string(b)
		return err
	}, After(produce))

	require.NoError(t, d.RunLocal(context.Background(), LocalDir(t.TempDir())))
	assert.Equal(t, "payload", got)
}

func TestRunLocalFailurePropagates(t *testing.T) {
	d := New("test_dag")
	boom := errors.New("boom")

	produce := d.Task("produce", func(_ context.Context, rt *Runtime) error {
		out, err := rt.Output("nums")
		if err != nil {
			return err
		}
		if _, err = out.Write([]byte("1\n")); err != nil {
			return err
		}
		return boom // артефакт будет abort'нут — читатель не зависнет
	})

	d.Task("consume", func(_ context.Context, rt *Runtime) error {
		in, err := rt.Input("produce", "nums")
		if err != nil {
			return err
		}
		defer in.Close()

		_, err = io.ReadAll(in)
		return err
	}, AfterStreamed(produce))

	err := d.RunLocal(context.Background(), LocalDir(t.TempDir()))
	require.Error(t, err)
	// errgroup возвращает первую ошибку: это либо boom от produce,
	// либо ошибка abort'нутого стрима у consume — обе валидны
	if !errors.Is(err, boom) {
		assert.ErrorIs(t, err, streamstore.ErrAborted)
	}
}

func TestRunLocalCloseThenErrorAborts(t *testing.T) {
	// ключевой сценарий новой семантики: программист закрыл out, но таск
	// упал — артефакт должен быть abort'нут, а не остаться committed
	d := New("test_dag")
	boom := errors.New("boom")

	produce := d.Task("produce", func(_ context.Context, rt *Runtime) error {
		out, err := rt.Output("data")
		if err != nil {
			return err
		}
		if _, err = out.Write([]byte("looks complete")); err != nil {
			return err
		}
		if err = out.Close(); err != nil {
			return err
		}
		return boom
	})

	var consumeErr error
	d.Task("consume", func(_ context.Context, rt *Runtime) error {
		in, err := rt.Input("produce", "data")
		if err != nil {
			consumeErr = err
			return err
		}
		defer in.Close()

		_, consumeErr = io.ReadAll(in)
		return consumeErr
	}, AfterStreamed(produce))

	err := d.RunLocal(context.Background(), LocalDir(t.TempDir()))
	require.Error(t, err)
	// потребитель не должен успешно съесть данные неуспешной попытки
	require.Error(t, consumeErr)
}

func TestRunLocalDoubleCloseSafe(t *testing.T) {
	d := New("test_dag")

	produce := d.Task("produce", func(_ context.Context, rt *Runtime) error {
		out, err := rt.Output("data")
		if err != nil {
			return err
		}
		if _, err = out.Write([]byte("payload")); err != nil {
			return err
		}
		if err = out.Close(); err != nil {
			return err
		}
		return out.Close() // повторный Close безопасен
	})

	var got string
	d.Task("consume", func(_ context.Context, rt *Runtime) error {
		in, err := rt.Input("produce", "data")
		if err != nil {
			return err
		}
		defer in.Close()

		b, err := io.ReadAll(in)
		got = string(b)
		return err
	}, After(produce))

	require.NoError(t, d.RunLocal(context.Background(), LocalDir(t.TempDir())))
	assert.Equal(t, "payload", got)
}

func TestRunLocalWriteAfterCloseFails(t *testing.T) {
	d := New("test_dag")

	d.Task("produce", func(_ context.Context, rt *Runtime) error {
		out, err := rt.Output("data")
		if err != nil {
			return err
		}
		if err = out.Close(); err != nil {
			return err
		}
		_, err = out.Write([]byte("late"))
		assert.Error(t, err)
		return nil
	})

	require.NoError(t, d.RunLocal(context.Background(), LocalDir(t.TempDir())))
}

func TestRunLocalPanicRecovered(t *testing.T) {
	d := New("test_dag")

	d.Task("bad", func(_ context.Context, _ *Runtime) error {
		panic("kaboom")
	})

	err := d.RunLocal(context.Background(), LocalDir(t.TempDir()))
	require.Error(t, err)
	assert.ErrorContains(t, err, "panic: kaboom")
}

func TestRunLocalUndeclaredInput(t *testing.T) {
	d := New("test_dag")

	d.Task("stranger", func(_ context.Context, rt *Runtime) error {
		_, err := rt.Output("data")
		return err
	})

	d.Task("sneaky", func(_ context.Context, rt *Runtime) error {
		_, err := rt.Input("stranger", "data") // зависимость не объявлена
		return err
	})

	err := d.RunLocal(context.Background(), LocalDir(t.TempDir()))
	require.Error(t, err)
	assert.ErrorContains(t, err, "not a declared dependency")
}

func TestRunLocalMissingArtifactResolves(t *testing.T) {
	// потребитель ждёт артефакт, который отправитель так и не создал:
	// после успеха отправителя ожидание разрешается в NotFound, а не деДлок
	d := New("test_dag")

	produce := d.Task("produce", func(_ context.Context, _ *Runtime) error {
		return nil // ничего не пишет
	})

	d.Task("consume", func(_ context.Context, rt *Runtime) error {
		_, err := rt.Input("produce", "never_written")
		return err
	}, AfterStreamed(produce))

	err := d.RunLocal(context.Background(), LocalDir(t.TempDir()))
	require.Error(t, err)
	assert.ErrorIs(t, err, streamstore.ErrNotFound)
}

func TestRunLocalTaskTimeout(t *testing.T) {
	// Timeout отменяет контекст таска: кооперативный таск завершается с
	// ошибкой дедлайна, ран падает
	d := New("test_dag")

	d.Task("slow", func(ctx context.Context, _ *Runtime) error {
		<-ctx.Done()
		return context.Cause(ctx)
	}, Timeout(50*time.Millisecond))

	err := d.RunLocal(context.Background(), LocalDir(t.TempDir()))
	require.Error(t, err)
	assert.ErrorContains(t, err, "timeout")
}

func TestRunLocalParamsAndLogicalDate(t *testing.T) {
	d := New("params_dag")

	type cfg struct {
		Date  string `json:"date"`
		Limit int    `json:"limit"`
	}

	var got cfg
	var logicalDate time.Time
	d.Task("a", func(_ context.Context, rt *Runtime) error {
		logicalDate = rt.LogicalDate()
		return rt.BindParams(&got)
	})

	before := time.Now()
	err := d.RunLocal(context.Background(), LocalDir(t.TempDir()),
		LocalParams([]byte(`{"date":"2026-08-01","limit":10}`)))
	require.NoError(t, err)

	assert.Equal(t, cfg{Date: "2026-08-01", Limit: 10}, got)
	assert.False(t, logicalDate.Before(before), "logical date локального рана — время запуска")

	// без параметров: Params() == nil, BindParams — ошибка
	d2 := New("no_params_dag")
	d2.Task("a", func(_ context.Context, rt *Runtime) error {
		assert.Nil(t, rt.Params())
		var v cfg
		assert.Error(t, rt.BindParams(&v))
		return nil
	})
	require.NoError(t, d2.RunLocal(context.Background(), LocalDir(t.TempDir())))
}

func TestRunLocalTaskValues(t *testing.T) {
	d := New("values_dag")

	produce := d.Task("produce", func(_ context.Context, rt *Runtime) error {
		return rt.PushValue("rows", 42)
	})

	var got int
	d.Task("consume", func(_ context.Context, rt *Runtime) error {
		return rt.PullValue("produce", "rows", &got)
	}, After(produce))

	require.NoError(t, d.RunLocal(context.Background(), LocalDir(t.TempDir())))
	assert.Equal(t, 42, got)

	// чтение у не-зависимости и отсутствующего ключа — ошибки
	d2 := New("values_bad")
	other := d2.Task("other", func(context.Context, *Runtime) error { return nil })
	prod2 := d2.Task("produce", func(context.Context, *Runtime) error { return nil })
	d2.Task("consume", func(_ context.Context, rt *Runtime) error {
		var v int
		assert.Error(t, rt.PullValue("other", "rows", &v), "other — не зависимость")
		assert.Error(t, rt.PullValue("produce", "missing", &v), "ключа нет")
		return nil
	}, After(prod2))
	_ = other
	require.NoError(t, d2.RunLocal(context.Background(), LocalDir(t.TempDir())))
}
