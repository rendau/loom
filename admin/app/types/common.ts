// Общие типы gateway API (grpc-gateway, protojson: snake_case,
// int64 приходят строками, timestamp — RFC3339-строками).

// type-алиас (не interface): алиасу TS даёт неявную индексную сигнатуру,
// и он проходит в QueryParams api-клиента без кастов
export type ListParams = {
  page?: number
  page_size?: number
  with_total_count?: boolean
  only_count?: boolean
  sort?: string[]
}

export interface PaginationInfo {
  page: string
  page_size: string
  total_count: string
}

export interface ErrorRep {
  code: string
  message: string
  fields?: Record<string, string>
}
