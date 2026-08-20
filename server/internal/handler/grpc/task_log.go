package handler

import (
	"errors"
	"io"

	"github.com/samber/lo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/rendau/loom/api/server_v1"
	tasklogModel "github.com/rendau/loom/server/internal/domain/tasklog/model"
	"github.com/rendau/loom/server/internal/handler/grpc/dto"
	tasklogUsc "github.com/rendau/loom/server/internal/usecase/tasklog"
)

type TaskLog struct {
	pb.UnsafeTaskLogServiceServer

	usecase *tasklogUsc.Usecase
}

func NewTaskLog(uc *tasklogUsc.Usecase) *TaskLog {
	return &TaskLog{usecase: uc}
}

// PushTaskLog — приём лог-стрима от SDK: первое сообщение — header
// (идентификация попытки), далее батчи строк. Закрытие стрима клиентом —
// нормальное завершение; commit лог-стрима делает планировщик при
// финализации попытки, не приёмник.
func (h *TaskLog) PushTaskLog(stream pb.TaskLogService_PushTaskLogServer) error {
	first, err := stream.Recv()
	if err != nil {
		return encodeErr(err)
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
	if err = h.usecase.ValidateAttempt(stream.Context(), key); err != nil {
		return encodeErr(err)
	}

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&pb.PushTaskLogResponse{})
		}
		if err != nil {
			return encodeErr(err)
		}

		batch := msg.GetBatch()
		if batch == nil {
			return status.Error(codes.InvalidArgument, "unexpected message")
		}

		if err = h.usecase.Append(key, lo.Map(batch.GetEntries(), dto.DecodeTaskLogEntry)); err != nil {
			return encodeErr(err)
		}
	}
}

// ReadTaskLog отдаёт лог попытки; при follow=true стрим живёт до завершения
// попытки (live-логи).
func (h *TaskLog) ReadTaskLog(req *pb.ReadTaskLogRequest, stream pb.TaskLogService_ReadTaskLogServer) error {
	key := tasklogModel.AttemptKey{
		RunId:   req.GetRunId(),
		Task:    req.GetTask(),
		Attempt: req.GetAttempt(),
	}

	err := h.usecase.Read(stream.Context(), key, req.GetFollow(), func(entries []tasklogModel.Entry) error {
		return stream.Send(&pb.ReadTaskLogResponse{Entries: dto.EncodeTaskLogEntries(entries)})
	})
	if err != nil {
		return encodeErr(err)
	}
	return nil
}
