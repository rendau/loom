// DTO пулов (зеркало api/proto/server_v1/pool.proto).

export interface Pool {
  name: string
  slots: number
  created_at: string
  modified_at?: string
}
