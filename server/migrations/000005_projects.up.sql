-- Проекты: один docker-образ может нести несколько дагов, а от каждого дага
-- образа («шаблона») можно завести сколько угодно дагов-инстансов со своими
-- настройками. Идентификатор дага становится составным: (project_name, name).
--
-- Раскладка:
--   project       — образ и его digest, авто-обновление;
--   dag_template  — даг, объявленный в образе, с манифестом из `describe`;
--   dag           — инстанс шаблона: расписание, пауза, пул, переменные и т.д.
-- Шаблон, исчезнувший из образа, не удаляется — помечается orphaned, чтобы
-- инстансы не теряли последний известный граф.

create table project (
    name         text primary key,
    image        text        not null, -- url образа, как задан при регистрации (тег)
    image_digest text        not null, -- закреплённый digest (repo@sha256:...)
    image_size_bytes bigint  not null default 0, -- размер в registry (config + слои), 0 — неизвестен
    auto_update  boolean     not null default false,
    created_at   timestamptz not null default now(),
    modified_at  timestamptz
);

create table dag_template (
    project_name text        not null references project (name) on delete cascade,
    name         text        not null, -- имя дага в коде (манифест `describe`)
    sdk_version  text        not null default '',
    manifest     jsonb       not null, -- манифест дага как есть
    orphaned     boolean     not null default false, -- пропал из образа при последней регистрации
    created_at   timestamptz not null default now(),
    modified_at  timestamptz,
    primary key (project_name, name)
);

-- ── перенос существующих дагов: даг → проект своего имени ────────────────

insert into project (name, image, image_digest, auto_update, created_at, modified_at)
select name, image, image_digest, auto_update, created_at, modified_at from dag;

insert into dag_template (project_name, name, sdk_version, manifest, created_at, modified_at)
select name, coalesce(nullif(manifest ->> 'name', ''), name),
       coalesce(manifest ->> 'sdk_version', ''), manifest, created_at, modified_at
from dag;

-- ── dag: составной ключ (project_name, name) + ссылка на шаблон ──────────

alter table task_resources drop constraint task_resources_dag_name_fkey;
alter table user_dag drop constraint user_dag_dag_name_fkey;

alter table dag
    add column project_name text not null default '',
    add column template     text not null default '';

update dag set project_name = name,
               template = coalesce(nullif(manifest ->> 'name', ''), name);

alter table dag
    drop constraint dag_pkey,
    add primary key (project_name, name),
    add constraint dag_project_fkey foreign key (project_name)
        references project (name) on delete cascade,
    add constraint dag_template_fkey foreign key (project_name, template)
        references dag_template (project_name, name) on delete restrict,
    alter column project_name drop default,
    alter column template drop default,
    -- образ уехал в project, манифест — в dag_template
    drop column image,
    drop column image_digest,
    drop column manifest,
    drop column auto_update;

-- Чтение дага всегда идёт вместе с образом проекта и манифестом шаблона:
-- FK гарантируют обе стороны, поэтому view с inner join безопасен. Пишется
-- по-прежнему в таблицу dag.
create view dag_full as
select d.project_name, d.name, d.template, d.schedule, d.catchup, d.paused,
       d.pool, d.next_run_at, d.created_at, d.modified_at,
       p.image, p.image_digest, p.auto_update,
       t.sdk_version, t.manifest, t.orphaned as template_orphaned
from dag d
    join project p on p.name = d.project_name
    join dag_template t on t.project_name = d.project_name and t.name = d.template;

-- ── связанные сущности: дага теперь два поля ────────────────────────────

alter table task_resources add column project_name text not null default '';
update task_resources set project_name = dag_name;
alter table task_resources
    drop constraint task_resources_pkey,
    add primary key (project_name, dag_name, task),
    add constraint task_resources_dag_fkey foreign key (project_name, dag_name)
        references dag (project_name, name) on delete cascade,
    alter column project_name drop default;

alter table user_dag add column project_name text not null default '';
update user_dag set project_name = dag_name;
alter table user_dag
    drop constraint user_dag_pkey,
    add primary key (user_id, project_name, dag_name),
    add constraint user_dag_dag_fkey foreign key (project_name, dag_name)
        references dag (project_name, name) on delete cascade,
    alter column project_name drop default;

-- Права на весь проект: пользователь владеет всеми его дагами, включая
-- заведённые позже.
create table user_project (
    user_id      text not null references app_user (id) on delete cascade,
    project_name text not null references project (name) on delete cascade,
    primary key (user_id, project_name)
);

-- run переживает удаление дага (retention чистит такие раны по глобальным
-- лимитам), поэтому ссылки на dag нет — только денормализованные имена.
alter table run
    add column project_name text not null default '',
    -- шаблон образа, из которого запускается таск (env LOOM_DAG): образ
    -- может нести несколько дагов, а ран должен пережить удаление дага
    add column template text not null default '';
update run set project_name = dag_name, template = dag_name;
alter table run
    alter column project_name drop default,
    alter column template drop default;

drop index run_dag_name_idx;
create index run_dag_idx on run (project_name, dag_name, created_at desc);

-- ── переменные, секреты, настройки: три скоупа ──────────────────────────
-- ('', '') — глобальный, (project, '') — проектный, (project, dag) — дага;
-- при резолве в launch более узкий перекрывает более широкий.

alter table variable add column project_name text not null default '';
update variable set project_name = dag_name where dag_name <> '';
alter table variable
    drop constraint variable_pkey,
    add primary key (project_name, dag_name, name);

alter table secret add column project_name text not null default '';
update secret set project_name = dag_name where dag_name <> '';
alter table secret
    drop constraint secret_pkey,
    add primary key (project_name, dag_name, name);

alter table setting add column project_name text not null default '';
update setting set project_name = dag_name where dag_name <> '';
alter table setting
    drop constraint setting_pkey,
    add primary key (project_name, dag_name, name);

-- Снапшот env-резолва рана: источник значения теперь трёхуровневый.
alter table run_env
    add column scope_kind text not null default 'global', -- global | project | dag
    add column scope_name text not null default '';       -- имя проекта или '<проект>/<даг>'
update run_env set scope_kind = 'dag', scope_name = scope || '/' || scope where scope <> '';
alter table run_env drop column scope;

-- ── очередь регистраций: регистрируется проект, а не даг ────────────────
-- Записи эфемерны (чистятся по dag_reg_ttl), поэтому таблица пересоздаётся.

drop table dag_registration;

create table project_registration (
    id           text primary key,
    project_name text not null default '',    -- известен сразу: задаётся при регистрации
    image        text not null,
    source       text not null,               -- manual | auto
    -- желаемые настройки нового проекта (применяются только при создании)
    auto_update  boolean,
    -- заводить даги-инстансы по новым шаблонам образа
    create_dags  boolean not null default true,
    status       text not null default 'pending', -- pending | running | success | failed
    error        text not null default '',
    -- итог по дагам каталога: [{name, status, error}]
    result       jsonb not null default '[]',
    created_at   timestamptz not null default now(),
    started_at   timestamptz,
    finished_at  timestamptz
);

create index project_registration_active_idx on project_registration (created_at)
    where status in ('pending', 'running');
create index project_registration_project_idx
    on project_registration (project_name, created_at desc);
