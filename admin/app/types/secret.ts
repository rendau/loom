// DTO секретов (зеркало api/proto/server_v1/secret.proto). Списки отдают
// только метаданные; значение — через getSecretValue (по кнопке).

import type { Scope } from '~/types/common'

export interface SecretMeta {
  name: string
  scope: Scope
  created_at: string
  modified_at?: string
}
