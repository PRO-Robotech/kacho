// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «shell-суита не пишет в дерево, из которого
// запущена» СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Инъекция идёт в обе стороны и настоящими формами из дерева. Формы записи —
// ЧЕТЫРЕ, и это не украшение: каждая ловится своей веткой предиката, а слить их
// в одно утверждение «хоть где-то нашлось» нельзя — тогда отключение любой одной
// ветки осталось бы зелёным.
//
//	живой корень → перенаправление `>`                 → краснеет, называет путь;
//	живой корень → `cp` назначением                    → краснеет (ветка другая);
//	живой корень → `sed -i`                            → краснеет (ветка третья);
//	живой корень → встроенный python, `open(p,"w")`    → краснеет (ветка четвёртая);
//	живой корень → изменяющая `git` в живом дереве     → краснеет;
//	живой корень → помощник → `"$@"` → python          → краснеет (три шага);
//
//	КОПИЯ дерева (`mktemp -d`) теми же четырьмя формами → молчит;
//	живой корень → встроенный python, `open(p)`        → молчит (РЕЖИМ, не имя);
//	живой корень → чтение (`cat`, `helm template`)     → молчит;
//	изменяющая `git` в СВОЁМ репозитории               → молчит;
//	запрещённая форма в комментарии                    → молчит (читается код);
//	запись по пути, объявленному деревом артефактом    → молчит (и это истекает).
//
// # Почему близнец обязан быть ДРУГОЙ конструкцией
//
// Копия дефекта с переименованной переменной близнецом не является: она
// проверяет то же самое место предиката. Здесь близнец каждой формы отличается
// ПРОИСХОЖДЕНИЕМ цели (временный каталог против корня дерева) либо РЕЖИМОМ
// доступа (чтение против записи) — то есть ровно тем признаком, по которому
// предикат и должен различать.
package repohygiene

import (
	"os"
	"strings"
	"testing"
)

// Производитель корня живого дерева. Форма снята с дерева дословно: восхождение
// от файла САМОГО скрипта. Имя переменной намеренно не `REPO_ROOT` и не
// `DEPLOY_ROOT` — предикат выводит производителя из ТЕЛА подстановки, а не из
// имени, и это здесь проверяется заодно.
const synthShellProducer = `#!/usr/bin/env bash
set -euo pipefail
TREE_TOP="$(cd "$(dirname "$0")/../.." && pwd)"
CHART="$TREE_TOP/helm/umbrella/charts/kacho-geo/templates/deployment.yaml"
`

// ДЕФЕКТ 1 — перенаправление в файл живого дерева. Форма прежней редакции
// суиты `image-rollout-binding-test.sh`.
const synthShellRedirect = synthShellProducer + `
bak="$(mktemp)"; cp "$CHART" "$bak"
grep -v 'kacho.cloud/image-id' "$bak" >"$CHART"
`

// ДЕФЕКТ 2 — `cp`, у которого НАЗНАЧЕНИЕ в живом дереве. Ветка другая: путь
// стоит последним аргументом, а не за оператором перенаправления.
const synthShellCopyBack = synthShellProducer + `
bak="$(mktemp)"
cp "$bak" "$CHART"
`

// ДЕФЕКТ 3 — правка на месте. Ветка третья: цель узнаётся по флагу `-i`, а
// первый аргумент — программа, а не путь.
// Здесь же стоит here-string (`<<<`): он похож на heredoc первыми двумя
// символами, и разбор, не отличающий их, съедает ОСТАТОК скрипта как «тело» —
// вместе с дефектом ниже. Тогда молчание гейта означает не чистоту, а
// непрочитанное. Форма настоящая: в дереве `read -ra … <<<` встречается в
// суитах развёртки.
const synthShellSedInPlace = synthShellProducer + `
IFS='/' read -ra parts <<<"$CHART"
echo "сегментов в пути: ${#parts[@]}"
sed -i 's/replicas: 1/replicas: 2/' "$CHART"
`

// ДЕФЕКТ 4 — встроенный интерпретатор. Форма прежних редакций суит
// `networkpolicy-egress-test.sh` и `podtemplate-annotation-single-owner-test.sh`;
// именно её текстовый предикат по shell не видит вовсе.
const synthShellPythonWrite = synthShellProducer + `
python3 - "$CHART" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
open(p, 'w').write(s.replace('enabled: true', 'enabled: false'))
PY
`

// ДЕФЕКТ 5 — изменяющая git-команда против живого репозитория.
const synthShellGitMutate = synthShellProducer + `
git -C "$TREE_TOP" add -f -- helm/umbrella/charts/kacho-geo/templates/deployment.yaml
`

// ДЕФЕКТ 6 — три шага: вызов помощника с живым путём → локальная переменная →
// передача `"$@"` дальше → запись встроенным интерпретатором. Дословная форма
// прежней редакции `podtemplate-annotation-single-owner-test.sh`; без прохода до
// неподвижной точки этот путь невидим ЦЕЛИКОМ.
const synthShellThreeHops = synthShellProducer + `
inject_at() {
  python3 - "$@" <<'PY'
import sys
path, extra = sys.argv[1:3]
src = open(path).read()
open(path, "w").write(src + extra)
PY
}

run_with_injection() {
  local file="$1" bak
  bak="$(mktemp)"; cp "$file" "$bak"
  if ! INJ_ERR="$(inject_at "$@" 2>&1)"; then
    cp "$bak" "$file"; return 3
  fi
  cp "$bak" "$file"
}

if ! run_with_injection "$CHART" '  annotation: injected'; then
  echo "инъекция не удалась"
fi
`

// ДЕФЕКТ 7 — запись стоит на строке, ЗАКРЫВАЮЩЕЙ многострочную программу awk.
// Тело программы живёт в одинарных кавычках и переживает перевод строки; внутри
// него `>` — сравнение, а не перенаправление. Разбор, начинающий каждую строку
// «вне кавычек», выдумывает здесь запись в файл `length(best))` и одновременно
// теряет настоящую — ту, что за закрывающей кавычкой. Форма настоящая: снята с
// проверки пинов инструментов в дереве.
const synthShellAwkThenWrite = synthShellProducer + `
awk '
  {
    best = ""
    for (i = 1; i <= NF; i++) {
      if (length($i) > length(best)) best = $i
    }
    print best
  }
' "$CHART" >"$CHART"
`

// ЗАКОННЫЙ БЛИЗНЕЦ 6 — тот же встроенный python по тому же живому пути, режим
// `"rb"`: это ЧТЕНИЕ двоичного файла. Различать надо режим по существу, а не по
// первой букве — иначе гейт красит собственные проверки дерева, которые именно
// так и читают файлы.
const synthShellPythonReadBinary = synthShellProducer + `
python3 - "$CHART" <<'PROG'
import sys
with open(sys.argv[1], "rb") as fh:
    blob = fh.read()
print(len(blob))
PROG
`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — те же ЧЕТЫРЕ формы записи, но против КОПИИ дерева.
// Дословная форма исправленного кода задачи #696: копия собирается во временный
// каталог, гейт прогоняется из неё, живое дерево только читается.
const synthShellWorkCopy = synthShellProducer + `
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cp -r "$TREE_TOP/." "$WORK/"
REL="helm/umbrella/charts/kacho-geo/templates/deployment.yaml"

grep -v 'kacho.cloud/image-id' "$TREE_TOP/$REL" >"$WORK/$REL"
cp "$TREE_TOP/$REL" "$WORK/$REL"
sed -i 's/replicas: 1/replicas: 2/' "$WORK/$REL"
python3 - "$WORK/$REL" <<'PY'
import sys
p = sys.argv[1]
open(p, 'w').write(open(p).read())
PY
`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — тот же встроенный интерпретатор по тому же живому пути,
// но ЧИТАЮЩИЙ. Различает РЕЖИМ, а не имя интерпретатора: запрет на чтение был бы
// запретом на сами суиты — они только этим и заняты.
const synthShellPythonRead = synthShellProducer + `
python3 - "$CHART" <<'PY'
import sys, yaml
docs = [d for d in yaml.safe_load_all(open(sys.argv[1])) if d]
print(len(docs))
PY
cat "$CHART" >/dev/null
helm template kacho-umbrella "$TREE_TOP/helm/umbrella" >/dev/null
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — изменяющая git-команда против СВОЕГО репозитория.
const synthShellOwnRepo = synthShellProducer + `
WORK="$(mktemp -d)"
git -C "$WORK" init -q
cp "$CHART" "$WORK/deployment.yaml"
git -C "$WORK" add -f -- deployment.yaml
`

// ЗАКОННЫЙ БЛИЗНЕЦ 4 — запрещённая форма в КОММЕНТАРИИ и в теле heredoc, которое
// никакой интерпретатор не исполняет. Гейт, краснеющий на собственном
// объяснении запрета, снимут первым.
const synthShellProse = synthShellProducer + `
# Так делать нельзя:
#   grep -v x "$CHART" >"$CHART"
#   cp "$bak" "$CHART"
#   python3 - "$CHART" <<PY ... open(p,"w") ... PY
cat <<'DOC'
Пример неверной суиты:
  sed -i 's/a/b/' "$CHART"
  git -C "$TREE_TOP" add -- .
DOC
`

// ЗАКОННЫЙ БЛИЗНЕЦ 5 — запись по пути, который САМО ДЕРЕВО объявило артефактом.
// Отчёты прогона, каталог `out/`: прерывание оставляет там мусор, который никто
// не читает как содержимое дерева. Послабление истекает само — снимут строку из
// `.gitignore`, и то же место станет находкой.
const synthShellArtifact = synthShellProducer + `
mkdir -p "$TREE_TOP/out"
printf 'ok\n' >"$TREE_TOP/out/summary.txt"
cp "$CHART" "$TREE_TOP/out/chart-copy.yaml"
`

// synthArtifactRule — «объявление артефакта» для инъекции: тот же вопрос, что
// гейт по дереву задаёт git-у, только с известным ответом.
func synthArtifactRule(rel string) bool {
	return strings.HasPrefix(rel, "out/") || strings.Contains(rel, "/out/")
}

func TestShellProbeWriteGateSeparatesLiveWritesFromReads(t *testing.T) {
	const (
		defRedirect  = "deploy/tests/helm/synth-redirect-test.sh"
		defCopyBack  = "deploy/tests/helm/synth-copy-back-test.sh"
		defSed       = "deploy/tests/helm/synth-sed-test.sh"
		defPython    = "deploy/tests/helm/synth-python-test.sh"
		defGit       = "deploy/tests/helm/synth-git-test.sh"
		defThreeHops = "deploy/tests/helm/synth-three-hops-test.sh"
		defAwkThen   = "deploy/tests/helm/synth-awk-then-write-test.sh"
		twinWork     = "deploy/tests/helm/synth-work-copy-test.sh"
		twinRead     = "deploy/tests/helm/synth-python-read-test.sh"
		twinOwnRepo  = "deploy/tests/helm/synth-own-repo-test.sh"
		twinProse    = "deploy/tests/helm/synth-prose-test.sh"
		twinArtifact = "deploy/tests/helm/synth-artifact-test.sh"
		twinReadBin  = "deploy/tests/helm/synth-python-read-binary-test.sh"
	)

	findings, census := auditShellProbeWritesToLiveTree(map[string]string{
		defRedirect:  synthShellRedirect,
		defCopyBack:  synthShellCopyBack,
		defSed:       synthShellSedInPlace,
		defPython:    synthShellPythonWrite,
		defGit:       synthShellGitMutate,
		defThreeHops: synthShellThreeHops,
		defAwkThen:   synthShellAwkThenWrite,
		twinWork:     synthShellWorkCopy,
		twinRead:     synthShellPythonRead,
		twinOwnRepo:  synthShellOwnRepo,
		twinProse:    synthShellProse,
		twinArtifact: synthShellArtifact,
		twinReadBin:  synthShellPythonReadBinary,
	}, synthArtifactRule)

	got := map[string][]shellWriteFinding{}
	for _, f := range findings {
		got[f.File] = append(got[f.File], f)
	}

	// ── краснеет: четыре формы записи, каждая своей веткой ──────────────────
	// Синтетические суиты лежат по адресу настоящих (`deploy/tests/helm/`) и
	// восходят на два каталога — то есть корнем считают `deploy/`. Ожидаемый
	// путь выписан от него: цель гейта — назвать место В ДЕРЕВЕ, и проверять
	// надо именно это, а не сам факт непустой строки.
	const chart = "deploy/helm/umbrella/charts/kacho-geo/templates/deployment.yaml"

	red := []struct {
		file, what, why string
	}{
		{defRedirect, "перенаправление >",
			"перенаправление в файл дерева — та самая форма, которую текстовый предикат " +
				"ловил, и единственная из четырёх, что ему по силам"},
		{defCopyBack, "cp (назначение)",
			"возврат резервной копии поверх файла дерева: ветка «цель последним " +
				"аргументом», а не «цель за оператором»"},
		{defSed, "sed -i",
			"правка на месте: цель узнаётся по флагу, а первый аргумент — программа"},
		{defPython, "python3 (запись в теле)",
			"запись внутри встроенного интерпретатора — форма, которой написаны две " +
				"из трёх найденных суит и которой текстовый поиск не видит вовсе"},
	}
	for _, r := range red {
		t.Run("краснеет: "+r.what, func(t *testing.T) {
			fs, ok := got[r.file]
			if !ok {
				t.Fatalf("%s НЕ пойман — %s", r.what, r.why)
			}
			if fs[0].What != r.what {
				t.Errorf("вид записи назван неверно: %q вместо %q", fs[0].What, r.what)
			}
			if fs[0].Line == 0 {
				t.Errorf("вердикт без строки не приводит к правке: %+v", fs[0])
			}
			if fs[0].Path != chart {
				t.Errorf("цель названа неверно: %q вместо %q — вердикт, не называющий "+
					"места, заставляет искать его руками", fs[0].Path, chart)
			}
		})
	}

	t.Run("краснеет: запись за закрывающей кавычкой программы awk", func(t *testing.T) {
		fs, ok := got[defAwkThen]
		if !ok {
			t.Fatal("перенаправление на строке, закрывающей многострочную программу awk, " +
				"НЕ поймано — значит состояние кавычек не переживает перевод строки, и " +
				"тело чужой программы читается как shell")
		}
		if len(fs) != 1 {
			t.Errorf("находок %d, ожидалась одна: `>` ВНУТРИ программы awk — сравнение, "+
				"а не запись, и выдуманная координата тут хуже пропущенной: %+v", len(fs), fs)
		}
		if fs[0].Path != chart {
			t.Errorf("цель названа неверно: %q вместо %q — путь собран из текста чужой "+
				"программы", fs[0].Path, chart)
		}
	})

	t.Run("краснеет: изменяющая git-команда в живом дереве", func(t *testing.T) {
		fs, ok := got[defGit]
		if !ok {
			t.Fatal("`git -C <живой корень> add` НЕ пойман — фантомная запись в индексе " +
				"осталась бы невидимой ровно для тех гейтов, что берут состав у индекса")
		}
		if fs[0].What != "git add" {
			t.Errorf("подкоманда названа неверно: %q", fs[0].What)
		}
	})

	t.Run("краснеет: три шага до записи", func(t *testing.T) {
		fs, ok := got[defThreeHops]
		if !ok {
			t.Fatal("живой путь, уехавший через помощника и `\"$@\"` в интерпретатор, " +
				"НЕ пойман — а это дословная форма прежней редакции суиты " +
				"podtemplate; без прохода до неподвижной точки путь невидим целиком")
		}
		var sawBody, sawCopy bool
		for _, f := range fs {
			switch f.What {
			case "python3 (запись в теле)":
				sawBody = true
			case "cp (назначение)":
				sawCopy = true
			}
		}
		if !sawBody || !sawCopy {
			t.Errorf("из трёхшагового пути найдено не всё (интерпретатор=%v, cp=%v): %+v",
				sawBody, sawCopy, fs)
		}
	})

	// ── молчит: законные близнецы ───────────────────────────────────────────
	silent := []struct {
		file, why string
	}{
		{twinWork, "те же четыре формы против КОПИИ дерева — дословная форма " +
			"исправленного кода задачи #696; гейт, краснеющий здесь, объявляет дефектом " +
			"собственный фикс и будет снят первым же прогоном"},
		{twinRead, "тот же интерпретатор по тому же пути, но ЧИТАЮЩИЙ, плюс `cat` и " +
			"`helm template` — запрет на чтение был бы запретом на сами суиты"},
		{twinOwnRepo, "изменяющая git-команда против СВОЕГО репозитория во временном каталоге"},
		{twinProse, "запрещённые формы в комментарии и в неисполняемом heredoc — гейт, " +
			"краснеющий на собственном объяснении запрета, снимут первым"},
		{twinReadBin, "тот же интерпретатор по тому же пути в режиме `\"rb\"` — это " +
			"ЧТЕНИЕ двоичного файла, и различать режим надо по существу, а не по первой " +
			"букве: иначе гейт красит собственные проверки дерева"},
		{twinArtifact, "запись по пути, объявленному деревом артефактом: каталог отчётов " +
			"ровно для того и заведён, а корпуса такая запись не портит"},
	}
	for _, tw := range silent {
		t.Run("молчит: "+tw.file, func(t *testing.T) {
			if fs, ok := got[tw.file]; ok {
				t.Errorf("объявлено находкой (%+v) — %s", fs, tw.why)
			}
		})
	}

	t.Run("перепись сама различает виды", func(t *testing.T) {
		if census.Files != 13 {
			t.Errorf("разобрано скриптов %d, ожидалось 13 — часть корпуса не прочитана, "+
				"и молчание по ней ничего не значит", census.Files)
		}
		if census.Producers == 0 {
			t.Error("выводов живого корня 0 — источник происхождения не распознан, и " +
				"«ноль находок» неотличимо от «ноль прочитанного»")
		}
		if census.Bodies == 0 {
			t.Error("тел встроенных интерпретаторов 0 — разбор heredoc сломан")
		}
		if census.Artifacts == 0 {
			t.Error("записей по объявленному артефактом пути 0 — правило артефактов не " +
				"исполнялось вовсе, и молчание близнеца получено по другой причине")
		}
		if census.Tainted <= len(findings) {
			t.Errorf("помеченных %d при находках %d — правило артефактов ничего не "+
				"вычло, значит близнец молчит не по нему", census.Tainted, len(findings))
		}
	})
}

// TestShellProbeWriteGateNeedsAProducerToSayAnything — предпосылка предиката.
//
// Происхождение ведётся ОТ производителя корня живого дерева. Убери его — и тот
// же самый дефект перестаёт находиться. Проба закрепляет это как СВОЙСТВО, а не
// как случайность: иначе «ноль находок» на дереве без производителей читалось бы
// как чистота, и гейт молчал бы именно там, где он сломан.
func TestShellProbeWriteGateNeedsAProducerToSayAnything(t *testing.T) {
	const rel = "deploy/tests/helm/synth-redirect-test.sh"

	with, _ := auditShellProbeWritesToLiveTree(map[string]string{rel: synthShellRedirect}, nil)
	if len(with) == 0 {
		t.Fatal("с производителем дефект не найден — предикат мёртв")
	}

	// Тот же текст, но корень приходит извне: производителя в скрипте нет.
	without := strings.Replace(synthShellRedirect,
		`TREE_TOP="$(cd "$(dirname "$0")/../.." && pwd)"`,
		`TREE_TOP="${TREE_TOP:?корень задаёт вызывающий}"`, 1)
	got, census := auditShellProbeWritesToLiveTree(map[string]string{rel: without}, nil)
	if census.Producers != 0 {
		t.Fatalf("производитель насчитан там, где его нет: %+v", census)
	}
	if len(got) != 0 {
		t.Fatalf("без производителя найдено %d — предикат ведёт происхождение не от "+
			"того, от чего заявлено: %+v", len(got), got)
	}
	// Отсюда и требование к гейту по дереву: на нуле производителей он ПАДАЕТ,
	// а не молчит — иначе исчезновение источника происхождения выглядело бы как
	// чистое дерево.
}

// TestShellProbeCorpusTakesBothConventions — корпус собирается по ДВУМ признакам.
//
// Ни один из них не полон сам по себе, и это измерено: по имени в дереве 36
// файлов, по раскладке 63, вместе 65 — то есть каждый признак пропускает то,
// что видит другой. Гейт, взявший один, тихо не читал бы целый вид суит.
func TestShellProbeCorpusTakesBothConventions(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
		why  string
	}{
		{"deploy/tests/helm/sec-hardening-test.sh", true, "оба признака"},
		{"tools/carrydrift/drift-test.sh", true, "только имя: каталога `tests/` над ним нет"},
		{"gateway/tests/newman/scripts/run.sh", true, "только раскладка: `test` в имени нет"},
		{"deploy/scripts/dev-up.sh", false,
			"инструмент дерева: пишет в него ПО СВОЕМУ НАЗНАЧЕНИЮ, и запрет был бы " +
				"запретом на него — та же граница, по которой Go-половина исключает " +
				"не-тестовые исходники"},
		{"deploy/tests/helm/README.md", false, "не скрипт"},
	}
	for _, c := range cases {
		if got := isShellProbePath(c.rel); got != c.want {
			t.Errorf("isShellProbePath(%q) = %v, ожидалось %v — %s", c.rel, got, c.want, c.why)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Контроль на ПРЕЖНИХ редакциях трёх суит.
// ─────────────────────────────────────────────────────────────────────────────

// priorEditions — прежние редакции трёх суит, найденных при заведении Go-половины
// (#696). Взяты ДОСЛОВНО из истории и лежат фикстурами, а не читаются из git:
// конвейер клонирует дерево на одну ревизию, и контроль, зависящий от истории,
// в нём просто не выполнялся бы — молча.
//
// Провенанс проверяем одной командой:
//
//	git show e22436f1^:deploy/tests/helm/<имя>.sh
//
// Расширение у фикстур НЕ `.sh` намеренно: иначе прежние — заведомо дефектные —
// редакции попали бы в корпус и этого, и всех прочих гейтов дерева, и красное на
// них означало бы не находку, а фикстуру.
var priorEditions = []string{
	"podtemplate-annotation-single-owner-test.sh",
	"image-rollout-binding-test.sh",
	"networkpolicy-egress-test.sh",
}

func loadPriorEditions(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range priorEditions {
		body, err := os.ReadFile("testdata/shellprobe/" + name + ".before")
		if err != nil {
			t.Fatalf("фикстура прежней редакции не прочитана: %v — контроль ниже был бы "+
				"утверждением ни о чём", err)
		}
		out["deploy/tests/helm/"+name] = string(body)
	}
	return out
}

// TestShellProbeWriteGateFindsAllThreeHistoricalSuites — контроль «3 из 3».
//
// Ради него задача и заведена: текстовый предикат нашёл бы ОДНУ суиту из трёх и
// объявил бы остальные две чистыми. Число проверяется здесь на настоящих
// исходниках, а не на пересказе.
func TestShellProbeWriteGateFindsAllThreeHistoricalSuites(t *testing.T) {
	findings, census := auditShellProbeWritesToLiveTree(loadPriorEditions(t), nil)
	if census.Files != len(priorEditions) {
		t.Fatalf("разобрано %d фикстур из %d — молчание по непрочитанному ничего не "+
			"значит (перепись: %+v)", census.Files, len(priorEditions), census)
	}
	hit := map[string]bool{}
	for _, f := range findings {
		hit[f.File] = true
	}
	if len(hit) != len(priorEditions) {
		t.Fatalf("суит с находками %d из %d — предмет закрыт не весь; найдено: %+v",
			len(hit), len(priorEditions), findings)
	}
	t.Logf("контроль: суит %d из %d, находок %d, перепись %+v",
		len(hit), len(priorEditions), len(findings), census)
}

// TestShellProbeWriteGateSeesWhatATextualPredicateCannot — довод, по которому
// текстовый предикат отвергнут, ИЗМЕРЕН, а не заявлен.
//
// От поиска по тексту предикат отличают ровно две способности: прочитать тело
// встроенного интерпретатора и провести происхождение через позиционные
// параметры функций скрипта. Отключаем обе — и на тех же трёх настоящих суитах
// остаётся ОДНА. Проба утверждает оба числа: без верхнего «1 из 3» ничего не
// значит (может, предикат вообще ничего не находит), без нижнего — «3 из 3» не
// объясняет, за счёт чего.
//
// Каждая способность проверяется и ПООТДЕЛЬНОСТИ: обе дают «2 из 3», то есть ни
// одна не лишняя. Без этих двух случаев можно было бы снять любую одну и не
// заметить.
func TestShellProbeWriteGateSeesWhatATextualPredicateCannot(t *testing.T) {
	src := loadPriorEditions(t)

	suites := func(caps shellAuditCapabilities) int {
		t.Helper()
		findings, _ := auditShellProbeWritesToLiveTreeWith(src, nil, caps)
		hit := map[string]bool{}
		for _, f := range findings {
			hit[f.File] = true
		}
		return len(hit)
	}

	if n := suites(fullShellAudit); n != 3 {
		t.Fatalf("предикат целиком нашёл %d суит из 3 — сравнивать не с чем", n)
	}
	if n := suites(shellAuditCapabilities{}); n != 1 {
		t.Errorf("без чтения тел интерпретаторов И без прослеживания параметров найдено "+
			"%d суит из 3, ожидалась 1. Это число — довод, по которому текстовый "+
			"предикат отвергнут: гейт, находящий треть предмета и печатающий «ноль "+
			"находок», и есть класс «форма без содержания»", n)
	}
	if n := suites(shellAuditCapabilities{paramFlow: true}); n != 2 {
		t.Errorf("без чтения тел интерпретаторов найдено %d из 3, ожидалось 2 — "+
			"способность перестала быть несущей, и её можно снять незамеченной", n)
	}
	if n := suites(shellAuditCapabilities{interpreterBodies: true}); n != 2 {
		t.Errorf("без прослеживания параметров найдено %d из 3, ожидалось 2 — "+
			"способность перестала быть несущей, и её можно снять незамеченной", n)
	}
}
