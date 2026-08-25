// Package artifactcli — gRPC-клиент artifact-сервера для control plane:
// финализация попыток и их лог-стримов, прокси-чтение логов для админки,
// удаление данных рана (retention).
package artifactcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/samber/lo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	pb "github.com/rendau/loom/api/artifact_v1"
	artifactModel "github.com/rendau/loom/server/internal/domain/artifact/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	tasklogModel "github.com/rendau/loom/server/internal/domain/tasklog/model"
	"github.com/rendau/loom/server/internal/errs"
)

const (
	readReconnectMinDelay = 200 * time.Millisecond
	readReconnectMaxDelay = 3 * time.Second
)

type Service struct {
	conn      *grpc.ClientConn
	client    pb.ArtifactServiceClient
	logClient pb.TaskLogServiceClient
}

func New(addr string) (*Service, error) {
	conn, err := grpc.NewClient(addr, dialOpts()...)
	if err != nil {
		return nil, fmt.Errorf("grpc.NewClient: %w", err)
	}

	return &Service{
		conn:      conn,
		client:    pb.NewArtifactServiceClient(conn),
		logClient: pb.NewTaskLogServiceClient(conn),
	}, nil
}

// dialOpts — быстрое обнаружение мёртвого соединения (keepalive), быстрый
// реконнект (агрессивный backoff) и ретрай unary-вызовов на UNAVAILABLE:
// рестарт artifact-сервера не должен ронять финализацию попыток.
func dialOpts() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  200 * time.Millisecond,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   3 * time.Second,
			},
			MinConnectTimeout: 3 * time.Second,
		}),
		grpc.WithDefaultServiceConfig(`{
			"methodConfig": [{
				"name": [{}],
				"retryPolicy": {
					"maxAttempts": 5,
					"initialBackoff": "0.2s",
					"maxBackoff": "3s",
					"backoffMultiplier": 2,
					"retryableStatusCodes": ["UNAVAILABLE"]
				}
			}]
		}`),
	}
}

func (s *Service) FinishAttempt(ctx context.Context, ref runModel.AttemptRef) error {
	_, err := s.client.FinishAttempt(ctx, &pb.FinishAttemptRequest{
		RunId:   ref.RunId,
		Task:    ref.Task,
		Attempt: ref.Attempt,
	})
	if err != nil {
		return fmt.Errorf("FinishAttempt: %w", err)
	}
	return nil
}

func (s *Service) DeleteRunArtifacts(ctx context.Context, runId string) error {
	_, err := s.client.DeleteRunArtifacts(ctx, &pb.DeleteRunArtifactsRequest{RunId: runId})
	if err != nil {
		return fmt.Errorf("DeleteRunArtifacts: %w", err)
	}
	return nil
}

// ListRunArtifacts — метаданные всех артефактов рана (для админки).
func (s *Service) ListRunArtifacts(ctx context.Context, runId string) ([]artifactModel.Info, error) {
	rep, err := s.client.ListRunArtifacts(ctx, &pb.ListRunArtifactsRequest{RunId: runId})
	if err != nil {
		return nil, fmt.Errorf("ListRunArtifacts: %w", decodeArtifactErr(err))
	}

	return lo.Map(rep.GetArtifacts(), func(v *pb.ArtifactInfo, _ int) artifactModel.Info {
		info := artifactModel.Info{
			Task:    v.GetRef().GetTask(),
			Attempt: v.GetRef().GetAttempt(),
			Name:    v.GetRef().GetName(),
			State:   decodeArtifactState(v.GetState()),
			Size:    v.GetSize(),
		}
		if ms := v.GetModifiedUnixMs(); ms > 0 {
			info.Modified = time.UnixMilli(ms)
		}
		return info
	}), nil
}

// GetStorageStats — занятость хранилища artifact-сервера.
func (s *Service) GetStorageStats(ctx context.Context) (artifactModel.StorageStats, error) {
	rep, err := s.client.GetStorageStats(ctx, &pb.StorageStatsRequest{})
	if err != nil {
		return artifactModel.StorageStats{}, fmt.Errorf("GetStorageStats: %w", err)
	}

	decode := func(v *pb.DirStats) artifactModel.StorageDir {
		return artifactModel.StorageDir{
			UsedBytes:  v.GetUsedBytes(),
			TotalBytes: v.GetTotalBytes(),
			FreeBytes:  v.GetFreeBytes(),
		}
	}
	return artifactModel.StorageStats{Data: decode(rep.GetData()), Logs: decode(rep.GetLogs())}, nil
}

// ReadArtifactTo стримит содержимое артефакта (с offset) в w; limit > 0 —
// не больше limit байт (превью). Обрыв соединения переживается реконнектом
// с продолжением с последнего отданного байта.
func (s *Service) ReadArtifactTo(ctx context.Context, ref artifactModel.Ref, offset, limit int64, w io.Writer) error {
	written := int64(0)
	delay := readReconnectMinDelay

	for {
		err := s.readArtifactOnce(ctx, ref, offset+written, limit-written, limit > 0, w, &written)
		if err == nil {
			return nil
		}
		if fnErr, ok := errors.AsType[errReadFn](err); ok {
			return fnErr.err
		}
		if status.Code(err) != codes.Unavailable {
			return decodeArtifactErr(err)
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		delay = min(delay*2, readReconnectMaxDelay)
	}
}

func (s *Service) readArtifactOnce(ctx context.Context, ref artifactModel.Ref, offset, remain int64, limited bool, w io.Writer, written *int64) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := s.client.ReadArtifact(ctx, &pb.ReadArtifactRequest{
		Ref: &pb.ArtifactRef{RunId: ref.RunId, Task: ref.Task, Attempt: ref.Attempt, Name: ref.Name},
		// follow не нужен: админка читает текущее содержимое, включая
		// writing-хвост, и не ждёт продолжения записи
		Offset: offset,
	})
	if err != nil {
		return err
	}

	for {
		rep, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		chunk := rep.GetChunk()
		if limited && int64(len(chunk)) > remain {
			chunk = chunk[:remain]
		}
		if len(chunk) > 0 {
			if _, wErr := w.Write(chunk); wErr != nil {
				return errReadFn{err: wErr}
			}
			*written += int64(len(chunk))
			remain -= int64(len(chunk))
		}
		if limited && remain <= 0 {
			return nil
		}
	}
}

func decodeArtifactErr(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("%w: %v", errs.ArtifactNotFound, err)
	case codes.Aborted:
		return fmt.Errorf("%w: %v", errs.ArtifactAborted, err)
	case codes.InvalidArgument:
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: err.Error()}
	default:
		return err
	}
}

func decodeArtifactState(v pb.ArtifactState) string {
	switch v {
	case pb.ArtifactState_ARTIFACT_STATE_WRITING:
		return "writing"
	case pb.ArtifactState_ARTIFACT_STATE_COMMITTED:
		return "committed"
	case pb.ArtifactState_ARTIFACT_STATE_ABORTED:
		return "aborted"
	default:
		return ""
	}
}

// Finish финализирует лог попытки на artifact-сервере: дописывает финальные
// строки (исход попытки) и коммитит стрим. Идемпотентен.
func (s *Service) Finish(ctx context.Context, key tasklogModel.AttemptKey, final []tasklogModel.Entry) error {
	_, err := s.logClient.FinishTaskLog(ctx, &pb.FinishTaskLogRequest{
		RunId:   key.RunId,
		Task:    key.Task,
		Attempt: key.Attempt,
		Final:   lo.Map(final, encodeTaskLogEntry),
	})
	if err != nil {
		return fmt.Errorf("FinishTaskLog: %w", err)
	}
	return nil
}

// DeleteRunTaskLogs удаляет логи рана (retention).
func (s *Service) DeleteRunTaskLogs(ctx context.Context, runId string) error {
	_, err := s.logClient.DeleteRunTaskLogs(ctx, &pb.DeleteRunTaskLogsRequest{RunId: runId})
	if err != nil {
		return fmt.Errorf("DeleteRunTaskLogs: %w", err)
	}
	return nil
}

// errReadFn — маркер ошибки из fn читателя: не сетевая, реконнект не нужен.
type errReadFn struct{ err error }

func (e errReadFn) Error() string { return e.err.Error() }

// ReadTaskLog читает лог попытки с artifact-сервера начиная со строки
// afterSeq+1, отдавая строки батчами в fn; при follow=true — до финализации
// лога. Обрыв соединения (рестарт artifact-сервера) переживается
// реконнектом с продолжением с последней отданной строки (after_seq) —
// читатель не замечает рестарта и не получает дублей.
func (s *Service) ReadTaskLog(ctx context.Context, key tasklogModel.AttemptKey, afterSeq int64, follow bool, fn func([]tasklogModel.Entry) error) error {
	seq := afterSeq
	delay := readReconnectMinDelay

	for {
		err := s.readTaskLogOnce(ctx, key, &seq, follow, fn)
		if err == nil {
			return nil
		}
		if fnErr, ok := errors.AsType[errReadFn](err); ok {
			return fnErr.err
		}
		if status.Code(err) != codes.Unavailable {
			return decodeLogErr(err)
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		delay = min(delay*2, readReconnectMaxDelay)
	}
}

func (s *Service) readTaskLogOnce(ctx context.Context, key tasklogModel.AttemptKey, seq *int64, follow bool, fn func([]tasklogModel.Entry) error) error {
	stream, err := s.logClient.ReadTaskLog(ctx, &pb.ReadTaskLogRequest{
		RunId:    key.RunId,
		Task:     key.Task,
		Attempt:  key.Attempt,
		AfterSeq: *seq,
		Follow:   follow,
	})
	if err != nil {
		return err
	}

	for {
		rep, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		entries := lo.Map(rep.GetEntries(), decodeTaskLogEntry)
		if len(entries) == 0 {
			continue
		}
		if fnErr := fn(entries); fnErr != nil {
			return errReadFn{err: fnErr}
		}
		*seq += int64(len(entries))
	}
}

// decodeLogErr маппит статусы artifact-сервера в доменные ошибки control
// plane: 404/ABORTED лога должны доходить до админки теми же кодами, что и
// раньше.
func decodeLogErr(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("%w: %v", errs.ObjectNotFound, err)
	case codes.Aborted:
		return fmt.Errorf("%w: %v", errs.AttemptLogAborted, err)
	default:
		return err
	}
}

func encodeTaskLogEntry(e tasklogModel.Entry, _ int) *pb.TaskLogEntry {
	return &pb.TaskLogEntry{TsUnixMs: e.TsUnixMs, Source: encodeTaskLogSource(e.Source), Line: e.Line}
}

func decodeTaskLogEntry(e *pb.TaskLogEntry, _ int) tasklogModel.Entry {
	return tasklogModel.Entry{TsUnixMs: e.GetTsUnixMs(), Source: decodeTaskLogSource(e.GetSource()), Line: e.GetLine()}
}

func encodeTaskLogSource(v string) pb.TaskLogSource {
	switch v {
	case tasklogModel.SourceLog:
		return pb.TaskLogSource_TASK_LOG_SOURCE_LOG
	case tasklogModel.SourceStdout:
		return pb.TaskLogSource_TASK_LOG_SOURCE_STDOUT
	case tasklogModel.SourceStderr:
		return pb.TaskLogSource_TASK_LOG_SOURCE_STDERR
	case tasklogModel.SourceServer:
		return pb.TaskLogSource_TASK_LOG_SOURCE_SERVER
	default:
		return pb.TaskLogSource_TASK_LOG_SOURCE_UNSPECIFIED
	}
}

func decodeTaskLogSource(v pb.TaskLogSource) string {
	switch v {
	case pb.TaskLogSource_TASK_LOG_SOURCE_LOG:
		return tasklogModel.SourceLog
	case pb.TaskLogSource_TASK_LOG_SOURCE_STDOUT:
		return tasklogModel.SourceStdout
	case pb.TaskLogSource_TASK_LOG_SOURCE_STDERR:
		return tasklogModel.SourceStderr
	case pb.TaskLogSource_TASK_LOG_SOURCE_SERVER:
		return tasklogModel.SourceServer
	default:
		return ""
	}
}

func (s *Service) Close() error {
	return s.conn.Close()
}
