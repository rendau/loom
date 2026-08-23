package handler

import (
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

// ReadTaskLog отдаёт лог попытки (прокси с artifact-сервера); при
// follow=true стрим живёт до завершения попытки (live-логи).
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
