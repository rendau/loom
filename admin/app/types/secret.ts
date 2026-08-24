// DTO секретов (зеркало api/proto/server_v1/secret.proto). Списки отдают
// только метаданные; значение — через getSecretValue (по кнопке).

export interface SecretMeta {
  name: string
  dag_name: string // '' — глобальный скоуп
  created_at: string
  modified_at?: string
}
