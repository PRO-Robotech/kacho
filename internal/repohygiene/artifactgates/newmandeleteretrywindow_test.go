// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmandeleteretrywindow_test.go — шаг удаления, который УТВЕРЖДАЕТ 200, обязан
// уметь ПЕРЕЖДАТЬ 404.
//
// # Предмет
//
// Обёртка окна видимости (`retry_until_authorized`) решает, какой код пережидать,
// по скрипту шага. У шага удаления без собственного утверждения этот скрипт на
// момент решения ПУСТ — а утверждение `delete accepted: status 200` дописывается
// ему ПОЗЖЕ, при сериализации. Решение принималось на предпосылке, которую
// следующий же проход отменял: обёртка ждала только 403, а падал шаг на 404.
//
// Почему именно 404, а не 403. Ресурс, скрывающий существование, отвечает
// неавторизованному вызывающему `NotFound` — побайтово тем же текстом, что и
// настоящее «не найдено» (`security.md` §6 hide-existence). Значит у мутации над
// таким ресурсом окно материализации видно КАК 404, и обёртка, не ждущая 404, не
// может сработать на том единственном коде, который она увидит: форма ожидания
// есть, содержания нет.
//
// # Замер, из которого гейт выведен
//
// Ревизия cc13e696, 86 коллекций, 8843 шага: 474 шага удаления несли дописанное
// утверждение `status 200` и обёртку окна видимости, и у 403 из них 404 в
// ожидание не попадал. Контроль в другую сторону на той же переписи: 2032 шага
// 404 пережидали — то есть предикат различает форму, а не помечает всё подряд.
//
// Наблюдалось на стволе (`main`, прогон 31802555601): после переноса
// балансировщика в другой проект и обратно уборка получила
// `404 "NetworkLoadBalancer <id> not found"` с ПЕРВОЙ попытки и упала, хотя была
// обёрнута — обёртка ждала 403.
//
// # Что гейт НЕ утверждает
//
// Он не требует ждать 404 везде. 404, НАЗВАННЫЙ шагом законным исходом
// (`to.eql(404)` / `oneOf([...404...])`), в ожидание не попадает и попадать не
// должен: иначе проба пережидала бы ровно то, ради чего написана. Гейт смотрит
// только на шаги с ДОПИСАННЫМ утверждением 200 — у них 404 законным исходом быть
// не может по построению.
package artifactgates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// nmDeleteDefaultAssertMark — маркер ДОПИСАННОГО утверждения шага удаления.
// Генераторы вставляют его вместе с самим утверждением (`_DELETE_ACCEPTED`),
// поэтому его присутствие означает ровно одно: шаг не нёс своего утверждения, и
// теперь обязан ответить 200.
const nmDeleteDefaultAssertMark = "УТВЕРЖДЕНИЕ ПО УМОЛЧАНИЮ для шага удаления"

// nmRetryWindowRe — набор кодов, которые пережидает обёртка окна видимости.
var nmRetryWindowRe = regexp.MustCompile(`if \(\[([\d,\s]+)\]\.includes\(pm\.response\.code\) && _arc`)

func TestNewmanDeleteStepAssertingOkAlsoWaitsOutHideExistence(t *testing.T) {
	root := repoRoot(t)

	// Состав — из ИНДЕКСА git, а не обходом диска: под корнем лежат рабочие копии
	// агентов и распаковки отчётов прогонов, и вердикт по ним был бы свойством
	// чужого каталога, а не коммита.
	tt := newTrackedTree(t, root)
	var cols []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			cols = append(cols, rel)
		}
	}
	sort.Strings(cols)

	findings, cen, err := auditDeleteRetryWindow(root, cols)
	if err != nil {
		t.Fatal(err)
	}

	// ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ — «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать; " +
			"чинить надо обход, а не выходить успехом")
	}
	if cen.steps == 0 {
		t.Fatalf("прочитано коллекций %d, шагов 0 — обход не узнал ни одного шага", cen.collections)
	}
	// Распознаватели обоих механизмов обязаны что-то находить. Ноль дописанных
	// утверждений ЛИБО ноль обёрток означает, что сменилась форма и гейт ослеп, —
	// это отказ, а не достижение цели: оба механизма в дереве есть by construction
	// (их ставят генераторы), в отличие от самих находок.
	if cen.defaultAsserted == 0 {
		t.Fatal("ни одного шага удаления с дописанным утверждением — распознаватель " +
			"читает не ту форму (маркер сменился?), гейт ослеп")
	}
	if cen.wrapped == 0 {
		t.Fatal("ни одной обёртки окна видимости — распознаватель читает не ту форму, гейт ослеп")
	}

	t.Logf("осмотрено: коллекций %d, шагов %d; обёрток окна видимости %d; "+
		"шагов удаления с дописанным утверждением %d, из них обёрнутых %d; "+
		"шагов, пережидающих 404, %d (контроль обратной стороны)",
		cen.collections, cen.steps, cen.wrapped,
		cen.defaultAsserted, cen.defaultAssertedWrapped, cen.waits404)

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "шагов удаления, утверждающих 200 и НЕ пережидающих 404: %d\n\n", len(findings))
		fmt.Fprintf(&b, "Такой шаг обязан ответить 200, но его обёртка окна видимости 404 не ждёт.\n")
		fmt.Fprintf(&b, "Ресурс, скрывающий существование, отдаёт окно материализации ИМЕННО как 404\n")
		fmt.Fprintf(&b, "(security.md §6: текст побайтово равен настоящему «не найдено»), поэтому\n")
		fmt.Fprintf(&b, "ожидание не может сработать на том коде, который шаг увидит.\n")
		fmt.Fprintf(&b, "Чинится В ГЕНЕРАТОРЕ (`_wrap_own_fresh_reads` / `retry_until_authorized`),\n")
		fmt.Fprintf(&b, "а не правкой кейса: решение о том, чего ждать, обязано приниматься по тому\n")
		fmt.Fprintf(&b, "скрипту, который шаг ПОНЕСЁТ, а не по тому, который он несёт в момент обёртки.\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}

// nmDeleteRetryCensus — объём осмотренного.
type nmDeleteRetryCensus struct {
	collections            int
	steps                  int
	wrapped                int
	defaultAsserted        int
	defaultAssertedWrapped int
	waits404               int
}

type nmDeleteRetryFinding struct {
	collection string
	folder     string
	step       string
	waits      string
}

func (f nmDeleteRetryFinding) String() string {
	return fmt.Sprintf("%s :: %s :: %s — утверждает 200, ждёт только [%s]",
		f.collection, f.folder, f.step, f.waits)
}

// auditDeleteRetryWindow — весь разбор одним входом.
//
// Вынесено из тела гейта намеренно: проба, доказывающая способность гейта упасть,
// обязана гонять ТУ ЖЕ функцию — иначе она доказывала бы свойство своей копии.
func auditDeleteRetryWindow(root string, cols []string) ([]nmDeleteRetryFinding, nmDeleteRetryCensus, error) {
	var findings []nmDeleteRetryFinding
	var cen nmDeleteRetryCensus

	for _, rel := range cols {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, cen, fmt.Errorf("чтение %s: %w", rel, err)
		}
		var col nmCollection
		if err := json.Unmarshal(b, &col); err != nil {
			return nil, cen, fmt.Errorf("разбор %s: %w", rel, err)
		}
		cen.collections++

		var scan func(folder string, items []nmItem)
		scan = func(folder string, items []nmItem) {
			for _, it := range items {
				if it.isFolder() {
					name := folder
					if it.Name != "" {
						name = it.Name
					}
					scan(name, it.Item)
					continue
				}
				cen.steps++
				for _, ev := range it.Event {
					if ev.Listen != "test" {
						continue
					}
					src := strings.Join(ev.Script.Exec, "\n")
					m := nmRetryWindowRe.FindStringSubmatch(src)
					if m == nil {
						if strings.Contains(src, nmDeleteDefaultAssertMark) {
							cen.defaultAsserted++
						}
						continue
					}
					cen.wrapped++
					waits := strings.NewReplacer(" ", "").Replace(m[1])
					has404 := false
					for _, c := range strings.Split(waits, ",") {
						if c == "404" {
							has404 = true
						}
					}
					if has404 {
						cen.waits404++
					}
					if !strings.Contains(src, nmDeleteDefaultAssertMark) {
						continue
					}
					cen.defaultAsserted++
					cen.defaultAssertedWrapped++
					if !has404 {
						findings = append(findings, nmDeleteRetryFinding{
							collection: rel, folder: folder, step: it.Name, waits: waits,
						})
					}
				}
			}
		}
		scan("", col.Item)
	}
	return findings, cen, nil
}
