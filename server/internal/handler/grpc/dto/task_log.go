package dto

import (
	"github.com/samber/lo"

	pb "github.com/rendau/loom/api/server_v1"
	domainModel "github.com/rendau/loom/server/internal/domain/tasklog/model"
)

// domain → proto

func EncodeTaskLogEntry(v domainModel.Entry, _ int) *pb.TaskLogEntry {
	return &pb.TaskLogEntry{
		TsUnixMs: v.TsUnixMs,
		Source:   encodeTaskLogSource(v.Source),
		Line:     v.Line,
	}
}

func EncodeTaskLogEntries(entries []domainModel.Entry) []*pb.TaskLogEntry {
	return lo.Map(entries, EncodeTaskLogEntry)
}

func encodeTaskLogSource(v string) pb.TaskLogSource {
	switch v {
	case domainModel.SourceLog:
		return pb.TaskLogSource_TASK_LOG_SOURCE_LOG
	case domainModel.SourceStdout:
		return pb.TaskLogSource_TASK_LOG_SOURCE_STDOUT
	case domainModel.SourceStderr:
		return pb.TaskLogSource_TASK_LOG_SOURCE_STDERR
	case domainModel.SourceServer:
		return pb.TaskLogSource_TASK_LOG_SOURCE_SERVER
	default:
		return pb.TaskLogSource_TASK_LOG_SOURCE_UNSPECIFIED
	}
}
