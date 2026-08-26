// DTO переменных (зеркало api/proto/server_v1/variable.proto). В отличие от
// секретов значения видны в списке.

import type { Scope } from '~/types/common'

export interface Variable {
  name: string
  value: string
  scope: Scope
  created_at: string
  modified_at?: string
}
