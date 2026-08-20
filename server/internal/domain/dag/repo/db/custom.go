package db

import (
	"github.com/rendau/loom/server/internal/domain/dag/model"
)

var allowedSortFields = map[string]string{
	"name":        "name",
	"created_at":  "created_at",
	"modified_at": "modified_at",
}

func (r *Repo) getConditions(pars *model.ListReq) (map[string]any, map[string][]any) {
	conditions := make(map[string]any, 3)
	conditionExps := make(map[string][]any, 3)

	if pars == nil {
		return conditions, conditionExps
	}

	if pars.Paused != nil {
		conditions["paused"] = *pars.Paused
	}

	return conditions, conditionExps
}
