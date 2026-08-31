// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция для гейта величины окна в корпусе проб — В ОБЕ СТОРОНЫ.
//
// Гейт, чью способность падать не доказали, неотличим от вечно-зелёного.
// Поэтому здесь:
//
//	(а) НАСТОЯЩИЙ дефект в КАЖДОЙ известной форме записи обязан дать находку с
//	    координатой и с именем формы. По каждой форме отдельно: форма, о которой
//	    распознаватель не знает, даёт не находку и не молчание, а невидимость;
//	(б) ЗАКОННЫЙ БЛИЗНЕЦ обязан молчать — и близнец здесь не выдуман: это
//	    дословно тот текст, которым правка #1730 заменила величину;
//	(в) СОСЕДНИЕ ЗАКОННЫЕ ФОРМЫ обязаны молчать: объявление бюджета — тоже
//	    длительность в комментарии, а голый токен `FGA` законно стоит в корпусе
//	    десятками. Гейт, краснеющий на них, был бы снят первым;
//	(г) СУЖЕНИЕ ОБХОДА до корпуса registry обязано быть свойством гейта, а не
//	    случайностью: тот же дефект вне корпуса обязан остаться невидимым;
//	(д) ПУСТОЙ ОБХОД отличим от «нарушений нет».

// ct3ProbeFile — один файл синтетического корпуса.
type ct3ProbeFile struct {
	// rel — путь ОТНОСИТЕЛЬНО корня синтетического дерева.
	rel string
	// body — содержимое.
	body string
}

func writeCt3ProbeTree(t *testing.T, files ...ct3ProbeFile) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", f.rel, err)
		}
		if err := os.WriteFile(p, []byte(f.body), 0o600); err != nil {
			t.Fatalf("write %s: %v", f.rel, err)
		}
	}
	return root
}

func ct3ProbeRun(t *testing.T, root string) (ct3ProbeWindowCensus, []string) {
	t.Helper()
	c, err := collectRegistryProbeWindow(mustSyntheticTree(t, root))
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	return c, registryProbeWindowFindings(c)
}

// ct3InCorpus — путь ВНУТРИ корпуса, за который гейт отвечает.
func ct3InCorpus(name string) string { return ct3ProbeCorpusDir + name }

// ct3DefectForms — по одному настоящему дефекту на каждую известную форму
// записи. Записи не придуманы: все три стояли в дереве ОДНОВРЕМЕННО — корпус
// случаев писал дефисом, документы и оболочка средним тире, клиентская страница
// кириллической единицей.
var ct3DefectForms = []struct {
	name string
	line string
	form string
}{
	{"дефис + латинская s", "# retry-on-404 поглощает grant-latency (FGA-пропагация ~0.6-2s).",
		"величина снятого механизма"},
	{"среднее тире + латинская s", "  # (FGA-пропагация ~0.6–2s). Первый pull может дать 404.",
		"величина снятого механизма"},
	{"среднее тире + кириллическая с", "| poll-retry, ~0.6–2 с | REG-30 |",
		"величина снятого механизма"},
	{"запятая как разделитель дробной части", "# окно ~0,6-2 сек — ждём.",
		"величина снятого механизма"},
	{"переписанное число, приписанное очереди снятого движка",
		"# poll-retry: FGA propagation takes ~1-3s to become visible.",
		"цена, приписанная очереди снятого движка"},
}

// (а) НАСТОЯЩИЙ ДЕФЕКТ в каждой форме — находка с координатой И с именем формы.
func TestCt3ProbeWindowInjection_EachWrittenFormIsAFinding(t *testing.T) {
	for _, d := range ct3DefectForms {
		t.Run(d.name, func(t *testing.T) {
			root := writeCt3ProbeTree(t, ct3ProbeFile{
				rel:  ct3InCorpus("cases/registry-authz.py"),
				body: "# заголовок случая\n" + d.line + "\nCASES.append(Case(id='X'))\n",
			})
			c, findings := ct3ProbeRun(t, root)

			if len(findings) != 1 {
				t.Fatalf("форма %q обязана дать РОВНО одну находку, получено %d: %v",
					d.name, len(findings), findings)
			}
			// Находка обязана называть КООРДИНАТУ и ФОРМУ: без первой читателю
			// негде править, без второй — непонятно, что именно опознано.
			for _, want := range []string{"cases/registry-authz.py:2", d.form} {
				if !strings.Contains(findings[0], want) {
					t.Errorf("находка обязана называть %q, а называет: %s", want, findings[0])
				}
			}
			if len(c.Citations) != 1 || c.Citations[0].Line != 2 {
				t.Errorf("перепись обязана дать одну цитату на строке 2, получено %+v",
					c.Citations)
			}
		})
	}
}

// ct3LawfulReplacement — ДОСЛОВНО тот текст, которым правка #1730 заменила
// величину. Близнец не выдуман: если гейт краснеет на собственной починке, он
// не различает предмет и его описание.
const ct3LawfulReplacement = `  # register-on-first-push материализует per-object v_get на новом repo асинхронно.
  # Окно видимости складывают ДВА слагаемых, и у каждого свой владелец: кэш вердиктов
  # registry (ручка KACHO_REGISTRY_AUTHZ_CACHE_TTL) и материализация выдачи у владельца
  # прав (величину называет документация IAM). Здесь величина НЕ пишется: она была бы
  # вторым местом об одном предмете и разошлась бы с ручкой молча — бюджет ожидания
  # виден на связывании ниже. Первый pull может дать 404 (existence-hidden, грант ещё
  # не долетел) — poll-retry до 10× по 1.5s, затем финальный assert (#10 grant-latency).
`

// ct3LawfulNeighbours — соседние ЗАКОННЫЕ формы, на которых гейт обязан молчать.
// Каждая — длительность или токен `FGA` в законном смысле, то есть ровно то, из
// чего состоит корпус.
const ct3LawfulNeighbours = `# бюджет объявлен на связывании: cap 20 x 500 мс.
# poll-retry до 10x по 1.5s, затем финальный assert.
# owner-tuple едет register-outbox -> drainer -> IAM RegisterResource -> FGA reconciler.
# per-object FGA Write атомарен; deny-404 не течёт FGA-типом.
# _rya = retry_until_authorized(budget=80, interval_ms=600)  # 48s
# окно у другого домена ~0.7-2s — про registry это ничего не утверждает.
`

// (б)+(в) ЗАКОННЫЙ БЛИЗНЕЦ и соседние законные формы обязаны МОЛЧАТЬ, а перепись
// — показать, что обход их ПРОЧИТАЛ: молчание на непрочитанном не доказательство.
func TestCt3ProbeWindowInjection_LawfulTextIsSilent(t *testing.T) {
	root := writeCt3ProbeTree(t,
		ct3ProbeFile{rel: ct3InCorpus("scripts/dataplane-e2e.sh"), body: ct3LawfulReplacement},
		ct3ProbeFile{rel: ct3InCorpus("cases/registry-authz.py"), body: ct3LawfulNeighbours},
	)
	c, findings := ct3ProbeRun(t, root)

	if len(findings) != 0 {
		t.Fatalf("законный текст обязан молчать, получено: %v", findings)
	}
	if c.FilesRead != 2 {
		t.Fatalf("обход обязан прочитать оба файла, прочитано %d", c.FilesRead)
	}
	wantLines := strings.Count(ct3LawfulReplacement, "\n") + strings.Count(ct3LawfulNeighbours, "\n") + 2
	if c.LinesScanned != wantLines {
		t.Errorf("осмотрено строк %d, ожидалось %d — молчание на непрочитанном "+
			"не является доказательством", c.LinesScanned, wantLines)
	}
}

// (в2) ОДНА СТРОКА — ОДНО МЕСТО: строка, попадающая под ОБЕ формы, обязана быть
// посчитана один раз. Иначе один дефект считался бы дважды, и перепись врала бы
// в ту сторону, которую труднее заметить, — в сторону завышения.
func TestCt3ProbeWindowInjection_OneLineCountsOnce(t *testing.T) {
	root := writeCt3ProbeTree(t, ct3ProbeFile{
		rel:  ct3InCorpus("docs/RESULTS.md"),
		body: "- pull -> 200 (poll-retry to absorb FGA propagation, ~0.6-2s);\n",
	})
	c, findings := ct3ProbeRun(t, root)
	if len(c.Citations) != 1 || len(findings) != 1 {
		t.Fatalf("строка под обеими формами обязана дать одну цитату и одну находку, "+
			"получено %d/%d: %v", len(c.Citations), len(findings), findings)
	}
}

// (г) СУЖЕНИЕ ОБХОДА — свойство гейта, а не случайность. Тот же дефект вне
// корпуса registry обязан остаться невидимым: граница объявлена решением
// (шапка гейта), и проба обязана это решение закрепить — иначе следующий
// расширит обход, не заметив, что перечень владельцев окна тем самым сменился.
func TestCt3ProbeWindowInjection_ScopeIsTheRegistryProbeCorpus(t *testing.T) {
	defect := "# (FGA-пропагация ~0.6–2s) — ждём.\n"
	root := writeCt3ProbeTree(t,
		ct3ProbeFile{rel: "services/nlb/tests/newman/cases/load-balancer.py", body: defect},
		ct3ProbeFile{rel: "services/registry/internal/check/permission_map.go", body: defect},
		ct3ProbeFile{rel: "ui-future/shared/src/lib/use-operation.ts", body: defect},
		// Положительный контроль: без него молчание означало бы и «обход сужен»,
		// и «распознаватель умер».
		ct3ProbeFile{rel: ct3InCorpus("scripts/dataplane-e2e.sh"), body: defect},
	)
	c, findings := ct3ProbeRun(t, root)

	if c.FilesRead != 1 {
		t.Fatalf("обход обязан прочитать РОВНО файлы корпуса %s, прочитано %d",
			c.CorpusDir, c.FilesRead)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "scripts/dataplane-e2e.sh") {
		t.Fatalf("положительный контроль внутри корпуса обязан дать одну находку, "+
			"получено: %v", findings)
	}
}

// (д) ПУСТОЙ ОБХОД отличим от «нарушений нет»: находок ноль в обоих случаях, и
// различает их ТОЛЬКО перепись — поэтому она и печатается всегда, а прогон на
// пустом обходе падает проверкой предпосылки в самом гейте.
func TestCt3ProbeWindowInjection_EmptyWalkIsDistinguishable(t *testing.T) {
	c, findings := ct3ProbeRun(t, t.TempDir())
	if c.FilesRead != 0 || c.LinesScanned != 0 {
		t.Fatalf("на пустом дереве обход обязан быть пуст: файлов %d, строк %d",
			c.FilesRead, c.LinesScanned)
	}
	if len(findings) != 0 {
		t.Fatalf("на пустом дереве находок нет — их отсутствие и есть то, что "+
			"неотличимо от исправного без переписи; получено: %v", findings)
	}
	if c.FormsKnown == 0 {
		t.Fatal("формы записи обязаны быть известны и на пустом дереве")
	}
}
