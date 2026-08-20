package model

type AttemptUpsert struct {
	PKRunId   string
	PKTask    string
	PKAttempt int32

	Status *string
}

func (m *AttemptUpsert) CreateColumnMap() map[string]any {
	result := map[string]any{
		"run_id":  m.PKRunId,
		"task":    m.PKTask,
		"attempt": m.PKAttempt,
	}
	if m.Status != nil {
		result["status"] = *m.Status
	}
	return result
}

func (m *AttemptUpsert) UpdateColumnMap() map[string]any {
	result := m.CreateColumnMap()
	delete(result, "run_id")
	delete(result, "task")
	delete(result, "attempt")
	return result
}

func (m *AttemptUpsert) PKColumnMap() map[string]any {
	return map[string]any{"run_id": m.PKRunId, "task": m.PKTask, "attempt": m.PKAttempt}
}

func (m *AttemptUpsert) ReturningColumnMap() map[string]any {
	return map[string]any{}
}
