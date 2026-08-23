package handler

import (
	"context"
	"errors"
	"io"

	"github.com/samber/lo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/rendau/loom/api/artifact_v1"
	tasklogDomain "github.com/rendau/loom/artifact/internal/domain/tasklog"
	tasklogModel "github.com/rendau/loom/artifact/internal/domain/tasklog/model"
)

type TaskLog struct {
	pb.UnsafeTaskLogServiceServer

	svc *tasklogDomain.Service
}

func NewTaskLog(svc *tasklogDomain.Service) *TaskLog {
	return &TaskLog{svc: svc}
}

// PushTaskLog — bidi-стрим приёма логов: header → ack с числом уже
// записанных строк, далее батчи с first_seq → ack после каждой записи.
// Закрытие стрима клиентом — нормальное завершение; commit лог-стрима
// делает control plane через FinishTaskLog.
func (h *TaskLog) PushTaskLog(stream pb.TaskLogService_PushTaskLogServer) error {
	first, err := stream.Recv()
	if err != nil {
		return encodeLogErr(err)
	}

	header := first.GetHeader()
	if header == nil {
		return status.Error(codes.InvalidArgument, "first message must be header")
	}

	key := tasklogModel.AttemptKey{
		RunId:   header.GetRunId(),
		Task:    header.GetTask(),
		Attempt: header.GetAttempt(),
	}

	nextSeq, err := h.svc.NextSeq(key)
	if err != nil {
		return encodeLogErr(err)
	}
	if err = stream.Send(&pb.PushTaskLogAck{NextSeq: nextSeq}); err != nil {
		return err
	}

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return encodeLogErr(err)
		}

		batch := msg.GetBatch()
		if batch == nil {
			return status.Error(codes.InvalidArgument, "unexpected message")
		}

		nextSeq, err = h.svc.Append(key, batch.GetFirstSeq(), lo.Map(batch.GetEntries(), decodeTaskLogEntry))
		if err != nil {
			return encodeLogErr(err)
		}
		if err = stream.Send(&pb.PushTaskLogAck{NextSeq: nextSeq}); err != nil {
			return err
		}
	}
}

// ReadTaskLog отдаёт лог попытки с позиции after_seq; при follow=true стрим
// живёт до финализации лога (live-логи).
func (h *TaskLog) ReadTaskLog(req *pb.ReadTaskLogRequest, stream pb.TaskLogService_ReadTaskLogServer) error {
	key := tasklogModel.AttemptKey{
		RunId:   req.GetRunId(),
		Task:    req.GetTask(),
		Attempt: req.GetAttempt(),
	}

	err := h.svc.Read(stream.Context(), key, req.GetAfterSeq(), req.GetFollow(), func(entries []tasklogModel.Entry) error {
		return stream.Send(&pb.ReadTaskLogResponse{Entries: lo.Map(entries, encodeTaskLogEntry)})
	})
	if err != nil {
		return encodeLogErr(err)
	}
	return nil
}

// FinishTaskLog — финализация лога попытки control plane'ом: финальные
// строки + commit + маркер завершённой попытки. Идемпотентен.
func (h *TaskLog) FinishTaskLog(ctx context.Context, req *pb.FinishTaskLogRequest) (*pb.FinishTaskLogResponse, error) {
	key := tasklogModel.AttemptKey{
		RunId:   req.GetRunId(),
		Task:    req.GetTask(),
		Attempt: req.GetAttempt(),
	}

	if err := h.svc.Finish(key, lo.Map(req.GetFinal(), decodeTaskLogEntry)); err != nil {
		return nil, encodeLogErr(err)
	}
	return &pb.FinishTaskLogResponse{}, nil
}

func (h *TaskLog) DeleteRunTaskLogs(ctx context.Context, req *pb.DeleteRunTaskLogsRequest) (*pb.DeleteRunTaskLogsResponse, error) {
	if err := h.svc.DeleteRun(req.GetRunId()); err != nil {
		return nil, encodeLogErr(err)
	}
	return &pb.DeleteRunTaskLogsResponse{}, nil
}

// encodeLogErr — маппинг ошибок tasklog-домена в grpc-статусы: seq-гап —
// InvalidArgument (нарушение протокола доставки), остальное — общий маппинг
// ошибок streamstore.
func encodeLogErr(err error) error {
	if errors.Is(err, tasklogDomain.ErrSeqGap) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return encodeErr(err)
}

func decodeTaskLogEntry(e *pb.TaskLogEntry, _ int) tasklogModel.Entry {
	return tasklogModel.Entry{
		TsUnixMs: e.GetTsUnixMs(),
		Source:   decodeTaskLogSource(e.GetSource()),
		Line:     e.GetLine(),
	}
}

func encodeTaskLogEntry(e tasklogModel.Entry, _ int) *pb.TaskLogEntry {
	return &pb.TaskLogEntry{
		TsUnixMs: e.TsUnixMs,
		Source:   encodeTaskLogSource(e.Source),
		Line:     e.Line,
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
