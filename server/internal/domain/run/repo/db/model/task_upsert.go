package model

type TaskUpsert struct {
	PKRunId string
	PKTask  string

	Status   *string
	Attempt  *int32
	Pool     *string
	Priority *int32
}

func (m *TaskUpsert) CreateColumnMap() map[string]any {
	result := map[string]any{"run_id": m.PKRunId, "task": m.PKTask}
	if m.Status != nil {
		result["status"] = *m.Status
	}
	if m.Attempt != nil {
		result["attempt"] = *m.Attempt
	}
	if m.Pool != nil {
		result["pool"] = *m.Pool
	}
	if m.Priority != nil {
		result["priority"] = *m.Priority
	}
	return result
}

func (m *TaskUpsert) UpdateColumnMap() map[string]any {
	result := m.CreateColumnMap()
	delete(result, "run_id")
	delete(result, "task")
	return result
}

func (m *TaskUpsert) PKColumnMap() map[string]any {
	return map[string]any{"run_id": m.PKRunId, "task": m.PKTask}
}

func (m *TaskUpsert) ReturningColumnMap() map[string]any {
	return map[string]any{}
}
