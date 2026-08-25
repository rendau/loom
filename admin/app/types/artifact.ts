// DTO артефактов (зеркало api/proto/server_v1/artifact.proto). int64 в
// protojson приходят строками.

export interface ArtifactMain {
  task: string
  attempt: number
  name: string
  state: 'writing' | 'committed' | 'aborted'
  size: string
  modified_at?: string
}

export interface StorageDirStats {
  used_bytes: string
  total_bytes: string
  free_bytes: string
}

export interface StorageStats {
  data: StorageDirStats
  logs: StorageDirStats
}
