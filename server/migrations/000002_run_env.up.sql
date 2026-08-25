-- Снапшот env-резолва рана: что реально ушло в поды тасков при launch
-- (admin/docs/design/07, требование №3). Пишется upsert'ом при каждом
-- launch попытки — ретрай обновляет записи фактической инъекцией, поэтому
-- объём ограничен числом env-привязок рана, а не числом попыток.
-- Значения секретов не сохраняются — только имя и скоуп-источник.
--
-- Очистка — как у run_value/task_instance/attempt: retention удаляет ран
-- (DELETE FROM run), записи run_env уходят каскадом по FK. Отдельный
-- индекс по run_id не нужен: PK начинается с run_id и обслуживает и
-- каскадное удаление, и выборку ListRunEnv.
create table run_env (
    run_id      text not null references run (id) on delete cascade,
    env         text not null,           -- имя env-переменной в контейнере
    kind        text not null,           -- variable | secret
    name        text not null,           -- имя переменной/секрета control plane
    scope       text not null default '', -- '' — глобальный скоуп, иначе имя дага
    value       text not null default '', -- только у variable
    resolved_at timestamptz not null default now(),
    primary key (run_id, kind, env)
);
