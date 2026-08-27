# loom SDK

Библиотека для написания дагов [loom](https://github.com/rendau/loom) —
оркестратора пайплайнов на Go (аналог Apache Airflow). Даг — обычный
Go-бинарник на этом SDK, упакованный в docker-образ; **в одном образе может
быть несколько дагов** — на control plane образ становится проектом, а его
даги — шаблонами, от которых заводят сколько угодно дагов-инстансов. Данные
между тасками текут персистентными стримами через artifact-сервер, мелкие
значения — через control plane.

```bash
go get github.com/rendau/loom/sdk
```

## Минимальный даг

```go
package main

import (
	"bufio"
	"context"
	"fmt"

	loom "github.com/rendau/loom/sdk"
)

func main() {
	dag := loom.New("demo")

	extract := dag.Task("extract", func(_ context.Context, rt *loom.Runtime) error {
		out, err := rt.Output("numbers") // io.Writer; Close опционален
		if err != nil {
			return err
		}
		for i := 1; i <= 100; i++ {
			fmt.Fprintln(out, i)
		}
		// return nil коммитит все выходы; ошибка или паника — abort
		return nil
	})

	dag.Task("load", func(_ context.Context, rt *loom.Runtime) error {
		in, err := rt.Input("extract", "numbers")
		if err != nil {
			return err
		}
		defer in.Close()

		scanner := bufio.NewScanner(in)
		count := 0
		for scanner.Scan() {
			count++
		}
		rt.Log().Info("loaded", "count", count)
		return scanner.Err()
	}, loom.After(extract))

	loom.Main(dag)
}
```

Запуск локально — весь даг одним процессом, артефакты остаются в
`.loom/runs/<run-id>/` для изучения:

```bash
go run . describe                    # JSON-каталог образа: манифесты всех дагов
go run . run                         # локальный прогон целиком
go run . run --params='{"day":"x"}'  # с параметрами рана
```

Если бинарник несёт несколько дагов, нужный выбирается флагом `--dag`
(в распределённом режиме его передаёт control plane через `LOOM_DAG`):

```go
func main() {
	loom.Main(newOrdersETL(), newNsiSync()) // каждый даг — шаблон проекта
}
```

```bash
go run . describe                    # оба дага в каталоге
go run . run --dag=orders_etl        # локальный прогон одного из них
```

Несколько инстансов одного шаблона (например, свой даг на каждый магазин)
заводятся в админке; различаются они переменными и секретами дага — код
общий, значения у каждого инстанса свои.

В распределённом режиме control plane запускает тот же образ по таску
(`run --task=<name>` + env-контракт `LOOM_*`) — код дага не меняется:
таск общается с миром только через `Runtime`.

## Ключевые опции

- **Дага**: `MaxActiveRuns(n)`. Cron-расписание, catchup и пул слотов в коде
  не задаются — ими управляет админка control plane после регистрации дага.
- **Таска**: `After(dep)` — ждать успеха отправителя; `AfterStreamed(dep)` —
  ко-старт и чтение по мере записи; `Retries(n)`, `RetryDelay(d)`,
  `Timeout(d)`, `Resources(spec)`, `Priority(n)`,
  `Secret(envName, secretName[, description])`,
  `Variable(envName, varName[, description])` — инъекции секретов и
  переменных control plane в env контейнера (в локальном режиме не
  инжектятся — задавайте окружением процесса). Необязательное описание
  уезжает в манифест: админка показывает по нему, какие переменные нужны
  дагу и что в них класть, — спрашивать автора дага не приходится.

  ```go
  dag.Task("load", loadFn,
      loom.Variable("PG_DSN", "pg_dsn", "DSN основной БД, формат postgres://…"),
      loom.Secret("S3_KEY", "s3_key", "ключ доступа к бакету выгрузок"),
  )
  ```
- **Runtime**: `Output`/`Input` — стримы артефактов; `PushValue`/`PullValue` —
  мелкие значения (аналог XCom, ≤64KB); `Params`/`BindParams` — параметры
  рана; `LogicalDate` — «дата данных»; `Log()` — логгер (в распределённом
  режиме строки уходят стримом на artifact-сервер с подтверждениями
  доставки и дублем в stdout контейнера).

Семантика артефактов, commit/abort и жизненный цикл попыток — в
[README монорепы](https://github.com/rendau/loom#readme).

## Скилл для агента

Рядом с SDK лежит скилл `skills/loom-dag/` — инструкция для AI-агента (Claude Code
и совместимых) по написанию дагов: API и семантика артефактов, подводные камни,
жизненный цикл дага на control plane. Установка в проект дага:

```bash
mkdir -p .claude/skills
cp -r "$(go list -m -f '{{.Dir}}' github.com/rendau/loom/sdk)/skills/loom-dag" .claude/skills/
chmod -R u+w .claude/skills/loom-dag
```

Подробности — `skills/loom-dag/README.md`.
