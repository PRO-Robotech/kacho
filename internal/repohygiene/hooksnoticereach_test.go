// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// hooksnoticereach_test.go — НЕПРОВЯЗАННЫЙ КЛОН ОБЯЗАН БЫТЬ ЗАМЕТЕН.
//
// # Предмет
//
// Провязка хука отправки — единственное, что стоит между правкой и стволом:
// конвейер проверяет ветку один раз, на запросе в ствол, а внутри накопительной
// линии вердикта не будет вовсе. При этом непровязанность НЕНАБЛЮДАЕМА по
// исходу отправки: она проходит молча в обоих случаях.
//
// Общий клон четырнадцати рабочих копий прожил непровязанным неизвестно сколько
// (#1051). Нашлось это не по красному прогону, а потому что кто-то специально
// спросил, было ли красное, — а его не было ПОТОМУ, ЧТО проверка не
// исполнялась. Само по себе `make check-hooks` этого не чинит: чтобы его
// позвать, надо уже подозревать.
//
// Поэтому механизм говорит САМ — тихим режимом `install.sh notice`, повешенным
// на то, что человек и так запускает перед отправкой. Этот гейт стережёт ровно
// его достижимость: снять `$(HOOKS_NOTICE)` с целей или убрать вызов из
// прогонщика можно одной строкой, и ни одна проба сегодня не покраснела бы —
// а следующий клон был бы так же нем.
//
// # Чего гейт НЕ делает
//
// Он не проверяет, провязан ли клон, в котором его запустили: провязка —
// свойство машины, а не дерева, и краснеть на чужой настройке гейт дерева не
// вправе. Способность самой провязки говорить и молчать доказывает
// `scripts/hooks/install-inject.sh`.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	hooksNoticeMakefile = "Makefile"
	hooksNoticeRunner   = "scripts/ci-local.sh"
)

var (
	// Цель, объявляющая тихий режим, и переменная, которой она навешивается.
	reHooksNoticeTarget = regexp.MustCompile(`(?m)^hooks-notice:`)
	reHooksNoticeVar    = regexp.MustCompile(`HOOKS_NOTICE\s*:=\s*hooks-notice`)
	// Цель, несущая `$(HOOKS_NOTICE)` среди предпосылок.
	reHooksNoticeUser = regexp.MustCompile(`(?m)^([a-zA-Z0-9_.-]+):[^\n=]*\$\(HOOKS_NOTICE\)`)
	// Вызов тихого режима из прогонщика, который зовут руками перед отправкой.
	reRunnerNotice = regexp.MustCompile(`hooks/install\.sh"?\s+notice`)
)

// hooksNoticeReach — перепись путей, по которым непровязанность становится
// слышна.
type hooksNoticeReach struct {
	TargetDeclared bool
	VarDeclared    bool
	MakeTargets    []string
	RunnerCalls    int
}

// adjudicateHooksNoticeReach — суждение, отделённое от чтения дерева: иначе
// доказать способность гейта упасть можно было бы только испортив дерево.
func adjudicateHooksNoticeReach(r hooksNoticeReach) []string {
	var out []string
	if !r.TargetDeclared {
		out = append(out, "цель `hooks-notice` в "+hooksNoticeMakefile+" не объявлена: "+
			"тихого режима нет вовсе, и непровязанный клон молчит")
	}
	if !r.VarDeclared {
		out = append(out, "переменная HOOKS_NOTICE не объявлена: навешивать напоминание "+
			"на цели нечем")
	}
	if len(r.MakeTargets) == 0 && r.RunnerCalls == 0 {
		out = append(out, "напоминание о непровязанном клоне НЕДОСТИЖИМО: ни одна цель "+
			hooksNoticeMakefile+" не несёт $(HOOKS_NOTICE), и "+hooksNoticeRunner+
			" не зовёт `install.sh notice`.\n"+
			"    Тогда непровязанность снова ненаблюдаема: отправка проходит молча и "+
			"при работающем хуке, и при отсутствующем, а конвейер станет первым "+
			"читателем кода. Найдено это было один раз и случайно (#1051); второй "+
			"раз случая может не быть.")
		return out
	}
	if r.RunnerCalls == 0 {
		out = append(out, hooksNoticeRunner+" не зовёт `install.sh notice`.\n"+
			"    Это ЕДИНСТВЕННЫЙ путь, покрывающий того, кто гоняет прогонщик руками "+
			"непосредственно перед отправкой: его запуск руками и есть признак того, "+
			"что хука может не быть — провязанный клон позвал бы прогонщик сам.")
	}
	if len(r.MakeTargets) == 0 {
		out = append(out, "ни одна цель "+hooksNoticeMakefile+" не несёт $(HOOKS_NOTICE).\n"+
			"    Тогда напоминания не увидит тот, кто гоняет пробы через make, — а это "+
			"обычный путь перед отправкой.")
	}
	return out
}

func TestUnwiredCloneStaysAudible(t *testing.T) {
	root := repoRoot(t)

	read := func(rel string) string {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s не читается: %v — гейт не может судить о дереве, которого не видит",
				rel, err)
		}
		return string(body)
	}

	mk := read(hooksNoticeMakefile)
	runner := read(hooksNoticeRunner)

	reach := hooksNoticeReach{
		TargetDeclared: reHooksNoticeTarget.MatchString(mk),
		VarDeclared:    reHooksNoticeVar.MatchString(mk),
		RunnerCalls:    len(reRunnerNotice.FindAllString(runner, -1)),
	}
	for _, m := range reHooksNoticeUser.FindAllStringSubmatch(mk, -1) {
		reach.MakeTargets = append(reach.MakeTargets, m[1])
	}

	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: %s — %d байт, %s — %d байт; цель объявлена %v, "+
		"переменная объявлена %v; целей с $(HOOKS_NOTICE): %d (%s); вызовов из "+
		"прогонщика: %d",
		hooksNoticeMakefile, len(mk), hooksNoticeRunner, len(runner),
		reach.TargetDeclared, reach.VarDeclared,
		len(reach.MakeTargets), strings.Join(reach.MakeTargets, ", "), reach.RunnerCalls)

	if len(mk) == 0 || len(runner) == 0 {
		t.Fatal("один из осматриваемых файлов прочитан как ноль байт — «ноль находок» " +
			"означало бы «ноль прочитанного»")
	}
	for _, finding := range adjudicateHooksNoticeReach(reach) {
		t.Error(finding)
	}
}
