package dto

import (
	commonPb "github.com/rendau/loom/api/common"
	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
)

func DecodeListParams(v *commonPb.ListParamsSt) commonModel.ListParams {
	if v == nil {
		return commonModel.ListParams{}
	}
	return commonModel.ListParams{
		Page:           v.Page,
		PageSize:       v.PageSize,
		WithTotalCount: v.WithTotalCount,
		OnlyCount:      v.OnlyCount,
		Sort:           v.Sort,
	}
}
