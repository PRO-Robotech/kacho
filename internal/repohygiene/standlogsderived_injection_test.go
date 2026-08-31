// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт выведенного перечня СПОСОБЕН упасть, СПОСОБЕН
// смолчать — и что падает он на СВОЁМ предмете, а не на соседском.
//
// # ТРИ ПРОГОНА, А НЕ ДВА
//
// Инъекция обязана ронять ТОЛЬКО проверяемое (`testing.md` §«Гейт на класс»,
// п. 2в). Гейт читает шаги тех же конвейеров, что и гейт эха мёртвого стенда
// (#728), поэтому доказательство идёт тремя прогонами:
//
//	контроль               — конвейер цел: молчат ОБА;
//	инъекция нового        — выписанный перечень: краснеет ТОЛЬКО новый;
//	инъекция существующего — диагностика без стража: краснеет ТОЛЬКО гейт #728.
//
// Без третьего прогона молчание соседа в прогоне 2 неотличимо от молчания
// мёртвого.
//
// # ЗАКОННЫЕ БЛИЗНЕЦЫ — РАЗНЫЕ КОНСТРУКЦИИ, А НЕ КОПИИ ОДНОЙ
//
// Обход по выведенному перечню, обход по собранным файлам и упоминание формы в
// КОММЕНТАРИИ — три разные законные записи. Копия прежнего близнеца близнецом не
// является: она подтверждает уже подтверждённое и оставляет непроверенной ту
// форму, ради которой её пишут.
//
// # ПОВЕДЕНИЕ САМОГО СБОРЩИКА ДОБЫВАЕТСЯ ИСПОЛНЕНИЕМ
//
// Гейт выше требует, чтобы перечень выводился. Что сборщик при этом ДЕЛАЕТ —
// вопрос отдельный, и ответ на него даёт прогон с подставным инструментом:
// служба, которой выписанный перечень не знал, обязана попасть в сбор, а
// недоступный журнал — быть названным числом и аннотацией, а не тишиной.
package repohygiene

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ─────────────────────────────── ФИКСТУРЫ ───────────────────────────────────

// slLawful — сбор журналов по перечню, ВЫВЕДЕННОМУ из кластера, плюс хвост по
// собранным файлам. Ровно то, что стоит в дереве.
const slLawful = `
name: проба
jobs:
  e2e:
    steps:
      - name: стенд
        id: stand
        run: make dev-up
      - name: журналы сервисов
        if: ${{ always() && steps.stand.outcome == 'success' }}
        run: |
          "$GITHUB_WORKSPACE/.github/scripts/stand-present-or-explain.sh" "стенд" || exit 0
          for obj in $(kubectl -n kacho get deploy,statefulset -o name || true); do
            kubectl -n kacho logs "$obj" --tail=20000 > "stand-logs/${obj##*/}.log" || true
          done
      - name: хвост журналов
        if: failure()
        run: |
          logs=(stand-logs/*.log)
          for f in "${logs[@]}"; do
            tail -60 "$f"
          done
`

// slEnumerated — ДЕФЕКТ #1741: перечень выписан именами в теле шага.
const slEnumerated = `
name: проба
jobs:
  e2e:
    steps:
      - name: стенд
        id: stand
        run: make dev-up
      - name: журналы сервисов
        if: ${{ always() && steps.stand.outcome == 'success' }}
        run: |
          "$GITHUB_WORKSPACE/.github/scripts/stand-present-or-explain.sh" "стенд" || exit 0
          for d in kacho-iam vpc compute kacho-nlb kacho-geo api-gateway; do
            kubectl -n kacho logs "deploy/$d" --tail=20000 > "stand-logs/$d.log" || true
          done
`

// slEchoDefect — ДЕФЕКТ #728, чужой: диагностика под always() без стража.
const slEchoDefect = `
name: проба
jobs:
  e2e:
    steps:
      - name: стенд
        run: make dev-up
      - name: что поднялось
        if: always()
        run: kubectl -n kacho get pods -o wide
`

func slWrite(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "synthetic.yml"), []byte(body), 0o600); err != nil {
		t.Fatalf("фикстура не записана: %v", err)
	}
	return dir
}

func slRun(t *testing.T, body string) ([]string, slCensus) {
	t.Helper()
	steps, files, err := slReadWorkflowSteps(slWrite(t, body))
	if err != nil {
		t.Fatalf("разбор фикстуры: %v", err)
	}
	return slScan(steps, files)
}

// ───────────────────── ТРИ ПРОГОНА ОДНОЙ ФИКСТУРЫ ───────────────────────────

func TestSL_Run1_ControlBothGatesSilent(t *testing.T) {
	finds, cen := slRun(t, slLawful)
	if cen.logSteps == 0 {
		t.Fatalf("гейт не увидел ни одного шага, читающего журналы: перепись %+v — "+
			"его молчание ниже было бы молчанием слепого", cen)
	}
	if len(finds) != 0 {
		t.Fatalf("гейт покраснел на целом конвейере: %v", finds)
	}
	if echo, _ := checkClusterDiagnosticsEcho("synthetic.yml", slLawful); len(echo) != 0 {
		t.Fatalf("сосед #728 покраснел на целом конвейере: %v", echo)
	}
}

func TestSL_Run2_EnumeratedListRedsOnlyTheNewGate(t *testing.T) {
	finds, cen := slRun(t, slEnumerated)
	if cen.logSteps == 0 {
		t.Fatalf("гейт не увидел шага, читающего журналы: перепись %+v", cen)
	}
	if len(finds) == 0 {
		t.Fatal("выписанный перечень не дал находки — гейт не способен упасть на своём предмете")
	}
	joined := strings.Join(finds, "\n")
	// Находка обязана называть КООРДИНАТУ и сам перечень: без них читатель не
	// знает, что чинить, и тратит на догадку целый прогон.
	for _, want := range []string{"synthetic.yml", "журналы сервисов", "kacho-iam"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("находка не называет %q — чинить по ней нельзя:\n%s", want, joined)
		}
	}
	// Сосед обязан промолчать: страж на месте, его предмета инъекция не касается.
	if echo, _ := checkClusterDiagnosticsEcho("synthetic.yml", slEnumerated); len(echo) != 0 {
		t.Fatalf("инъекция уронила ЧУЖОЙ гейт — красное пришло бы от соседа, "+
			"и вакуумность нового осталась бы недоказанной: %v", echo)
	}
}

func TestSL_Run3_MissingGuardRedsOnlyTheNeighbour(t *testing.T) {
	echo, _ := checkClusterDiagnosticsEcho("synthetic.yml", slEchoDefect)
	if len(echo) == 0 {
		t.Fatal("сосед #728 промолчал на своём предмете — его молчание в прогоне 2 " +
			"неотличимо от молчания мёртвого, и доказательство рассыпается")
	}
	if finds, _ := slRun(t, slEchoDefect); len(finds) != 0 {
		t.Fatalf("новый гейт покраснел на ЧУЖОМ предмете: %v", finds)
	}
}

// ───────────────────────── ЗАКОННЫЕ БЛИЗНЕЦЫ ────────────────────────────────

// Близнец 1: обход по перечню, выведенному подстановкой, — предмет запрета
// отсутствует by construction.
func TestSL_DerivedLoopIsLawfulAndSeen(t *testing.T) {
	finds, cen := slRun(t, slLawful)
	if cen.loops < 2 {
		t.Fatalf("распознаватель обходов увидел %d — законные близнецы не осмотрены, "+
			"и их молчание ничего не доказывает", cen.loops)
	}
	if len(finds) != 0 {
		t.Fatalf("выведенный перечень объявлен находкой: %v", finds)
	}
}

// Близнец 2: обход по литеральному перечню, НЕ читающий журналов, — чужой
// предмет. Гейт запрещает не всякий перечень, а перечень В СБОРЕ ЖУРНАЛОВ.
func TestSL_EnumeratedLoopThatReadsNoLogsIsLawful(t *testing.T) {
	const body = `
name: проба
jobs:
  e2e:
    steps:
      - name: миграции
        run: |
          for svc in iam vpc compute nlb; do
            make -C deploy migrate SVC="$svc"
          done
`
	finds, cen := slRun(t, body)
	if cen.loops == 0 {
		t.Fatal("обход не осмотрен — молчание гейта здесь ничего не доказывает")
	}
	if len(finds) != 0 {
		t.Fatalf("перечень, не читающий журналов, объявлен находкой — гейт ловит форму, "+
			"а не существо: %v", finds)
	}
}

// Близнец 3: форма, названная в КОММЕНТАРИИ. Гейт по подстроке краснел бы на
// собственном объяснении — и на этом самом файле.
func TestSL_TheFormNamedInACommentIsNotCode(t *testing.T) {
	const body = `
name: проба
jobs:
  e2e:
    steps:
      - name: журналы сервисов
        run: |
          # Здесь стоял выписанный перечень:
          #   for d in kacho-iam vpc compute kacho-nlb; do kubectl logs "deploy/$d"; done
          # Он снят: перечень выводится из стенда.
          "$GITHUB_WORKSPACE/.github/scripts/collect-stand-logs.sh" kacho stand-logs
`
	finds, cen := slRun(t, body)
	if cen.logSteps == 0 {
		t.Fatal("шаг, читающий журналы, не распознан — молчание ничего не доказывает")
	}
	if len(finds) != 0 {
		t.Fatalf("гейт покраснел на СОБСТВЕННОМ объяснении в комментарии: %v", finds)
	}
}

// ────────────────── ПОВЕДЕНИЕ СБОРЩИКА — ИСПОЛНЕНИЕМ ────────────────────────

// slFakeKubectl — подставной инструмент: перечисляет носители и отдаёт (или не
// отдаёт) их журналы. Подделка структурно НЕ МОЖЕТ отдать «всё хорошо» молча:
// на неизвестной ей команде она отказывает.
const slFakeKubectl = `#!/bin/sh
# аргументы: -n <ns> <глагол> ...
for a in "$@"; do
  case "$a" in
    get) verb=get ;;
    logs) verb=logs ;;
  esac
done
if [ "${verb:-}" = "get" ]; then
  case "$*" in
    *events*) echo "LAST SEEN   TYPE   REASON"; exit 0 ;;
  esac
  printf '%s\n' $KACHO_FAKE_WORKLOADS
  exit 0
fi
if [ "${verb:-}" = "logs" ]; then
  for a in "$@"; do
    case "$a" in
      */*) obj="$a" ;;
    esac
  done
  name="${obj##*/}"
  case " $KACHO_FAKE_UNREADABLE " in
    *" $name "*) echo "Error from server: container not found" >&2; exit 1 ;;
  esac
  echo "журнал $name: строка"
  exit 0
fi
echo "подставной kubectl: неизвестная команда $*" >&2
exit 3
`

func slRunCollector(t *testing.T, workloads, unreadable string) (string, int, string) {
	t.Helper()
	root := repoRoot(t)
	script := filepath.Join(root, ".github", "scripts", standLogsCollector)
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("сборщика нет по координате %s: %v", script, err)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash недоступен (%v) — предпосылка исполняемой пробы не выполняется", err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(slFakeKubectl), 0o500); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "stand-logs")
	cmd := exec.Command(bash, script, "kacho", out, "100")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"KACHO_FAKE_WORKLOADS="+workloads,
		"KACHO_FAKE_UNREADABLE="+unreadable)
	raw, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("сборщик не запустился: %v\n%s", err, raw)
		}
		code = ee.ExitCode()
	}
	return string(raw), code, out
}

// ГЛАВНОЕ УТВЕРЖДЕНИЕ #1741: служба, которой выписанный перечень НЕ ЗНАЛ,
// попадает в сбор — потому что перечень спрашивается у стенда.
func TestSL_CollectorTakesServicesTheOldListNeverNamed(t *testing.T) {
	const workloads = "deployment.apps/kacho-iam deployment.apps/kratos " +
		"deployment.apps/kratos-courier deployment.apps/hydra statefulset.apps/zot"
	out, code, dir := slRunCollector(t, workloads, "")
	if code != 0 {
		t.Fatalf("сборщик вынес вердикт (код %d), а он диагностика: второе красное рядом "+
			"с настоящей причиной делает один отказ похожим на два\n%s", code, out)
	}
	if !strings.Contains(out, "поднято 5") || !strings.Contains(out, "собрано 5") {
		t.Errorf("перепись не назвала оба числа рядом — «журналы собраны» неотличимо от "+
			"«собраны не все»:\n%s", out)
	}
	for _, name := range []string{"kratos", "kratos-courier", "hydra", "zot"} {
		hits, err := filepath.Glob(filepath.Join(dir, "*"+name+"*.log"))
		if err != nil || len(hits) == 0 {
			t.Errorf("журнал %q не собран — служба поднята и в сбор не попала, "+
				"то есть предмет #1741 жив:\n%s", name, out)
		}
	}
	if strings.Contains(out, "::warning") {
		t.Errorf("сборщик предупредил при полном сборе — тревога без предмета перестаёт "+
			"читаться:\n%s", out)
	}
}

func TestSL_UnreadableLogIsNamedByNumberAndAnnotation(t *testing.T) {
	out, code, dir := slRunCollector(t,
		"deployment.apps/kacho-iam deployment.apps/kratos", "kratos")
	if code != 0 {
		t.Fatalf("сборщик вынес вердикт (код %d) на недоступном журнале\n%s", code, out)
	}
	if !strings.Contains(out, "недоступно 1") {
		t.Errorf("недоступный журнал не назван числом:\n%s", out)
	}
	if !strings.Contains(out, "::warning") || !strings.Contains(out, "kratos") {
		t.Errorf("недоступный журнал не назван аннотацией с именем службы — «собрали не всё» "+
			"неотличимо от «всё хорошо»:\n%s", out)
	}
	body, err := os.ReadFile(filepath.Join(dir, "deployment-kratos.log")) // #nosec G304 -- каталог пробы
	if err != nil {
		t.Fatalf("файл недоступного журнала не создан: %v", err)
	}
	// Причина уезжает В АРТЕФАКТ, а не только в журнал работы.
	if !strings.Contains(string(body), "журнал недоступен") {
		t.Errorf("файл не называет причины — в артефакте пустая выкладка неотличима от потери: %q", body)
	}
}

func TestSL_EmptyStandIsSaidOutLoudNotSwallowed(t *testing.T) {
	out, code, _ := slRunCollector(t, "", "")
	if code != 0 {
		t.Fatalf("сборщик вынес вердикт (код %d) на пустом стенде\n%s", code, out)
	}
	if !strings.Contains(out, "поднято 0") && !strings.Contains(out, "ни одного") {
		t.Errorf("пустой стенд проглочен — «ноль собранного» неотличимо от «ноль поднятого»:\n%s", out)
	}
	if !strings.Contains(out, "::warning") {
		t.Errorf("пустой стенд не назван аннотацией:\n%s", out)
	}
}
