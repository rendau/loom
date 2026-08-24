// DTO переменных (зеркало api/proto/server_v1/variable.proto). В отличие от
// секретов значения видны в списке.

export interface Variable {
  name: string
  value: string
  dag_name: string // '' — глобальный скоуп
  created_at: string
  modified_at?: string
}
