// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// rosterreaderexitcode_injection_test.go — способность гейта упасть, доказанная
// НАСТОЯЩИМ входом из истории дерева, и его способность промолчать на законном
// близнеце той же формы.
//
// # Почему вход берётся из git, а не выписывается здесь
//
// Синтетическая строка доказывала бы, что гейт краснеет на выдумке. Здесь входом
// служит текст файлов ДО починки, взятый у git по отпечатку базы полосы: ровно
// те три формы, что жили в дереве. Выписанный от руки близнец разошёлся бы с
// продуктом молча — тот же класс, который гейт и ловит.
//
// Если базового отпечатка в дереве нет (поверхностный клон, вырезанная история),
// исход — «НЕ ВЫПОЛНИЛОСЬ», а не «прошло»: у проб способности упасть третья
// категория не зачитывается в проход.
package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// rosterReaderBaseRev — отпечаток дерева ДО починки класса. Прибит намеренно:
// «предыдущая ревизия» перестаёт быть адресом на следующем же коммите.
const rosterReaderBaseRev = "d3fd77a6aa"

// gitShowAt — содержимое файла на названной ревизии.
//
// Через помощника дерева, а не голым `exec.Command`: `cmd.Dir` не выбирает
// репозиторий, когда в окружении есть `GIT_DIR`, — переменная сильнее рабочего
// каталога, и проба читала бы чужую копию. Держит это
// `TestGitCommandsRunWithScrubbedEnvironment`, и он нашёл здесь ровно этот вызов.
func gitShowAt(t *testing.T, root, rev, rel string) (string, bool) {
	t.Helper()
	out, err := gitenv.Command(root, "show", rev+":"+rel).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// TestRosterReaderGateRedsOnTheRealPreFixText — гейт находит каждую из трёх форм
// в том виде, в каком она лежала в дереве.
func TestRosterReaderGateRedsOnTheRealPreFixText(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	// Каждый случай — ОДИН файл и ОДНА ожидаемая форма. Собирать их в один вход
	// значило бы принять «нашлось три находки» за «нашлась каждая»: гейт,
	// узнающий одну форму трижды, прошёл бы такую проверку.
	cases := []struct {
		rel  string
		form string
		line string // фрагмент, обязанный попасть в находку
	}{
		{"deploy/tests/helm/stacks.sh", "список for", "for f in $(stacks_chain"},
		{"deploy/tests/helm/servername-checked-against-the-peer-test.sh", "список for", "for stack in $(stacks_names)"},
		{"services/vpc/tests/newman/scripts/run.sh", "подстановка процесса", "done < <(newman_all_stems"},
		{"deploy/tests/helm/networkpolicy-egress-test.sh", "аргумент helm/kubectl", "$(stacks_args"},
		{"deploy/tests/helm/rendered-documents-well-formed-test.sh", "here-string с подстановкой", `done <<<"$(stacks_table)"`},
	}
	missing := 0
	for _, c := range cases {
		body, ok := gitShowAt(t, root, rosterReaderBaseRev, c.rel)
		if !ok {
			t.Logf("НЕ ВЫПОЛНИЛОСЬ: %s на %s не читается — этот случай не проверен",
				c.rel, rosterReaderBaseRev)
			missing++
			continue
		}
		findings, cen := AuditRosterReaderExitCodes(map[string]string{c.rel: body})
		if cen.Consumptions == 0 {
			t.Errorf("%s: строк-потреблений ноль — вход не тот, что ожидался", c.rel)
			continue
		}
		hit := false
		for _, f := range findings {
			if f.Form == c.form && strings.Contains(f.Text, c.line) {
				hit = true
			}
		}
		if !hit {
			t.Errorf("%s: форма %q на настоящем тексте ДО починки не найдена — "+
				"гейт не способен упасть на том, ради чего написан (находок %d)",
				c.rel, c.form, len(findings))
			continue
		}
		t.Logf("  %s → форма %q найдена (потреблений в файле %d, находок %d)",
			c.rel, c.form, cen.Consumptions, len(findings))
	}
	if missing == len(cases) {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ни один файл базы %s не прочитан — "+
			"о способности гейта упасть не известно ничего", rosterReaderBaseRev)
	}
	t.Logf("перепись: случаев %d, не выполнилось %d", len(cases), missing)
}

// TestRosterReaderGateIsSilentOnTheLegalTwin — законная форма той же записи
// НЕ находка. Без этой половины гейт, краснеющий на всякой строке с именем
// читателя, прошёл бы проверку выше.
func TestRosterReaderGateIsSilentOnTheLegalTwin(t *testing.T) {
	t.Parallel()
	twins := map[string]string{
		// Присваивание с потребованным кодом — форма, которой сегодня написано
		// дерево.
		"legal/assign.sh": "args=\"$(stacks_args \"$stack\" \"$UMBRELLA\")\" || fatal \"отказ\"\n" +
			"# shellcheck disable=SC2086\n" +
			"helm template kacho-umbrella \"$UMBRELLA\" $args\n",
		// Обход ПЕРЕМЕННОЙ, а не подстановки.
		"legal/forvar.sh": "dirs=\"$(product_service_dirs)\" || exit 2\nfor d in $dirs api-gateway; do :; done\n",
		// here-string по переменной.
		"legal/herestring.sh": "rows=\"$(stacks_table)\" || fatal \"отказ\"\nwhile read -r r; do :; done <<<\"$rows\"\n",
		// Комментарий, в котором форма ОБЪЯСНЯЕТСЯ. Это половина, на которой
		// проверка по подстроке краснеет на собственном обосновании.
		"legal/prose.sh": "# for f in $(stacks_chain \"$want\" ' '); do — так было, и код терялся\n" +
			"# done < <(newman_all_stems \"$NEWMAN_DIR\") — тоже теряет\n",
		// Путь с сегментом `helm`: имя каталога не есть имя команды.
		"legal/path.mk": "\targs=\"$$(bash tests/helm/stacks.sh --args dev ./helm/umbrella)\"; \\\n",
	}
	findings, cen := AuditRosterReaderExitCodes(twins)
	if cen.FilesMentioned != len(twins)-0 {
		t.Logf("упомянувших файлов %d из %d", cen.FilesMentioned, len(twins))
	}
	if cen.Consumptions == 0 {
		t.Fatal("законные близнецы не дали ни одной строки-потребления — " +
			"молчание объяснялось бы тем, что гейт их не читал")
	}
	for _, f := range findings {
		t.Errorf("ЛОЖНАЯ НАХОДКА на законной форме: %s", f)
	}
	t.Logf("перепись: близнецов %d, строк-потреблений %d, ложных находок %d",
		len(twins), cen.Consumptions, cen.Findings)
}
