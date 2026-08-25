package k8sexecutor

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/fake"

	runModel "github.com/rendau/loom/server/internal/domain/run/model"
)

func testSpec(dag, task string) runModel.LaunchSpec {
	return runModel.LaunchSpec{
		Ref:     runModel.AttemptRef{RunId: "0198f0e2-2c1b-7c2a-9f3e-9a1d0f5b7c11", Task: task, Attempt: 2},
		DagName: dag,
		Image:   "registry/dag@sha256:deadbeef",
	}
}

func TestJobName(t *testing.T) {
	spec := testSpec("demo-etl", "extract")

	name := jobName(spec)

	assert.Equal(t, "lt-demo-etl-extract-2-"+refHash(spec.Ref), name)
	assert.Equal(t, name, jobName(spec), "имя детерминировано")
	assert.Empty(t, validation.IsDNS1123Label(name))
}

func TestJobNameSanitizesAndFits(t *testing.T) {
	cases := []struct{ name, dag, task string }{
		{"кириллица", "Товары/Каталог", "Загрузка Данных"},
		{"оба длинные", "products-catalog-daily-sync-full", "extract-products-from-source-v2"},
		{"пустые", "", ""},
		{"мусор", "--", "__"},
		{"предельные имена дага и таска", strings.Repeat("d", 63), strings.Repeat("t", 63)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, attempt := range []int32{0, 999, 2147483647} {
				spec := testSpec(c.dag, c.task)
				spec.Ref.Attempt = attempt

				name := jobName(spec)

				assert.Empty(t, validation.IsDNS1123Label(name), name)
				// имя Job'а уезжает в значение лейбла подов — потолок 63
				assert.LessOrEqual(t, len(name), jobNameMaxLen, name)
				assert.True(t, strings.HasPrefix(name, jobNamePrefix), name)
			}
		})
	}
}

// Бюджет длины общий на пару имён: короткий сосед не заставляет резать
// длинного, пока сумма влезает.
func TestJobNameCutsOnlyOverBudget(t *testing.T) {
	longDag := "products-catalog-daily-sync"

	spec := testSpec(longDag, "load")
	name := jobName(spec)

	assert.Equal(t, "lt-"+longDag+"-load-2-"+refHash(spec.Ref), name)
	assert.LessOrEqual(t, len(name), jobNameMaxLen)

	// зеркальный случай: длинный таск при коротком даге тоже не режется
	spec = testSpec("etl", "extract-products-from-source")
	assert.Equal(t, "lt-etl-extract-products-from-source-2-"+refHash(spec.Ref), jobName(spec))
}

// Длинные оба — бюджет делится пополам, обе части остаются узнаваемыми.
func TestJobNameSplitsBudgetBetweenLongNames(t *testing.T) {
	spec := testSpec("products-catalog-daily-sync", "extract-products-from-source")

	name := jobName(spec)

	assert.LessOrEqual(t, len(name), jobNameMaxLen)
	assert.Empty(t, validation.IsDNS1123Label(name))
	assert.Equal(t, "lt-products-catalog-daily-extract-products-from-s-2-"+refHash(spec.Ref), name)
}

func TestJobNameDiffersByAttempt(t *testing.T) {
	first := testSpec("demo-etl", "extract")
	second := first
	second.Ref.Attempt = 3

	assert.NotEqual(t, jobName(first), jobName(second))
}

// Kill находит Job по лейблу с хэшем попытки: имя Job'а из одного ref'а не
// собирается (в нём есть имя дага).
func TestKillDeletesJobOfAttempt(t *testing.T) {
	cs := fake.NewClientset()
	svc := New(cs, nil, "ns", "", 0)

	spec := testSpec("demo-etl", "extract")
	other := testSpec("demo-etl", "load")

	require.NoError(t, svc.Launch(context.Background(), spec))
	require.NoError(t, svc.Launch(context.Background(), other))

	require.NoError(t, svc.Kill(context.Background(), spec.Ref))

	list, err := cs.BatchV1().Jobs("ns").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, jobName(other), list.Items[0].Name)
}

func TestKillWithoutJobIsNoop(t *testing.T) {
	cs := fake.NewClientset()
	svc := New(cs, nil, "ns", "", 0)

	assert.NoError(t, svc.Kill(context.Background(), testSpec("demo-etl", "extract").Ref))
}
