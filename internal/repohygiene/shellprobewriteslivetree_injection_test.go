// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// shellprobewriteslivetree_injection_test.go — доказательство, что предикат
// shell-изоляции УМЕЕТ краснеть и УМЕЕТ молчать.
//
// Инъекция ведётся в обе стороны и по КАЖДОЙ форме записи отдельно, потому что
// предыдущая попытка закрыть этот класс текстом провалилась именно по формам:
// одну она видела, две — нет. Законный близнец у каждой формы СВОЙ и другой
// конструкции — копия чужого близнеца близнецом не является: она доказывала бы
// молчание на том, что уже проверено, и оставляла бы непроверенным то, ради чего
// написана.
package repohygiene

import (
	"embed"
	"strings"
	"testing"
)

//go:embed testdata/shell-live-write/*.sh.txt
var shellPriorRevisions embed.FS

// shellInjectionCase — пара «дефект / законный близнец» для одной формы записи.
type shellInjectionCase struct {
	form string // как называется форма — попадает в сообщение о провале
	bad  string // исходник с записью в ЖИВОЕ дерево
	// mark — подстрока строки, на которой стоит дефект. Номер строки не
	// выписывается числом: числа в фикстуре расходятся с фикстурой при первой же
	// правке, и расходятся молча.
	mark string
	good string // законный близнец ДРУГОЙ конструкции
	why  string // чем именно близнец законен — иначе «молчит» нечем объяснить
	// twinSilentBecause — ПОЧЕМУ близнец молчит, и это разные утверждения.
	// «Запись есть, цель не живая» и «записи нет вовсе» проверяются разными
	// счётчиками; свалив их в одно «просто молчит», контроль перестал бы
	// отличать законную конструкцию от неразобранной.
	twinSilentBecause string // "цель-не-живая" | "не-запись"
}

func shellInjectionCases() []shellInjectionCase {
	return []shellInjectionCase{
		{
			form:              "перенаправление вывода",
			twinSilentBecause: "цель-не-живая",
			bad: `#!/usr/bin/env bash
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VICTIM="$DEPLOY_ROOT/helm/umbrella/values.prod.yaml"
bak="$(mktemp)"; cp "$VICTIM" "$bak"
grep -v 'authz' "$bak" >"$VICTIM"
bash "$0" >/dev/null 2>&1
cp "$bak" "$VICTIM"
`,
			mark: `grep -v 'authz' "$bak" >"$VICTIM"`,
			good: `#!/usr/bin/env bash
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
cp -r "$REPO_ROOT/helm" "$WORK/helm"
grep -v 'authz' "$REPO_ROOT/helm/umbrella/values.prod.yaml" >"$WORK/helm/umbrella/values.prod.yaml"
`,
			why: "живое дерево ЧИТАЕТСЯ, пишется копия: у перенаправления цель — " +
				"последнее слово, и происхождение считается по НЕЙ, а не по любому живому слову",
		},
		{
			form:              "cp",
			twinSilentBecause: "цель-не-живая",
			bad: `#!/usr/bin/env bash
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHART="$ROOT/charts/kacho-iam/templates/deployment.yaml"
snapshot="$(mktemp)"; cp "$CHART" "$snapshot"
cp "$snapshot" "$CHART"
`,
			mark: `cp "$snapshot" "$CHART"`,
			good: `#!/usr/bin/env bash
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
cp -r "$ROOT/." "$WORK/"
ROOT="$WORK"
CHART="$ROOT/charts/kacho-iam/templates/deployment.yaml"
snapshot="$(mktemp)"; cp "$CHART" "$snapshot"
cp "$snapshot" "$CHART"
`,
			why: "переприсваивание СНИМАЕТ метку живого: после `ROOT=\"$WORK\"` то же " +
				"дословно выражение указывает в копию — так устроены починенные суиты дерева",
		},
		{
			form:              "sed -i",
			twinSilentBecause: "цель-не-живая",
			bad: `#!/usr/bin/env bash
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
values="$DEPLOY_ROOT/helm/umbrella/charts/kacho-geo/values.yaml"
sed -i 's/^\(    iamAuthz:\) true/\1 false/' "$values"
`,
			mark: `sed -i `,
			good: `#!/usr/bin/env bash
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
out="$(mktemp)"
sed 's/^\(    iamAuthz:\) true/\1 false/' \
    "$DEPLOY_ROOT/helm/umbrella/charts/kacho-geo/values.yaml" > "$out"
helm template kacho-umbrella "$DEPLOY_ROOT/helm/umbrella" -f "$out" >/dev/null
`,
			why: "правка НЕ на месте: тот же редактор, тот же живой файл на входе, " +
				"но результат уезжает во временный — флаг `-i` и есть весь дефект",
		},
		{
			form:              "встроенная нагрузка (python)",
			twinSilentBecause: "не-запись",
			bad: `#!/usr/bin/env bash
UMBRELLA="$(cd "$(dirname "$0")/../../helm/umbrella" && pwd)"
dep="$UMBRELLA/charts/kacho-iam/templates/deployment.yaml"
python3 - "$dep" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
open(p, "w").write(s.replace("enabled: true", "enabled: false"))
PY
`,
			mark: `python3 - "$dep"`,
			good: `#!/usr/bin/env bash
UMBRELLA="$(cd "$(dirname "$0")/../../helm/umbrella" && pwd)"
python3 - "$UMBRELLA/values.prod.yaml" <<'PY'
import sys, yaml
doc = yaml.safe_load(open(sys.argv[1]))
assert doc["authz"]["enabled"] is True
PY
`,
			why: "тот же запуск и тот же живой аргумент, но нагрузка ЧИТАЕТ: гейт " +
				"разбирает тело вставки, а не факт запуска интерпретатора с живым путём",
		},
		{
			form:              "цепочка помощников (живое значение через две границы)",
			twinSilentBecause: "цель-не-живая",
			bad: `#!/usr/bin/env bash
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
edit() {
  python3 - "$@" <<'PY'
import sys
open(sys.argv[1], "w").write("")
PY
}
run_with_injection() { local f="$1"; edit "$f"; }
if ! out="$(run_with_injection "$ROOT/helm/umbrella/Chart.yaml")"; then
  echo fail
fi
`,
			mark: `python3 - "$@"`,
			good: `#!/usr/bin/env bash
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
cp -r "$ROOT/." "$WORK/"
edit() {
  python3 - "$@" <<'PY'
import sys
open(sys.argv[1], "w").write("")
PY
}
run_with_injection() { local f="$1"; edit "$f"; }
if ! out="$(run_with_injection "$WORK/helm/umbrella/Chart.yaml")"; then
  echo fail
fi
`,
			why: "та же двухзвенная цепочка и тот же вызов из подстановки, но живое " +
				"значение в неё не входит — прослеживается ЗНАЧЕНИЕ, а не форма вызова",
		},
		{
			form:              "изменяющая команда git",
			twinSilentBecause: "не-запись",
			bad: `#!/usr/bin/env bash
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
git -C "$ROOT" add -f -- ui-future/shared/src/test/injected.test.ts
`,
			mark: `git -C "$ROOT" add`,
			good: `#!/usr/bin/env bash
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
git -C "$ROOT" status --porcelain
git -C "$ROOT" ls-files -- 'ui-future/**'
`,
			why: "чтение живого репозитория — работа половины гейтов дерева, и запрет " +
				"на него был бы запретом на них",
		},
		{
			form:              "запись, названная в комментарии НАГРУЗКИ",
			twinSilentBecause: "не-запись",
			bad: `#!/usr/bin/env bash
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
chart="$DEPLOY_ROOT/helm/umbrella/Chart.yaml"
python3 - "$chart" <<PLD
from pathlib import Path
import sys
p = Path(sys.argv[1])
p.write_text(p.read_text().replace("version:", "# version:"))
PLD
`,
			mark: `python3 - "$chart"`,
			good: `#!/usr/bin/env bash
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
python3 - "$DEPLOY_ROOT/helm/umbrella/Chart.yaml" <<PLD
import sys
# Правка на месте запрещена: open(p, "w") и Path(p).write_text() оставили бы
# инъекцию в живом дереве, если прогон снимут до возврата.
print(open(sys.argv[1]).read())
PLD
`,
			why: "признаки записи стоят в ПОЯСНЕНИИ внутри самой нагрузки: гейт " +
				"срезает комментарии чужого языка так же, как комментарии оболочки",
		},
		{
			form:              "упоминание формы в комментарии",
			twinSilentBecause: "цель-не-живая",
			bad: `#!/usr/bin/env bash
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cp "$(mktemp)" "$ROOT/helm/umbrella/values.dev.yaml"
`,
			mark: `cp "$(mktemp)" "$ROOT`,
			good: `#!/usr/bin/env bash
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# Запрещено: cp "$snapshot" "$ROOT/helm/umbrella/values.dev.yaml" — возврат
# последней строкой тела не переживает снятия прогона. Инъекция идёт в копию:
WORK="$(mktemp -d)"
cp "$ROOT/helm/umbrella/values.dev.yaml" "$WORK/values.dev.yaml"  # было: cp "$WORK/values.dev.yaml" "$ROOT/helm/umbrella/values.dev.yaml"
`,
			why: "запрещённая форма стоит в ПОЯСНЕНИИ к запрету — и отдельной " +
				"строкой, и хвостом законной команды: без срезания комментариев хвост " +
				"дописался бы к аргументам `cp` и подменил бы его цель на живую",
		},
	}
}

// TestShellProbeWriteInjectionRedsOnEveryFormAndStaysSilentOnItsTwin — инъекция
// в обе стороны по каждой форме.
func TestShellProbeWriteInjectionRedsOnEveryFormAndStaysSilentOnItsTwin(t *testing.T) {
	cases := shellInjectionCases()

	// Предикат снятия #724 называет ЧЕТЫРЕ формы поимённо. Проверка «форм не
	// меньше четырёх» этого не удерживает: набор из четырёх перенаправлений ей
	// удовлетворяет и не доказывает ничего сверх одной формы.
	need := map[string]bool{
		"перенаправление вывода": false,
		"cp":     false,
		"sed -i": false,
		"встроенная нагрузка (python)": false,
	}
	for _, c := range cases {
		if _, ok := need[c.form]; ok {
			need[c.form] = true
		}
	}
	for form, have := range need {
		if !have {
			t.Fatalf("форма %q не представлена в наборе — предикат снятия #724 называет "+
				"её поимённо, и без неё инъекция доказывает меньше, чем заявляет", form)
		}
	}
	for _, c := range cases {
		t.Run(c.form, func(t *testing.T) {
			rel := "deploy/tests/helm/injected-test.sh"

			// (а) ДЕФЕКТ обязан покраснеть — и назвать координату.
			bad, badCensus := auditShellProbeWritesToLiveTree(
				map[string]string{rel: c.bad}, nil)
			if len(bad) == 0 {
				t.Fatalf("форма %q НЕ поймана: предикат промолчал на записи в живое дерево.\n"+
					"перепись: %+v\nисходник:\n%s", c.form, badCensus, c.bad)
			}
			wantLine := shellLineOf(c.bad, c.mark)
			if wantLine == 0 {
				t.Fatalf("метка %q не найдена в фикстуре — сама фикстура разошлась с "+
					"тем, что она проверяет", c.mark)
			}
			hit := false
			for _, f := range bad {
				if f.Line == wantLine {
					hit = true
				}
			}
			if !hit {
				t.Errorf("форма %q поймана, но НЕ НА ТОЙ строке: ждали %d (%q), получили%s\n"+
					"координата — половина находки: без неё чинить придётся поиском",
					c.form, wantLine, strings.TrimSpace(c.mark), shellFindingsBrief(bad))
			}

			// (б) ЗАКОННЫЙ БЛИЗНЕЦ обязан молчать.
			good, goodCensus := auditShellProbeWritesToLiveTree(
				map[string]string{rel: c.good}, nil)
			if len(good) != 0 {
				t.Errorf("форма %q: ложное срабатывание на законной конструкции (%s):%s\n"+
					"перепись: %+v\nисходник:\n%s",
					c.form, c.why, shellFindingsBrief(good), goodCensus, c.good)
			}
			// Молчание обязано быть СОДЕРЖАТЕЛЬНЫМ, и содержание у двух видов
			// близнецов разное. Без этой развилки «молчит» означало бы и
			// «законен», и «не разобран» — то есть не означало бы ничего.
			if goodCensus.Commands == 0 {
				t.Fatalf("форма %q: законный близнец не разобран вовсе (команд 0) — "+
					"его молчание ничего не доказывает", c.form)
			}
			if badCensus.Writes == 0 {
				t.Fatalf("форма %q: у ДЕФЕКТА не распознано ни одного места записи "+
					"(перепись %+v) — пара «дефект/близнец» различается не тем, чем заявлено",
					c.form, badCensus)
			}
			switch c.twinSilentBecause {
			case "цель-не-живая":
				if goodCensus.Writes == 0 {
					t.Errorf("форма %q: близнец объявлен записью с неживой целью, но мест "+
						"записи у него 0 — он прошёл МИМО распознавания, а не через него",
						c.form)
				}
				if goodCensus.Live != 0 {
					t.Errorf("форма %q: у близнеца живых целей %d при нуле находок — "+
						"молчание получено отсевом, а не происхождением", c.form, goodCensus.Live)
				}
			case "не-запись":
				if goodCensus.Writes != 0 {
					t.Errorf("форма %q: близнец объявлен НЕ записью, а мест записи у него %d",
						c.form, goodCensus.Writes)
				}
			default:
				t.Fatalf("форма %q: не сказано, почему близнец молчит", c.form)
			}
		})
	}
}

// TestShellProbeWriteControlOnPriorRevisions — контроль на ПРЕЖНИХ редакциях трёх
// суит, чинившихся в #696.
//
// Это единственная часть доказательства, которую нельзя заменить синтетикой:
// синтетику пишет тот же человек, что и предикат, и она наследует его слепые
// зоны. Прежние редакции написаны до предиката и о нём не знают.
//
// Редакции лежат ФАЙЛАМИ с расширением `.sh.txt`, а не `.sh`, и это не
// косметика: файл `.sh` попал бы в корпус этого же гейта (и в обход
// самопроверок дерева) — фикстура пробы стала бы источником находок о самой
// себе. Из истории они не читаются: конвейер берёт дерево одним слоем, и
// `git show <ревизия>:<путь>` там не разрешается.
//
// # Одно отступление от дословности — названо, а не умолчано
//
// ПЕРВАЯ строка каждого снимка заменена комментарием-пометкой: в оригинале там
// стояла строка с интерпретатором, а гейт исполняемости этого дерева
// (`execbit_test.go`) справедливо требует бита исполнения от КАЖДОГО файла,
// который ею начинается. Ставить бит значило бы объявить снимок программой,
// которой он не является. Замена сохраняет НОМЕРА СТРОК (строка на строку) и
// ничего не меняет для предиката: и `#!`, и `#` — комментарий оболочки, разбор
// снимает оба одинаково. Восстанавливать строку обратно не надо: она вернёт
// красное на соседнем гейте и ничего не добавит здесь.
func TestShellProbeWriteControlOnPriorRevisions(t *testing.T) {
	entries, err := shellPriorRevisions.ReadDir("testdata/shell-live-write")
	if err != nil {
		t.Fatalf("прежние редакции не прочитаны: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("прежних редакций %d, а чинилось три (#696): контроль «3 из 3» "+
			"беспредметен, если корпус контроля не тот", len(entries))
	}
	caught := 0
	for _, e := range entries {
		body, err := shellPriorRevisions.ReadFile("testdata/shell-live-write/" + e.Name())
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		rel := "deploy/tests/helm/" + strings.TrimSuffix(e.Name(), ".prior.sh.txt") + ".sh"
		f, c := auditShellProbeWritesToLiveTree(map[string]string{rel: string(body)}, nil)
		if len(f) == 0 {
			t.Errorf("прежняя редакция %s НЕ поймана — перепись: %+v", e.Name(), c)
			continue
		}
		caught++
		t.Logf("%s: находок %d%s", e.Name(), len(f), shellFindingsBrief(f))
	}
	if caught != len(entries) {
		t.Errorf("поймано %d из %d прежних редакций. Текстовый предикат, отвергнутый "+
			"в #724, давал 1 из 3; всё, что меньше 3 из 3, — то же самое другими словами",
			caught, len(entries))
	}
}

// TestShellProbeWriteDeclaredSinkExpiresWithItsDeclaration — отсев по объявленной
// свалке проверяется В ОБЕ СТОРОНЫ на ОДНОМ И ТОМ ЖЕ исходнике.
//
// Разница между зелёным и красным здесь ровно одна — факт дерева. Значит
// послабление истекает само: снимут объявление, и та же строка станет находкой.
func TestShellProbeWriteDeclaredSinkExpiresWithItsDeclaration(t *testing.T) {
	rel := "tests/authz-fixtures/setup.sh"
	src := `#!/usr/bin/env bash
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/out}"
echo "production" > "$OUT_DIR/seed-posture"
`
	const sink = "tests/authz-fixtures/out/seed-posture"

	withSink, c1 := auditShellProbeWritesToLiveTree(map[string]string{rel: src},
		func(p string) bool { return p == sink })
	if len(withSink) != 0 {
		t.Errorf("объявленная деревом свалка покрашена:%s", shellFindingsBrief(withSink))
	}
	if c1.Live != 1 || c1.Declared != 1 {
		t.Errorf("перепись не различила отсев: живых целей %d, из них по свалке %d "+
			"(ждали 1 и 1) — без этой пары «ноль находок» неотличимо от «не разобрано»",
			c1.Live, c1.Declared)
	}

	noSink, c2 := auditShellProbeWritesToLiveTree(map[string]string{rel: src},
		func(string) bool { return false })
	if len(noSink) != 1 {
		t.Fatalf("без объявления та же запись обязана стать находкой, получено %d:%s",
			len(noSink), shellFindingsBrief(noSink))
	}
	if c2.Declared != 0 {
		t.Errorf("без объявления отсев всё равно сработал (%d) — послабление не истекает",
			c2.Declared)
	}
}

// TestShellProbeWriteCensusCountersCanBeZero — предпосылки гейта не вакуумны.
//
// Гейт по дереву падает, если производителей живого корня, мест записи либо
// встроенных нагрузок ноль. Утверждение стоит чего-то, только если эти счётчики
// СПОСОБНЫ обнулиться: иначе это условие, которое нельзя нарушить, то есть
// проверка формы без содержания.
func TestShellProbeWriteCensusCountersCanBeZero(t *testing.T) {
	_, c := auditShellProbeWritesToLiveTree(map[string]string{
		"deploy/tests/helm/plain-test.sh": `#!/usr/bin/env bash
set -euo pipefail
helm template kacho-umbrella ./helm/umbrella
echo "ok"
`,
	}, nil)
	if c.Scripts != 1 {
		t.Fatalf("скрипт не разобран: %+v", c)
	}
	if c.Producers != 0 || c.Payloads != 0 || c.Writes != 0 {
		t.Errorf("счётчики предпосылок не обнуляются на скрипте без них: %+v — "+
			"значит проверка предпосылок гейта не может покраснеть никогда", c)
	}
	if c.Commands == 0 {
		t.Errorf("команд осмотрено 0 при непустом скрипте — разбор сломан, и все " +
			"нули выше означают «не читали», а не «не нашли»")
	}
}

// shellLineOf — номер строки, содержащей подстроку (1 — первая, 0 — не найдено).
func shellLineOf(src, sub string) int {
	for i, ln := range strings.Split(src, "\n") {
		if strings.Contains(ln, sub) {
			return i + 1
		}
	}
	return 0
}

// TestShellProbeCorpusTakesProbesByRoleNotOnlyByPlace — отбор корпуса проверяется
// в ОБЕ стороны на именах, ВЗЯТЫХ ИЗ ДЕРЕВА.
//
// Отбор только по каталогу `tests/` оставлял снаружи 21 скрипт, пробами
// являющихся по существу, — включая два, вносящих дефект НАМЕРЕННО. Гейт,
// читающий 78 % своего предмета и печатающий «ноль находок», отличается от
// отвергнутого текстового предиката (треть предмета) долей, а не классом.
//
// Отрицательная половина здесь не украшение: без неё тот же контроль прошёл бы у
// предиката, возвращающего `true` на что угодно, — и корпус вобрал бы в себя
// производителей дерева (генераторы каталога прав, посевы, установщик хука),
// чья работа в том и состоит, чтобы писать файлы.
func TestShellProbeCorpusTakesProbesByRoleNotOnlyByPlace(t *testing.T) {
	probes := []string{
		"deploy/tests/helm/podtemplate-annotation-single-owner-test.sh", // место
		"deploy/scripts/assert-production-posture.sh",                   // утверждение о стенде
		"deploy/scripts/inject-admin-hop-defects.sh",                    // вноситель дефекта
		"scripts/hooks/prepush-groups-inject.sh",                        // он же, другой формой имени
		"deploy/scripts/run-gate-self-tests.sh",                         // гейт над гейтами
		".github/scripts/check-pinned-tools.sh",                         // проверка пинов
		"services/vpc/tools/audit-list-filter.sh",                       // ревизия фильтра
		"services/vpc/deploy/render-guard.sh",                           // страж рендера
		"tools/carrydrift/judge.sh",                                     // судья дрейфа
	}
	tools := []string{
		"gateway/scripts/gen-permission-catalog.sh", // производитель артефакта дерева
		"deploy/scripts/seed-geo-baseline.sh",       // посев стенда
		"scripts/hooks/install.sh",                  // установщик хука
		"scripts/ci-local.sh",                       // прогонщик
		"deploy/kind/create-cluster.sh",             // подъём кластера
		"ui-future/proxies.sh",
	}
	for _, rel := range probes {
		if !isShellProbePath(rel) {
			t.Errorf("%s пробой НЕ признан, а является ею по роли — значит не читается вовсе, "+
				"и «ноль находок» по нему получено даром", rel)
		}
	}
	for _, rel := range tools {
		if isShellProbePath(rel) {
			t.Errorf("%s признан пробой, хотя это производитель дерева: писать файлы — его "+
				"работа, и корпус, вобравший его, потребует списка исключений", rel)
		}
	}

	// Имена взяты из дерева, а не придуманы: разойдись они с ним — контроль
	// проверял бы соглашение, которого нет.
	root := repoRoot(t)
	tracked := map[string]bool{}
	for _, rel := range trackedPaths(t, root) {
		tracked[rel] = true
	}
	for _, rel := range append(append([]string{}, probes...), tools...) {
		if !tracked[rel] {
			t.Errorf("%s в дереве не отслеживается — фикстура пережила свой предмет", rel)
		}
	}
}

// TestShellProbeDeclaredSinkCoversDirectoryDeclarations — объявление КАТАЛОГА
// читается объявлением, и это проверяется на настоящем дереве.
//
// `git check-ignore` на пути без косой черты отвечает «нет» правилу вида
// `tmpcharts-*/`: он не знает, каталог перед ним или файл. Промолчав об этом,
// гейт превратил бы объявленную деревом свалку в вечную находку — а её пришлось
// бы гасить списком исключений, то есть заменить факт дерева памятью автора.
func TestShellProbeDeclaredSinkCoversDirectoryDeclarations(t *testing.T) {
	root := repoRoot(t)
	sink := shellDeclaredSink(t, root)

	// (а) объявленный каталог — свалка.
	if !sink("deploy/helm/umbrella/tmpcharts-123") {
		t.Error("объявленный деревом каталог подкачки свалкой не признан: правило " +
			"`deploy/.gitignore` объявляет `helm/umbrella/tmpcharts-*/`. " +
			"Проверь, что объявление ещё на месте — если его сняли, чинить надо не гейт")
	}
	// (б) отслеживаемый файл — НЕ свалка, даже если спросить его каталогом.
	if sink("deploy/helm/umbrella/values.prod.yaml") {
		t.Error("отслеживаемый файл признан свалкой — вопрос «а если это каталог?» " +
			"размыл предикат, и любая правка живого профиля стала бы законной")
	}
	// (в) необъявленный каталог — НЕ свалка. Без этой стороны (а) прошло бы
	// у предиката, отвечающего «да» на всё, что кончается косой чертой.
	if sink("deploy/helm/umbrella") {
		t.Error("необъявленный каталог признан свалкой — предикат отвечает по форме " +
			"вопроса, а не по факту дерева")
	}
}
