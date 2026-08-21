package dto

import (
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	domainModel "github.com/rendau/loom/server/internal/domain/run/model"
)

// domain → proto

func EncodeTaskValueMain(v *domainModel.TaskValue, _ int) *pb.TaskValueMain {
	if v == nil {
		return nil
	}
	return &pb.TaskValueMain{
		Task:       v.Task,
		Key:        v.Key,
		Value:      EncodeTaskValueJSON(v.Value),
		ModifiedAt: timestamppb.New(v.ModifiedAt),
	}
}

// EncodeTaskValueJSON конвертирует raw JSON из БД в protobuf Value; значение
// валидировалось при пуше, битое — отдаём null, не валим выдачу.
func EncodeTaskValueJSON(raw []byte) *structpb.Value {
	var v structpb.Value
	if err := v.UnmarshalJSON(raw); err != nil {
		return structpb.NewNullValue()
	}
	return &v
}
