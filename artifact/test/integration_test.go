// Package test — интеграционные тесты распределённого режима SDK против
// настоящего gRPC artifact-сервера: каждый RunTask играет роль отдельного
// контейнера таска, сервер поднимается in-process на 127.0.0.1.
package test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	artifactpb "github.com/rendau/loom/api/artifact_v1"
	serverpb "github.com/rendau/loom/api/server_v1"
	domain "github.com/rendau/loom/artifact/internal/domain/artifact"
	handler "github.com/rendau/loom/artifact/internal/handler/grpc"
	loom "github.com/rendau/loom/sdk"
	"github.com/rendau/loom/sdk/streamstore"
)

func startGrpcServer(t *testing.T, register func(*grpc.Server)) string {
	t.Helper()

	srv := grpc.NewServer()
	register(srv)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String()
}

func startArtifactServer(t *testing.T) string {
	t.Helper()

	svc, err := domain.New(t.TempDir())
	require.NoError(t, err)

	return startGrpcServer(t, func(srv *grpc.Server) {
		artifactpb.RegisterArtifactServiceServer(srv, handler.NewArtifact(svc))
	})
}

func taskSpec(artifactAddr, task string) loom.TaskRunSpec {
	return loom.TaskRunSpec{RunID: "run-1", Task: task, Attempt: 1, ArtifactAddr: artifactAddr}
}

// TestRunTaskStreamedExchange — стримовое ребро: получатель читает данные
// до commit отправителя (tail-follow через grpc), оба таска бегут
// одновременно, как при ко-старте executor'ом.
func TestRunTaskStreamedExchange(t *testing.T) {
	addr := startArtifactServer(t)

	head := []byte("head: first bytes go out before commit\n")
	tail := []byte("tail: the rest is written after\n")

	proceed := make(chan struct{})
	var got []byte

	d := loom.New("itest")
	producer := d.Task("producer", func(_ context.Context, rt *loom.Runtime) error {
		out, err := rt.Output("data")
		if err != nil {
			return err
		}
		if _, err = out.Write(head); err != nil {
			return err
		}

		// ждём, пока читатель заберёт голову: докажем чтение до commit
		<-proceed

		_, err = out.Write(tail)
		return err
	})
	d.Task("consumer", func(_ context.Context, rt *loom.Runtime) error {
		in, err := rt.Input("producer", "data")
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()

		buf := make([]byte, len(head))
		if _, err = io.ReadFull(in, buf); err != nil {
			return err
		}
		close(proceed)

		rest, err := io.ReadAll(in)
		if err != nil {
			return err
		}
		got = append(buf, rest...)

		return nil
	}, loom.AfterStreamed(producer))

	g, ctx := errgroup.WithContext(context.Background())
	g.Go(func() error { return d.RunTask(ctx, taskSpec(addr, "producer")) })
	g.Go(func() error { return d.RunTask(ctx, taskSpec(addr, "consumer")) })
	require.NoError(t, g.Wait())

	require.Equal(t, string(head)+string(tail), string(got))
}

// TestRunTaskSequentialExchange — обычное ребро: отправитель завершился и
// закоммитил, затем получатель читает целиком (данные больше одного chunk'а).
func TestRunTaskSequentialExchange(t *testing.T) {
	addr := startArtifactServer(t)

	payload := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1MiB — несколько chunk'ов
	var got []byte

	d := loom.New("itest")
	producer := d.Task("producer", func(_ context.Context, rt *loom.Runtime) error {
		out, err := rt.Output("data")
		if err != nil {
			return err
		}
		_, err = out.Write(payload)
		return err
	})
	d.Task("consumer", func(_ context.Context, rt *loom.Runtime) error {
		in, err := rt.Input("producer", "data")
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()

		got, err = io.ReadAll(in)
		return err
	}, loom.After(producer))

	ctx := context.Background()
	require.NoError(t, d.RunTask(ctx, taskSpec(addr, "producer")))
	require.NoError(t, d.RunTask(ctx, taskSpec(addr, "consumer")))

	require.Equal(t, payload, got)
}

// TestRunTaskFailedProducerAbortsOutputs — упавший отправитель abort'ит свои
// артефакты: читатель получает ErrAborted, а не пол-артефакта как валидный.
func TestRunTaskFailedProducerAbortsOutputs(t *testing.T) {
	addr := startArtifactServer(t)

	d := loom.New("itest")
	producer := d.Task("producer", func(_ context.Context, rt *loom.Runtime) error {
		out, err := rt.Output("data")
		if err != nil {
			return err
		}
		if _, err = out.Write([]byte("partial data")); err != nil {
			return err
		}
		return errors.New("boom")
	})
	d.Task("consumer", func(_ context.Context, rt *loom.Runtime) error {
		in, err := rt.Input("producer", "data")
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()

		_, err = io.ReadAll(in)
		return err
	}, loom.After(producer))

	ctx := context.Background()

	err := d.RunTask(ctx, taskSpec(addr, "producer"))
	require.ErrorContains(t, err, "boom")

	err = d.RunTask(ctx, taskSpec(addr, "consumer"))
	require.ErrorIs(t, err, streamstore.ErrAborted)
}

// TestRunTaskMissingArtifactAfterFinish — FinishAttempt (вызванный RunTask
// отправителя) разблокирует читателя несозданного артефакта: NOT_FOUND
// вместо вечного ожидания.
func TestRunTaskMissingArtifactAfterFinish(t *testing.T) {
	addr := startArtifactServer(t)

	d := loom.New("itest")
	producer := d.Task("producer", func(_ context.Context, _ *loom.Runtime) error {
		return nil // успех без единого артефакта
	})
	d.Task("consumer", func(_ context.Context, rt *loom.Runtime) error {
		in, err := rt.Input("producer", "data")
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()

		_, err = io.ReadAll(in)
		return err
	}, loom.After(producer))

	ctx := context.Background()

	require.NoError(t, d.RunTask(ctx, taskSpec(addr, "producer")))

	err := d.RunTask(ctx, taskSpec(addr, "consumer"))
	require.ErrorIs(t, err, streamstore.ErrNotFound)
}

// logCollector — стаб TaskLogService control plane'а.
type logCollector struct {
	serverpb.UnimplementedTaskLogServiceServer

	mu      sync.Mutex
	headers []*serverpb.TaskLogHeader
	entries []*serverpb.TaskLogEntry
}

func (c *logCollector) PushTaskLog(stream grpc.ClientStreamingServer[serverpb.PushTaskLogRequest, serverpb.PushTaskLogResponse]) error {
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&serverpb.PushTaskLogResponse{})
		}
		if err != nil {
			return err
		}

		c.mu.Lock()
		switch m := msg.GetMsg().(type) {
		case *serverpb.PushTaskLogRequest_Header:
			c.headers = append(c.headers, m.Header)
		case *serverpb.PushTaskLogRequest_Batch:
			c.entries = append(c.entries, m.Batch.GetEntries()...)
		}
		c.mu.Unlock()
	}
}

func (c *logCollector) logLines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lines []string
	for _, e := range c.entries {
		if e.GetSource() == serverpb.TaskLogSource_TASK_LOG_SOURCE_LOG {
			lines = append(lines, e.GetLine())
		}
	}

	return lines
}

// TestRunTaskLogStream — логи таска уезжают батчами на control plane:
// header идентифицирует attempt, строки Runtime.Log доставлены до
// завершения RunTask.
func TestRunTaskLogStream(t *testing.T) {
	artifactAddr := startArtifactServer(t)

	collector := &logCollector{}
	serverAddr := startGrpcServer(t, func(srv *grpc.Server) {
		serverpb.RegisterTaskLogServiceServer(srv, collector)
	})

	d := loom.New("itest")
	d.Task("worker", func(_ context.Context, rt *loom.Runtime) error {
		rt.Log().Info("hello from task")
		return nil
	})

	spec := taskSpec(artifactAddr, "worker")
	spec.ServerAddr = serverAddr
	require.NoError(t, d.RunTask(context.Background(), spec))

	collector.mu.Lock()
	require.Len(t, collector.headers, 1)
	require.Equal(t, "run-1", collector.headers[0].GetRunId())
	require.Equal(t, "worker", collector.headers[0].GetTask())
	require.EqualValues(t, 1, collector.headers[0].GetAttempt())
	collector.mu.Unlock()

	lines := collector.logLines()
	require.True(t, len(lines) >= 3, "want start/hello/success lines, got: %v", lines)

	var found bool
	for _, line := range lines {
		if strings.Contains(line, "hello from task") {
			found = true
			break
		}
	}
	require.True(t, found, "log line not delivered: %v", lines)
}
