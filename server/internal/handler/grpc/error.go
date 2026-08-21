package handler

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonPb "github.com/rendau/loom/api/common"
	"github.com/rendau/loom/server/internal/errs"
)

// encodeErr маппит доменные ошибки (errs.Err / errs.ErrFull) в grpc-статусы
// с details common.ErrorRep — gateway отдаёт их телом HTTP-ответа.
func encodeErr(err error) error {
	if err == nil {
		return nil
	}

	errCode := errs.ServiceNA
	desc := ""
	var fields map[string]string

	if full, ok := errs.AsErrFull(err); ok {
		if e, eOk := errs.AsErr(full.Err); eOk {
			errCode = e
		}
		desc = full.Desc
		fields = full.Fields
	} else if e, ok := errs.AsErr(err); ok {
		errCode = e
	} else {
		if _, isStatus := status.FromError(err); isStatus {
			return err
		}
		return status.Error(codes.Internal, err.Error())
	}

	var code codes.Code
	switch errCode {
	case errs.DagNotFound, errs.RunNotFound, errs.TaskNotFound, errs.AttemptNotFound,
		errs.ValueNotFound, errs.PoolNotFound, errs.SecretNotFound, errs.ObjectNotFound:
		code = codes.NotFound
	case errs.AttemptLogAborted:
		code = codes.Aborted
	case errs.LogAlreadyPushed, errs.RunNotFinished, errs.TaskNotRetryable, errs.AttemptOutdated:
		code = codes.FailedPrecondition
	default:
		code = codes.InvalidArgument
	}

	st := status.New(code, errCode.Error())
	if detailed, dErr := st.WithDetails(&commonPb.ErrorRep{
		Code:    errCode.Error(),
		Message: desc,
		Fields:  fields,
	}); dErr == nil {
		st = detailed
	}
	return st.Err()
}
