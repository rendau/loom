// DTO секретов (зеркало api/proto/server_v1/secret.proto). API write-only:
// значения наружу не отдаются.

export interface SecretMeta {
  name: string
  created_at: string
  modified_at?: string
}
