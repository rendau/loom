# Скилл `loom-dag`

Скилл для Claude Code (и совместимых агентов): как писать даги на loom SDK —
API, семантика артефактов и рёбер, подводные камни, проверенные приёмы,
жизненный цикл дага на control plane.

```
loom-dag/
├── SKILL.md                    # основной документ: модель, скелет, правила, чек-лист
└── references/
    ├── api.md                  # справочник API, CLI, env-контракта, манифеста
    ├── pitfalls.md             # подводные камни и типовые ошибки (SDK + внешние системы)
    ├── patterns.md             # проверенные приёмы из боевого репозитория дагов
    └── operations.md           # регистрация, настройки, ретраи, retention, отладка
```

## Установка в проект дага

Из модуля SDK, уже подтянутого в проект:

```bash
mkdir -p .claude/skills
cp -r "$(go list -m -f '{{.Dir}}' github.com/rendau/loom/sdk)/skills/loom-dag" .claude/skills/
chmod -R u+w .claude/skills/loom-dag   # в module cache файлы read-only
```

Или прямо из репозитория:

```bash
git clone --depth=1 https://github.com/rendau/loom /tmp/loom
mkdir -p .claude/skills && cp -r /tmp/loom/sdk/skills/loom-dag .claude/skills/
```

Глобально (для всех проектов) — то же самое в `~/.claude/skills/`.

Версия SDK, под которую написан скилл, — в поле `metadata.sdk_version` заголовка
`SKILL.md`. Обновляя SDK, обнови и скилл.
