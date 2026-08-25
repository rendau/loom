-- Настройки инсталляции переезжают из env-конфига в БД (редактируются
-- админкой). Скоуп как у variable/secret: dag_name = '' — глобальный,
-- имя дага — уточнение для конкретного дага, перекрывает глобальное при
-- резолве. Список известных имён, типы и валидацию контролирует домен
-- setting (server/internal/domain/setting) — произвольные имена не пишутся.
create table setting (
    dag_name    text not null default '',
    name        text not null,
    value       text not null,
    modified_at timestamptz not null default now(),
    primary key (dag_name, name)
);

-- Глобальные дефолты совпадают с прежними env-дефолтами
-- (RUN_TTL / K8S_JOB_TTL / DAG_REG_TTL).
insert into setting (dag_name, name, value) values
    ('', 'run_ttl', '720h'),      -- TTL завершённых ранов; 0 — по времени не чистить
    ('', 'run_keep_last', '0'),   -- хранить N последних завершённых ранов дага; 0 — не ограничивать
    ('', 'k8s_job_ttl', '1h'),    -- ttlSecondsAfterFinished Job'ов попыток; 0 — не удалять
    ('', 'dag_reg_ttl', '720h');  -- TTL записей истории регистраций; 0 — вечно

-- Оверрайды ресурсов тасков из админки: значения из кода дага (манифеста) —
-- рекомендуемые, непустое поле здесь — приоритетнее. Применяется при launch
-- попытки, поэтому подхватывается и ретраями без перерегистрации дага.
-- Чистка — каскадом при удалении дага.
create table task_resources (
    dag_name       text not null references dag (name) on delete cascade,
    task           text not null,
    cpu_request    text not null default '',
    cpu_limit      text not null default '',
    memory_request text not null default '',
    memory_limit   text not null default '',
    modified_at    timestamptz not null default now(),
    primary key (dag_name, task)
);
