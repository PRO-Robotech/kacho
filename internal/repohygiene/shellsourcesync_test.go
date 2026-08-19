// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// shellsourcesync_test.go — разбор обязан ЧИТАТЬ, а не молчать.
//
// # Почему это отдельная проба, а не следствие зелёного гейта
//
// Рассинхрон лексера не выглядит поломкой: незакрытая кавычка уводит чтение до
// конца файла, дальше код читается как строка, команды не находятся — и гейт
// печатает «находок 0». Отличить это от чистого скрипта по вердикту нельзя:
// оба зелёные. Наблюдалось четырежды за один заход, и каждый раз «ноль находок»
// по целому скрипту было получено даром — один из них, 467 строк, разобрался в
// ДВЕ команды.
//
// Поэтому здесь проверяется способность разбора в обе стороны: на конструкциях,
// ВЗЯТЫХ ИЗ ДЕРЕВА, он обязан дочитать до конца и выдать команды; на заведомо
// незакрытой — обязан поднять признак. Одна половина без другой ничего не
// значит: разбор, всегда объявляющий рассинхрон, прошёл бы отрицательную, а
// всегда молчащий — положительную.
package repohygiene

import (
	"strings"
	"testing"
)

// shellSyncCase — конструкция, на которой лексер уже терял синхронизацию.
type shellSyncCase struct {
	name string
	// seen — где эта форма живёт в дереве. Не координата для перехода, а
	// доказательство, что фикстура не выдумана: форма, которой в дереве нет,
	// проверяет воображение автора.
	seen string
	src  string
}

func shellSyncCases() []shellSyncCase {
	return []shellSyncCase{
		{
			name: "кавычка внутри одинарных внутри подстановки внутри строки",
			seen: "deploy/tests/helm/geo-authz-edge-armed-test.sh",
			src: `val="$(printf '%s\n' "$render" | tr -d '"[:space:]')"
echo "$val"
`,
		},
		{
			name: "ANSI-C цитата",
			seen: ".github/scripts/check-pinned-tools.sh",
			src: `IFS=$'\n' read -r -d '' -a lines <<<"$blob"
echo done
`,
		},
		{
			name: "документ-вставка внутри подстановки",
			seen: "deploy/tests/helm/config-rollout-binding-test.sh",
			src: `PY=$(cat <<'PYSRC'
def f(x):
    return x["k"]  # скобки и кавычки чужого языка
PYSRC
)
echo "$PY"
`,
		},
		{
			name: "строка-вставка внутри подстановки",
			seen: ".github/scripts/check-pinned-tools.sh",
			src: `scanned="$(
  read -r -a g <<<"${p##*"$S"}"
  echo "${g[@]}"
)"
echo "$scanned"
`,
		},
		{
			name: "вложенная подстановка в умолчании",
			seen: "deploy/scripts/helm-umbrella-deps.sh",
			src: `out="${OUT:-${DEFAULT_OUT}}"
echo "$out"
`,
		},
		{
			name: "обратные кавычки с кавычкой внутри одинарных",
			seen: "форма языка; в дереве встречается обратными кавычками вообще",
			src:  "v=`printf '%s' \"$x\" | tr -d '\"'`\necho \"$v\"\n",
		},
	}
}

// TestShellParseReadsToTheEndOnFormsTakenFromTheTree — положительная половина.
func TestShellParseReadsToTheEndOnFormsTakenFromTheTree(t *testing.T) {
	cases := shellSyncCases()
	if len(cases) == 0 {
		t.Fatal("набор форм пуст — проверять нечего, и «прошло» означало бы «не читали»")
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmds, _, un := shellParseChecked(c.src)
			if un {
				t.Errorf("разбор потерял синхронизацию (форма живёт в %s). Дальше он "+
					"читает код как строку, и «находок 0» по такому скрипту получено даром",
					c.seen)
			}
			// «Дочитал» — не то же, что «прочитал». Разбор, молча съевший весь
			// исходник одним словом, тоже не поднимет признака.
			if len(cmds) < 2 {
				t.Errorf("команд распознано %d при %d строках — признак не поднят, но и "+
					"прочитано ничего", len(cmds), strings.Count(c.src, "\n"))
			}
		})
	}
}

// TestShellParseRaisesDesyncOnGenuinelyUnterminatedInput — отрицательная
// половина. Без неё положительная прошла бы у разбора, который признак не
// поднимает НИКОГДА.
func TestShellParseRaisesDesyncOnGenuinelyUnterminatedInput(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"незакрытая одинарная", "echo 'hello\necho world\n"},
		{"незакрытая двойная", "echo \"hello\necho world\n"},
		{"незакрытая подстановка", "x=$(echo hi\necho world\n"},
		{"незакрытая фигурная", "x=${p:-\necho world\n"},
		{"незакрытые обратные", "x=`echo hi\necho world\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, _, un := shellParseChecked(c.src); !un {
				t.Errorf("на заведомо незакрытом входе признак рассинхрона НЕ поднят — " +
					"значит и на живом скрипте он не поднимется, а «ноль находок» по " +
					"непрочитанному будет неотличимо от чистоты")
			}
		})
	}
}
