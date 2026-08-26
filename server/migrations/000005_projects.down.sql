-- Обратный переход: проект схлопывается обратно в даг (берётся первый
-- инстанс каждого проекта, остальные теряются — это ожидаемо, вернуться к
-- одному дагу на образ без потерь нельзя).

drop view if exists dag_full;

drop table if exists project_registration cascade;

create table dag_registration (
    id          text primary key,
    image       text not null,
    source      text not null,
    schedule    text,
    catchup     boolean,
    paused      boolean,
    auto_update boolean,
    pool        text,
    status      text not null default 'pending',
    error       text not null default '',
    dag_name    text not null default '',
    created_at  timestamptz not null default now(),
    started_at  timestamptz,
    finished_at timestamptz
);

create index dag_registration_active_idx on dag_registration (created_at)
    where status in ('pending', 'running');
create index dag_registration_dag_idx on dag_registration (dag_name, created_at desc);

alter table run_env add column scope text not null default '';
update run_env set scope = split_part(scope_name, '/', 2) where scope_kind = 'dag';
alter table run_env drop column scope_kind, drop column scope_name;

alter table setting drop constraint setting_pkey;
delete from setting a using setting b
    where a.name = b.name and a.dag_name = b.dag_name and a.project_name > b.project_name;
alter table setting add primary key (dag_name, name), drop column project_name;

alter table secret drop constraint secret_pkey;
delete from secret a using secret b
    where a.name = b.name and a.dag_name = b.dag_name and a.project_name > b.project_name;
alter table secret add primary key (dag_name, name), drop column project_name;

alter table variable drop constraint variable_pkey;
delete from variable a using variable b
    where a.name = b.name and a.dag_name = b.dag_name and a.project_name > b.project_name;
alter table variable add primary key (dag_name, name), drop column project_name;

drop index run_dag_idx;
alter table run drop column project_name, drop column template;
create index run_dag_name_idx on run (dag_name, created_at desc);

drop table if exists user_project cascade;

alter table user_dag drop constraint user_dag_dag_fkey, drop constraint user_dag_pkey;
delete from user_dag a using user_dag b
    where a.user_id = b.user_id and a.dag_name = b.dag_name and a.project_name > b.project_name;
alter table user_dag add primary key (user_id, dag_name), drop column project_name;

alter table task_resources drop constraint task_resources_dag_fkey,
    drop constraint task_resources_pkey;
delete from task_resources a using task_resources b
    where a.task = b.task and a.dag_name = b.dag_name and a.project_name > b.project_name;
alter table task_resources add primary key (dag_name, task), drop column project_name;

-- в дага возвращаются образ и манифест; лишние инстансы проекта удаляются
delete from dag a using dag b
    where a.project_name = b.project_name and a.name > b.name;

alter table dag
    drop constraint dag_template_fkey,
    drop constraint dag_project_fkey,
    drop constraint dag_pkey,
    add column image        text not null default '',
    add column image_digest text not null default '',
    add column manifest     jsonb,
    add column auto_update  boolean not null default false;

update dag d set image = p.image, image_digest = p.image_digest, auto_update = p.auto_update
from project p where p.name = d.project_name;
update dag d set manifest = t.manifest
from dag_template t where t.project_name = d.project_name and t.name = d.template;

alter table dag
    alter column manifest set not null,
    add primary key (name),
    drop column project_name,
    drop column template;

alter table task_resources add constraint task_resources_dag_name_fkey
    foreign key (dag_name) references dag (name) on delete cascade;
alter table user_dag add constraint user_dag_dag_name_fkey
    foreign key (dag_name) references dag (name) on delete cascade;

drop table if exists dag_template cascade;
drop table if exists project cascade;
