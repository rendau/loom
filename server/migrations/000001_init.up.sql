-- Схема control plane. Пока проект не задеплоен, вся схема живёт в этом
-- едином init-файле (см. правило миграций в CLAUDE.md).

create table dag (
    name         text primary key,
    image        text        not null, -- url образа, как задан при регистрации (тег)
    image_digest text        not null, -- закреплённый digest (repo@sha256:...)
    schedule     text        not null default '',
    paused       boolean     not null default false,
    auto_update  boolean     not null default false, -- poll-синк новой версии образа (решение №30)
    manifest     jsonb       not null, -- манифест `describe` как есть
    next_run_at  timestamptz,          -- следующий запуск по cron; null — без расписания
    created_at   timestamptz not null default now(),
    modified_at  timestamptz
);

create table run (
    id           text primary key,
    dag_name     text        not null,
    image        text        not null, -- образ для запуска подов: repo@digest
    image_digest text        not null,
    trigger      text        not null, -- manual | schedule | backfill
    status       text        not null, -- running | success | failed
    manifest     jsonb       not null, -- снапшот манифеста на момент триггера
    params       jsonb,                -- параметры рана (аналог dagrun.conf); null — без параметров
    -- «дата данных»: тик расписания у cron/backfill-рана, момент триггера у ручного
    logical_date timestamptz not null,
    created_at   timestamptz not null default now(),
    finished_at  timestamptz
);

create index run_dag_name_idx on run (dag_name, created_at desc);
create index run_status_idx on run (status) where status = 'running';
-- retention: выборка завершённых ранов с истёкшим TTL
create index run_finished_idx on run (finished_at) where finished_at is not null;

-- Пулы слотов параллелизма (решение №26): таски конкурируют за слоты своего
-- пула; удаления нет — на пул могут ссылаться манифесты ранов.
create table pool (
    name        text primary key,
    slots       int  not null,
    created_at  timestamptz not null default now(),
    modified_at timestamptz
);

insert into pool (name, slots) values ('default', 64);

create table task_instance (
    run_id      text not null references run (id) on delete cascade,
    task        text not null,
    -- pending | queued | starting | running | up_for_retry | success | failed | upstream_failed
    status      text not null,
    attempt     int  not null default 0, -- номер текущей (последней) попытки
    -- пул и приоритет из манифеста: денормализация для claim-запроса очереди
    pool        text not null default 'default',
    priority    int  not null default 0,
    queued_at   timestamptz,
    started_at  timestamptz,
    retry_at    timestamptz,             -- когда вернуть up_for_retry в очередь
    finished_at timestamptz,
    primary key (run_id, task)
);

-- очередь планировщика: выборка queued-тасков через FOR UPDATE SKIP LOCKED,
-- приоритетные первыми
create index task_instance_queued_idx on task_instance (priority desc, queued_at) where status = 'queued';
-- подсчёт занятости пулов при claim'е
create index task_instance_active_pool_idx on task_instance (pool) where status in ('starting', 'running');
-- возврат ретраев в очередь по расписанию backoff'а
create index task_instance_retry_idx on task_instance (retry_at) where status = 'up_for_retry';

-- Секреты для env-инъекции в поды (решение №27): значение шифруется
-- AES-256-GCM при заданном SECRET_KEY; наружу через API не отдаётся.
create table secret (
    name        text  primary key,
    value       bytea not null,
    created_at  timestamptz not null default now(),
    modified_at timestamptz
);

-- Мелкие значения тасков (аналог XCom, решение №25): скоуп (run, task, key),
-- ретрай перезаписывает значение (upsert).
create table run_value (
    run_id      text  not null references run (id) on delete cascade,
    task        text  not null,
    key         text  not null,
    value       jsonb not null,
    modified_at timestamptz not null default now(),
    primary key (run_id, task, key)
);

create table attempt (
    run_id      text not null,
    task        text not null,
    attempt     int  not null,
    status      text not null, -- starting | running | success | failed
    created_at  timestamptz not null default now(),
    started_at  timestamptz,
    finished_at timestamptz,
    exit_code   int,
    exit_reason text not null default '',
    primary key (run_id, task, attempt),
    foreign key (run_id, task) references task_instance (run_id, task) on delete cascade
);

-- зомби-детект: выборка незавершённых попыток старше grace-периода
create index attempt_active_idx on attempt (created_at)
    where status in ('starting', 'running');
