import type { DagRef } from '~/types/common'

// DTO пользователей и сессий (зеркало api/proto/server_v1/auth.proto и
// user.proto).

export type UserRole = 'admin' | 'user'

export interface User {
  id: string
  username: string
  role: UserRole
  // Назначенные даги и проекты: их пользователь может менять. У admin —
  // пусто (доступно всё).
  dags?: DagRef[]
  projects?: string[]
  created_at: string
  modified_at?: string
}

export interface LoginRep {
  token: string
  user: User
  expires_at: string
}
