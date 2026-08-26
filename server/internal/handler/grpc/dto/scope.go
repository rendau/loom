package dto

import (
	commonPb "github.com/rendau/loom/api/common"
	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
)

// EncodeScope — скоуп значения (переменной, секрета, настройки) в proto.
func EncodeScope(v commonModel.Scope) *commonPb.ScopeSt {
	return &commonPb.ScopeSt{Project: v.Project, Dag: v.Dag}
}

// DecodeScope — скоуп из proto; nil — глобальный.
func DecodeScope(v *commonPb.ScopeSt) commonModel.Scope {
	if v == nil {
		return commonModel.GlobalScope()
	}
	return commonModel.Scope{Project: v.GetProject(), Dag: v.GetDag()}
}

// DecodeScopeFilter — необязательный фильтр по скоупу: nil — все скоупы.
func DecodeScopeFilter(v *commonPb.ScopeSt) *commonModel.Scope {
	if v == nil {
		return nil
	}
	return new(DecodeScope(v))
}
