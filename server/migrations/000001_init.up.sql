-- Схема control plane. Пока проект не задеплоен, вся схема живёт в этом
-- едином init-файле (см. правило миграций в CLAUDE.md).

create table dag (
    name         text primary key,
    image        text        not null, -- url образа, как задан при регистрации (тег)
    image_digest text        not null, -- закреплённый digest (repo@sha256:...)
    schedule     text        not null default '',
    paused       boolean     not null default false,
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
    trigger      text        not null, -- manual | schedule
    status       text        not null, -- running | success | failed
    manifest     jsonb       not null, -- снапшот манифеста на момент триггера
    created_at   timestamptz not null default now(),
    finished_at  timestamptz
);

create index run_dag_name_idx on run (dag_name, created_at desc);
create index run_status_idx on run (status) where status = 'running';
-- retention: выборка завершённых ранов с истёкшим TTL
create index run_finished_idx on run (finished_at) where finished_at is not null;

create table task_instance (
    run_id      text not null references run (id) on delete cascade,
    task        text not null,
    -- pending | queued | starting | running | up_for_retry | success | failed | upstream_failed
    status      text not null,
    attempt     int  not null default 0, -- номер текущей (последней) попытки
    queued_at   timestamptz,
    started_at  timestamptz,
    retry_at    timestamptz,             -- когда вернуть up_for_retry в очередь
    finished_at timestamptz,
    primary key (run_id, task)
);

-- очередь планировщика: выборка queued-тасков через FOR UPDATE SKIP LOCKED
create index task_instance_queued_idx on task_instance (queued_at) where status = 'queued';
-- возврат ретраев в очередь по расписанию backoff'а
create index task_instance_retry_idx on task_instance (retry_at) where status = 'up_for_retry';

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
