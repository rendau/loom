# loom SDK

Библиотека для написания дагов [loom](https://github.com/rendau/loom) —
оркестратора пайплайнов на Go (аналог Apache Airflow). Даг — обычный
Go-бинарник на этом SDK, упакованный в docker-образ: **один образ = один
даг**. Данные между тасками текут персистентными стримами через
artifact-сервер, мелкие значения — через control plane.

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
go run . describe                    # JSON-манифест дага
go run . run                         # локальный прогон целиком
go run . run --params='{"day":"x"}'  # с параметрами рана
```

В распределённом режиме control plane запускает тот же образ по таску
(`run --task=<name>` + env-контракт `LOOM_*`) — код дага не меняется:
таск общается с миром только через `Runtime`.

## Ключевые опции

- **Дага**: `MaxActiveRuns(n)`. Cron-расписание и catchup в коде не задаются —
  ими управляет админка control plane после регистрации дага.
- **Таска**: `After(dep)` — ждать успеха отправителя; `AfterStreamed(dep)` —
  ко-старт и чтение по мере записи; `Retries(n)`, `RetryDelay(d)`,
  `Timeout(d)`, `Resources(spec)`, `Pool(name)`, `Priority(n)`,
  `Secret(envName, secretName)`, `Variable(envName, varName)` — инъекции
  секретов и переменных control plane в env контейнера (в локальном режиме
  не инжектятся — задавайте окружением процесса).
- **Runtime**: `Output`/`Input` — стримы артефактов; `PushValue`/`PullValue` —
  мелкие значения (аналог XCom, ≤64KB); `Params`/`BindParams` — параметры
  рана; `LogicalDate` — «дата данных»; `Log()` — логгер (в распределённом
  режиме строки уходят стримом на artifact-сервер с подтверждениями
  доставки и дублем в stdout контейнера).

Семантика артефактов, commit/abort и жизненный цикл попыток — в
[README монорепы](https://github.com/rendau/loom#readme).
