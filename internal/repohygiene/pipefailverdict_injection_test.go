// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «вердикт не берётся из трубы под pipefail»
// СПОСОБЕН упасть — и что падает он на существе, а не на внешности формы.
//
// Инъекция идёт в обе стороны, потому что одного «краснеет» мало (гейт,
// краснеющий на всём, ничего не измеряет), и одного «молчит» тоже мало
// (молчание бывает от того, что читать не стали):
//
//	труба в grep -q под pipefail       → краснеет, называя координату;
//	то же через продолжение строки     → краснеет (склейка логических строк);
//	то же внутри "$( … )"              → краснеет (подстановка — это код);
//	grep -m1 (выход по N-му)           → краснеет: ранний выход тот же;
//	длинный флаг --quiet               → краснеет;
//
//	grep без ранне-выходящих флагов    → молчит: он дочитывает вход до EOF;
//	статус отброшен через || true      → молчит: вердикта никто не берёт;
//	файл без pipefail                  → молчит: у запрета нет предпосылки;
//	форма в КОММЕНТАРИИ                → молчит (в дереве таких два места);
//	форма в теле heredoc               → молчит: это данные, а не код;
//	форма в строковом литерале         → молчит;
//	"-q" как ОБРАЗЕЦ, а не флаг        → молчит;
//	починенная форма [[ == *…* ]]      → молчит.
//
// Обе половины гоняют ТУ ЖЕ функцию (`auditPipefailVerdicts`), что и обход
// дерева: проба, повторяющая логику гейта своей копией, доказывала бы свойство
// копии.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические скрипты. Каркас взят у настоящего
// `services/nlb/deploy/tests/render-guard.sh` — это форма из дерева, а не выдумка.
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ 1 — канон задачи #658: вердикт утверждения берётся из трубы.
const synthPipefailPlain = `#!/usr/bin/env bash
set -euo pipefail

assert_contains() {
  local out="$1" needle="$2" desc="$3"
  if printf '%s' "$out" | grep -qF -- "$needle"; then
    pass "$desc"
  else
    fail "$desc"
  fi
}
`

// ДЕФЕКТ 2 — та же форма, разнесённая продолжением строки. Ловится, только если
// логические строки склеиваются: физическая строка с grep начинается с трубы.
const synthPipefailContinued = `#!/usr/bin/env bash
set -euo pipefail

check_from() {
  helm template rel "$CHART" --show-only templates/netpol.yaml \
    | yq '.spec.ingress[0].from[0].podSelector' \
    | grep -qx "$want_from" \
    || fail "ingress from не тот"
}
`

// ДЕФЕКТ 3 — труба внутри подстановки внутри двойных кавычек. Наивная маска
// гасит всё, что в кавычках, и эту форму не видит вовсе.
const synthPipefailInSubstitution = `#!/usr/bin/env bash
set -euo pipefail

sum="$(helm template rel "$CHART" --show-only templates/deployment.yaml \
  | grep -m1 'kacho.cloud/config-checksum:')" || fail "deployment does not render"
`

// ДЕФЕКТ 4 — длинный флаг вместо короткого. Тот же ранний выход.
const synthPipefailLongFlag = `#!/usr/bin/env bash
set -o pipefail

if printf '%s' "$body" | grep --quiet '^kacho_'; then
  echo ok
fi
`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — grep БЕЗ ранне-выходящих флагов: он дочитывает вход до
// конца, писатель SIGPIPE не получает. Форма внешне та же самая.
const synthLegitGrepReadsToEOF = `#!/usr/bin/env bash
set -euo pipefail

echo "$RENDER" | grep -nE "(^|[^-a-zA-Z0-9])${BAD_HOST}" >&2
hits="$(printf '%s\n' "$out" | grep -c 'kind:')"
`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — статус конвейера ЯВНО отброшен: вердикта из него никто
// не берёт, значит ложного отказа быть не может.
const synthLegitStatusDiscarded = `#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$STS_LIST" | grep -q "pg-geo" || true
printf '%s\n' "$STS_LIST" | grep -qF "pg-vpc" || :
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — предпосылки нет: без pipefail статус конвейера берётся
// от последнего звена, а grep -q на совпадении даёт ноль.
const synthLegitNoPipefail = `#!/usr/bin/env bash
set -eu

if printf '%s' "$out" | grep -qF -- "$needle"; then
  echo found
fi
`

// ЗАКОННЫЙ БЛИЗНЕЦ 4 — форма стоит в КОММЕНТАРИИ, объясняющем сам запрет.
// В дереве таких мест два, и оба обязаны молчать: иначе гейт краснеет на
// собственном объяснении.
const synthLegitFormInComment = `#!/usr/bin/env bash
set -euo pipefail

# ВЫВОД БЕРЁТСЯ ПОДСТАНОВКОЙ, А НЕ КОНВЕЙЕРОМ В grep -q. С pipefail конвейер
# … | grep -q возвращает ОТКАЗ НА СОВПАДЕНИИ: grep выходит по первому
# попаданию, писатель слева получает SIGPIPE (141), и статус берётся от него.
out_noterm="$(census_render "$tmp/direct.yaml")"
if [[ "$out_noterm" == *"терминатора в стеке нет"* ]]; then
  ok "класс назван"
fi
`

// ЗАКОННЫЙ БЛИЗНЕЦ 5 — форма в теле heredoc: это данные, которые скрипт
// ПЕЧАТАЕТ, а не код, который он исполняет.
const synthLegitFormInHeredoc = `#!/usr/bin/env bash
set -euo pipefail

cat >"$tmp/injected.sh" <<'EOF'
if printf '%s' "$out" | grep -qF -- "$needle"; then
  echo injected
fi
EOF

cat <<-TXT
	пример негодной формы: printf '%s' "$x" | grep -q y
TXT
`

// ЗАКОННЫЙ БЛИЗНЕЦ 6 — форма внутри строкового литерала: это текст сообщения,
// а не исполняемый конвейер.
const synthLegitFormInStringLiteral = `#!/usr/bin/env bash
set -euo pipefail

fail 'негодная форма: printf | grep -q — чинится сравнением'
hint="используйте [[ ]] вместо cmd | grep -q"
`

// ЗАКОННЫЙ БЛИЗНЕЦ 7 — "-q" приезжает ОБРАЗЦОМ, а не флагом: после `--` и после
// `-e` следующий токен разбору как флаг не подлежит.
const synthLegitDashQAsPattern = `#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$flags" | grep -F -- "-q"
printf '%s\n' "$flags" | grep -e "-q" -e "-m1"
`

// ЗАКОННЫЙ БЛИЗНЕЦ 8 — починенная форма: внешнего процесса нет вовсе, значит
// нет ни трубы, ни писателя, ни SIGPIPE.
const synthLegitFixedForm = `#!/usr/bin/env bash
set -euo pipefail

assert_contains() {
  local out="$1" needle="$2" desc="$3"
  if [[ "$out" == *"$needle"* ]]; then
    pass "$desc"
  else
    fail "$desc"
  fi
}

if [[ "$RENDER" =~ endpoint:[[:space:]]*\"${GOOD_HOST}:9090\" ]]; then
  pass "endpoint"
fi
`

// ─────────────────────────────────────────────────────────────────────────────
// (а) ГЕЙТ КРАСНЕЕТ — и называет координату
// ─────────────────────────────────────────────────────────────────────────────

func TestPipefailVerdictGateRedensOnTheReturnedPipe(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line int    // физическая строка, которую гейт обязан назвать
		flag string // чем именно выход ранний
	}{
		{"труба в grep -qF под pipefail", synthPipefailPlain, 6, "-q"},
		{"та же труба через продолжение строки", synthPipefailContinued, 7, "-q"},
		{"труба внутри \"$( … )\"", synthPipefailInSubstitution, 5, "-m"},
		{"длинный флаг --quiet", synthPipefailLongFlag, 4, "-q"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			census, findings := auditPipefailVerdicts(map[string]string{"probe.sh": tc.src})

			if census.FilesPipefail != 1 {
				t.Fatalf("предпосылка не распознана: файлов под pipefail %d, ожидали 1 — "+
					"гейт молчал бы не потому, что чисто, а потому, что не читал",
					census.FilesPipefail)
			}
			if len(findings) != 1 {
				t.Fatalf("находок %d, ожидали 1: гейт НЕ СПОСОБЕН упасть на возвращённой трубе.\n"+
					"перепись: конвейеров %d, ранне-выходящих %d, отброшенных %d",
					len(findings), census.GrepPipes, census.EarlyExit, census.Discarded)
			}
			got := findings[0]
			if got.File != "probe.sh" || got.Line != tc.line {
				t.Errorf("координата названа неверно: %s:%d, ожидали probe.sh:%d — "+
					"находка без верного адреса заставляет искать дефект вручную",
					got.File, got.Line, tc.line)
			}
			if got.Flag != tc.flag {
				t.Errorf("вид раннего выхода назван неверно: %q, ожидали %q", got.Flag, tc.flag)
			}
			if !strings.Contains(got.String(), "probe.sh") {
				t.Errorf("сообщение находки не несёт координату: %q", got.String())
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (б) ГЕЙТ МОЛЧИТ — на законной форме той же внешности, и перепись это ЗАСЧИТЫВАЕТ
// ─────────────────────────────────────────────────────────────────────────────

func TestPipefailVerdictGateStaysSilentOnTheLawfulTwin(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// wantPipes — сколько конвейеров `| grep` гейт обязан РАССМОТРЕТЬ.
		// Ноль здесь означал бы, что молчание пришло от нечтения, а не от чистоты.
		wantPipes     int
		wantEarly     int
		wantDiscarded int
		wantPipefail  int
	}{
		{"grep дочитывает вход до EOF", synthLegitGrepReadsToEOF, 2, 0, 0, 1},
		{"статус отброшен через || true", synthLegitStatusDiscarded, 2, 2, 2, 1},
		{"без pipefail предпосылки нет", synthLegitNoPipefail, 0, 0, 0, 0},
		{"форма в комментарии", synthLegitFormInComment, 0, 0, 0, 1},
		{"форма в теле heredoc", synthLegitFormInHeredoc, 0, 0, 0, 1},
		{"форма в строковом литерале", synthLegitFormInStringLiteral, 0, 0, 0, 1},
		{"-q как образец, а не флаг", synthLegitDashQAsPattern, 2, 0, 0, 1},
		{"починенная форма без трубы", synthLegitFixedForm, 0, 0, 0, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			census, findings := auditPipefailVerdicts(map[string]string{"probe.sh": tc.src})

			if len(findings) != 0 {
				t.Fatalf("гейт покраснел на ЗАКОННОЙ форме — он ловит внешность, а не существо, "+
					"и первый же ложный срабат его отключит.\nнаходки: %v", findings)
			}
			if census.FilesRead != 1 {
				t.Fatalf("перепись прочитала %d файлов, ожидали 1", census.FilesRead)
			}
			if census.FilesPipefail != tc.wantPipefail {
				t.Errorf("файлов под pipefail %d, ожидали %d — предпосылка распознана неверно",
					census.FilesPipefail, tc.wantPipefail)
			}
			if census.GrepPipes != tc.wantPipes {
				t.Errorf("конвейеров `| grep` рассмотрено %d, ожидали %d — "+
					"молчание, пришедшее от нечтения, неотличимо от молчания по чистоте",
					census.GrepPipes, tc.wantPipes)
			}
			if census.EarlyExit != tc.wantEarly {
				t.Errorf("ранне-выходящих %d, ожидали %d", census.EarlyExit, tc.wantEarly)
			}
			if census.Discarded != tc.wantDiscarded {
				t.Errorf("с отброшенным статусом %d, ожидали %d", census.Discarded, tc.wantDiscarded)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (в) ПРЕДПОСЫЛКА гейта проверяема: пустой корпус и корпус без pipefail
//     обязаны быть ОТЛИЧИМЫ от «прочитали и не нашли»
// ─────────────────────────────────────────────────────────────────────────────

func TestPipefailVerdictCensusDistinguishesCleanFromUnread(t *testing.T) {
	// Пустой корпус: ноль прочитанного. Гейт дерева на этом ФАТАЛИТ; здесь
	// закрепляется то, на чём он это решение принимает.
	census, findings := auditPipefailVerdicts(map[string]string{})
	if census.FilesRead != 0 || len(findings) != 0 {
		t.Fatalf("на пустом корпусе перепись должна дать нули, получили %+v", census)
	}

	// Корпус есть, pipefail нет ни у кого: признак перестал что-либо измерять.
	// Это ОТДЕЛЬНОЕ состояние, и гейт дерева обязан отличать его от чистоты.
	census, findings = auditPipefailVerdicts(map[string]string{
		"a.sh": synthLegitNoPipefail,
		"b.sh": synthLegitNoPipefail,
	})
	if census.FilesRead != 2 {
		t.Fatalf("прочитано %d, ожидали 2", census.FilesRead)
	}
	if census.FilesPipefail != 0 {
		t.Fatalf("под pipefail %d, ожидали 0 — иначе предпосылка распознаётся неверно",
			census.FilesPipefail)
	}
	if len(findings) != 0 {
		t.Fatalf("находки на корпусе без предпосылки: %v", findings)
	}

	// И наоборот: идеал (всё починено) — это НЕ поломка. Ноль находок при
	// непустом корпусе и живой предпосылке обязан проходить.
	census, findings = auditPipefailVerdicts(map[string]string{
		"fixed.sh": synthLegitFixedForm,
		"eof.sh":   synthLegitGrepReadsToEOF,
	})
	if census.FilesPipefail != 2 {
		t.Fatalf("под pipefail %d, ожидали 2", census.FilesPipefail)
	}
	if len(findings) != 0 {
		t.Fatalf("пустой перечень находок есть ЦЕЛЬ, а не поломка, но гейт нашёл: %v", findings)
	}
}
