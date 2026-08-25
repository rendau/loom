// DTO настроек инсталляции (зеркало api/proto/server_v1/setting.proto).
// Скоуп как у переменных: dag_name '' — глобальный, имя дага — уточнение,
// перекрывающее глобальное при резолве.

export interface Setting {
  name: string
  value: string
  dag_name: string // '' — глобальный скоуп
  modified_at?: string
}

// Метаданные известных настроек — зеркало реестра сервера
// (server/internal/domain/setting/model.Defs): имена и типы валидирует
// сервер, здесь — подписи и подсказки для UI.
export interface SettingDef {
  name: string
  kind: 'duration' | 'int'
  label: string
  hint: string
  perDag: boolean
  default: string
}

export const settingDefs: SettingDef[] = [
  {
    name: 'run_ttl',
    kind: 'duration',
    label: 'TTL завершённых ранов',
    hint: 'ран старше срока удаляется целиком: артефакты, логи, записи БД; 0 — по времени не чистить',
    perDag: true,
    default: '720h',
  },
  {
    name: 'run_keep_last',
    kind: 'int',
    label: 'Хранить последних ранов',
    hint: 'сверх N последних завершённые раны дага удаляются; 0 — не ограничивать. Работает вместе с TTL: ран удаляется, если нарушает любой из лимитов',
    perDag: true,
    default: '0',
  },
  {
    name: 'k8s_job_ttl',
    kind: 'duration',
    label: 'TTL k8s Job попытки',
    hint: 'завершённый Job удаляется из кластера через этот срок (логи и артефакты уже сохранены); 0 — не удалять',
    perDag: true,
    default: '1h',
  },
  {
    name: 'dag_reg_ttl',
    kind: 'duration',
    label: 'TTL истории регистраций',
    hint: 'завершённые записи истории регистраций дагов старше срока удаляются; 0 — хранить вечно. Только глобальная',
    perDag: false,
    default: '720h',
  },
]

export const perDagSettingDefs = settingDefs.filter(d => d.perDag)

// Формат значения по типу — для валидации на клиенте до запроса.
export function settingValueValid(def: SettingDef, value: string): boolean {
  if (def.kind === 'int')
    return /^\d+$/.test(value)
  // duration в Go-нотации: 0 или комбинация 720h/90m/30s
  return /^\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h)(?:\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h))*$|^0$/.test(value)
}
