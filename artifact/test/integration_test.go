// Package test — интеграционные тесты распределённого режима SDK против
// настоящего gRPC artifact-сервера: каждый RunTask играет роль отдельного
// контейнера таска, сервер поднимается in-process на 127.0.0.1.
package test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	artifactpb "github.com/rendau/loom/api/artifact_v1"
	domain "github.com/rendau/loom/artifact/internal/domain/artifact"
	tasklogDomain "github.com/rendau/loom/artifact/internal/domain/tasklog"
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

// startArtifactServer поднимает artifact-сервер целиком: артефакты и логи
// тасков (отдельные streamstore-каталоги, как в проде).
func startArtifactServer(t *testing.T) string {
	t.Helper()

	svc, err := domain.New(t.TempDir())
	require.NoError(t, err)

	tasklogSvc, err := tasklogDomain.New(t.TempDir())
	require.NoError(t, err)

	return startGrpcServer(t, func(srv *grpc.Server) {
		artifactpb.RegisterArtifactServiceServer(srv, handler.NewArtifact(svc))
		artifactpb.RegisterTaskLogServiceServer(srv, handler.NewTaskLog(tasklogSvc))
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

// readTaskLog вычитывает лог попытки с artifact-сервера (без follow).
func readTaskLog(t *testing.T, addr, runId, task string, attempt int32) []*artifactpb.TaskLogEntry {
	t.Helper()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	stream, err := artifactpb.NewTaskLogServiceClient(conn).ReadTaskLog(context.Background(),
		&artifactpb.ReadTaskLogRequest{RunId: runId, Task: task, Attempt: attempt})
	require.NoError(t, err)

	var entries []*artifactpb.TaskLogEntry
	for {
		rep, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return entries
		}
		require.NoError(t, err)
		entries = append(entries, rep.GetEntries()...)
	}
}

// TestRunTaskLogStream — логи таска уезжают стримом на artifact-сервер:
// строки Runtime.Log подтверждены и читаемы к завершению RunTask.
func TestRunTaskLogStream(t *testing.T) {
	addr := startArtifactServer(t)

	d := loom.New("itest")
	d.Task("worker", func(_ context.Context, rt *loom.Runtime) error {
		rt.Log().Info("hello from task")
		return nil
	})

	require.NoError(t, d.RunTask(context.Background(), taskSpec(addr, "worker")))

	lines := lo.FilterMap(readTaskLog(t, addr, "run-1", "worker", 1),
		func(e *artifactpb.TaskLogEntry, _ int) (string, bool) {
			return e.GetLine(), e.GetSource() == artifactpb.TaskLogSource_TASK_LOG_SOURCE_LOG
		})
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

// ── рестарты artifact-сервера ───────────────────────────

// restartableServer — artifact-сервер, который можно «рестартнуть» на том же
// адресе: все соединения рвутся, домены пересоздаются над теми же
// каталогами — как рестарт пода с PVC.
type restartableServer struct {
	t       *testing.T
	addr    string
	dataDir string
	logDir  string
	srv     *grpc.Server
}

func startRestartableServer(t *testing.T) *restartableServer {
	t.Helper()

	rs := &restartableServer{t: t, dataDir: t.TempDir(), logDir: t.TempDir()}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	rs.addr = lis.Addr().String()

	rs.serve(lis)
	t.Cleanup(func() { rs.srv.Stop() })

	return rs
}

func (rs *restartableServer) serve(lis net.Listener) {
	svc, err := domain.New(rs.dataDir)
	require.NoError(rs.t, err)
	tasklogSvc, err := tasklogDomain.New(rs.logDir)
	require.NoError(rs.t, err)

	srv := grpc.NewServer()
	artifactpb.RegisterArtifactServiceServer(srv, handler.NewArtifact(svc))
	artifactpb.RegisterTaskLogServiceServer(srv, handler.NewTaskLog(tasklogSvc))

	go func() { _ = srv.Serve(lis) }()
	rs.srv = srv
}

func (rs *restartableServer) restart() {
	rs.srv.Stop()

	var lis net.Listener
	require.Eventually(rs.t, func() bool {
		var err error
		lis, err = net.Listen("tcp", rs.addr)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "rebind %s", rs.addr)

	rs.serve(lis)
}

// TestLogStreamSurvivesServerRestart — рестарт artifact-сервера посреди
// работы таска не теряет и не дублирует строки лога: sink переподключается
// и досылает неподтверждённый хвост, сервер дедуплицирует по seq.
func TestLogStreamSurvivesServerRestart(t *testing.T) {
	rs := startRestartableServer(t)

	const half = 50
	d := loom.New("itest")
	d.Task("worker", func(_ context.Context, rt *loom.Runtime) error {
		for i := range half {
			rt.Log().Info(fmt.Sprintf("line-%03d", i))
		}
		rs.restart()
		for i := half; i < 2*half; i++ {
			rt.Log().Info(fmt.Sprintf("line-%03d", i))
		}
		return nil
	})

	require.NoError(t, d.RunTask(context.Background(), taskSpec(rs.addr, "worker")))

	lines := lo.FilterMap(readTaskLog(t, rs.addr, "run-1", "worker", 1),
		func(e *artifactpb.TaskLogEntry, _ int) (string, bool) {
			return e.GetLine(), strings.Contains(e.GetLine(), "line-")
		})
	require.Len(t, lines, 2*half, "потери или дубли строк: %v", lines)
	for i, line := range lines {
		require.Contains(t, line, fmt.Sprintf("line-%03d", i), "строки не по порядку")
	}
}

// TestArtifactStreamsSurviveServerRestart — рестарт artifact-сервера посреди
// стримового обмена: писатель резюмит запись и досылает хвост, follow-
// читатель переоткрывается со своего offset'а; данные доходят целиком.
func TestArtifactStreamsSurviveServerRestart(t *testing.T) {
	rs := startRestartableServer(t)

	head := []byte("head-before-restart|")
	tail := []byte("tail-after-restart")

	headRead := make(chan struct{})
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

		<-headRead // читатель забрал голову — рвём обоих
		rs.restart()

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
		close(headRead)

		rest, err := io.ReadAll(in)
		if err != nil {
			return err
		}
		got = append(buf, rest...)

		return nil
	}, loom.AfterStreamed(producer))

	g, ctx := errgroup.WithContext(context.Background())
	g.Go(func() error { return d.RunTask(ctx, taskSpec(rs.addr, "producer")) })
	g.Go(func() error { return d.RunTask(ctx, taskSpec(rs.addr, "consumer")) })
	require.NoError(t, g.Wait())

	require.Equal(t, string(head)+string(tail), string(got))
}
