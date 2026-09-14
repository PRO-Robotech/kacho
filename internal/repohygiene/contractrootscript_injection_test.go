// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// contractrootscript_injection_test.go — способность гейта упасть и смолчать
// доказывается ИНЪЕКЦИЕЙ настоящим входом, а не прочтением.
//
// Каждая пара отличается РОВНО ОДНИМ фактом: назван ли домен. Формы записи
// перечислены ПОИМЁННО и доказаны каждая: форма, о которой распознаватель не
// знает, даёт не красное и не зелёное, а МОЛЧАНИЕ, — и именно так класс жил в
// оболочке до #2343.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/contractroot"
	"github.com/PRO-Robotech/corelib/gitenv"
)

// synthScriptTree — синтетическое дерево с одним скриптом.
//
// Дерево — НАСТОЯЩИЙ репозиторий, и файл в нём отслеживается: состав обхода
// берётся у индекса, поэтому неотслеживаемая фикстура дала бы пустую популяцию,
// то есть зелёную инъекцию, доказывающую ноль.
func synthScriptTree(t *testing.T, name, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("фикстура не собрана: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("фикстура не собрана: %v", err)
	}
	for _, args := range [][]string{{"init", "-q", "."}, {"add", "-A"}} {
		if out, err := gitenv.Command(root, args...).CombinedOutput(); err != nil {
			t.Fatalf("фикстура не собрана (git %v): %v\n%s", args, err, out)
		}
	}
	return root
}

// root0 — объявленный корень, СОБРАННЫЙ, а не выписанный: выписанный литерал
// лежал бы в исходнике этого файла и участвовал бы в собственной проверке.
func root0() string { return contractroot.Roots[0] }

// scriptFindings — находки гейта на синтетическом дереве.
func scriptFindings(t *testing.T, name, body string) ([]ContractRootScriptFinding, ContractRootScriptCensus) {
	t.Helper()
	root := synthScriptTree(t, name, body)
	f, c, err := AuditContractRootScripts(root)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	return f, c
}

// ─────────────────────────────────────────────────────────────────────────────
// ПИТОН
// ─────────────────────────────────────────────────────────────────────────────

// TestInjectionScript_PythonPathRootIsAFinding — путь, оборвавшийся на корне.
// Это ровно живой дефект `tools/unreadfieldaudit/unread_field_audit.py` (#2339).
func TestInjectionScript_PythonPathRootIsAFinding(t *testing.T) {
	t.Parallel()
	body := "PROTO_ROOT = \"proto/" + root0() + "/cloud\"\n"
	f, c := scriptFindings(t, "probe.py", body)
	if len(f) != 1 {
		t.Fatalf("отбор популяции литералом корня не найден: находок %d (%s) — "+
			"гейт потерял способность падать", len(f), c)
	}
	got := f[0].String()
	if !strings.Contains(got, "scripts/probe.py") {
		t.Errorf("находка не называет координаты: %s", got)
	}
	if f[0].Line != 1 {
		t.Errorf("находка называет строку %d вместо 1: разбор потерял позицию", f[0].Line)
	}
}

// TestInjectionScript_PythonPathWithDomainIsSilent — ЗАКОННЫЙ БЛИЗНЕЦ.
// Отличие РОВНО ОДНО: домен НАЗВАН, то есть литерал адресует одного члена.
func TestInjectionScript_PythonPathWithDomainIsSilent(t *testing.T) {
	t.Parallel()
	body := "PROTO_DIR = \"proto/" + root0() + "/cloud/compute/v1\"\n"
	f, c := scriptFindings(t, "probe.py", body)
	if c.ByLang[langPython].Lexemes == 0 {
		t.Fatalf("лексем не выделено вовсе: %s — близнец вакуумен", c)
	}
	if len(f) != 0 {
		t.Fatalf("имя ЧЛЕНА объявлено находкой: %v — гейт ловит форму, а не существо", f)
	}
}

// TestInjectionScript_PythonWildcardInPlaceOfDomainIsAFinding — на месте домена
// звёздочка: литерал отбирает ВСЁ под одним корнем.
func TestInjectionScript_PythonWildcardInPlaceOfDomainIsAFinding(t *testing.T) {
	t.Parallel()
	body := "D = glob.glob(\"proto/" + root0() + "/cloud/*/v1\")\n"
	f, _ := scriptFindings(t, "probe.py", body)
	if len(f) != 1 {
		t.Fatalf("звёздочка на месте домена не найдена: находок %d — форма вне наблюдения", len(f))
	}
}

// TestInjectionScript_PythonDocstringIsProseNotSelection — ПРОЗА молчит, а тот
// же текст в ИСПОЛНЯЕМОЙ позиции — находка.
//
// Обе половины обязательны: без первой гейт краснел бы на объяснении класса
// (весь корпус объясняет корни словами), без второй «молчит на прозе» было бы
// неотличимо от «не видит этой формы вовсе».
func TestInjectionScript_PythonDocstringIsProseNotSelection(t *testing.T) {
	t.Parallel()
	lit := "proto/" + root0() + "/cloud"

	prose := "\"\"\"Обход идёт по " + lit + " и это объяснение, а не отбор.\"\"\"\n"
	f, c := scriptFindings(t, "probe.py", prose)
	if c.ByLang[langPython].Lexemes == 0 {
		t.Fatalf("строковых лексем не выделено: %s", c)
	}
	if len(f) != 0 {
		t.Fatalf("ПРОЗА объявлена находкой: %v — гейт краснеет на собственном "+
			"объяснении класса", f)
	}

	used := "ROOT = \"" + lit + "\"\n"
	f2, _ := scriptFindings(t, "probe.py", used)
	if len(f2) != 1 {
		t.Fatalf("положительный контроль пары не сработал: тот же литерал в "+
			"ИСПОЛНЯЕМОЙ позиции дал находок %d — «молчит на прозе» неотличимо "+
			"от «не видит этой формы»", len(f2))
	}
}

// TestInjectionScript_PythonCommentIsSilent — комментарий отбором не является.
func TestInjectionScript_PythonCommentIsSilent(t *testing.T) {
	t.Parallel()
	body := "# перечень берётся из proto/" + root0() + "/cloud — так было до починки\nX = 1\n"
	f, _ := scriptFindings(t, "probe.py", body)
	if len(f) != 0 {
		t.Fatalf("КОММЕНТАРИЙ объявлен находкой: %v", f)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ОБОЛОЧКА
// ─────────────────────────────────────────────────────────────────────────────

// TestInjectionScript_ShellBareWordIsAFinding — в оболочке путь пишется ГОЛЫМ.
// Судить одни строковые литералы значило бы не видеть обычной формы записи.
func TestInjectionScript_ShellBareWordIsAFinding(t *testing.T) {
	t.Parallel()
	body := "#!/usr/bin/env bash\nfind proto/" + root0() + "/cloud -name '*.proto'\n"
	f, _ := scriptFindings(t, "probe.sh", body)
	if len(f) != 1 {
		t.Fatalf("голое слово-путь не найдено: находок %d — обычная форма записи "+
			"оболочки осталась вне наблюдения", len(f))
	}
	if f[0].Lang != langShell {
		t.Errorf("находка отнесена к языку %q вместо %q", f[0].Lang, langShell)
	}
}

// TestInjectionScript_ShellRegexEscapedPrefixIsAFinding — ФОРМА, в которой класс
// и наблюдался: приставка внутри регулярного выражения sed, точки экранированы.
//
// Распознаватель, не знающий её, был бы слеп ровно к тому дефекту, ради которого
// заведён, — и слеп МОЛЧА. Все три исторических места оболочки (#2343) записаны
// именно так.
func TestInjectionScript_ShellRegexEscapedPrefixIsAFinding(t *testing.T) {
	t.Parallel()
	body := "#!/usr/bin/env bash\n" +
		`sed -n 's/.*"fqn": "` + root0() + `\.cloud\.\([a-z0-9_]*\)\..*/\1/p' "$OUT"` + "\n"
	f, _ := scriptFindings(t, "probe.sh", body)
	if len(f) != 1 {
		t.Fatalf("ЭКРАНИРОВАННАЯ форма приставки не найдена: находок %d — "+
			"распознаватель слеп к той форме, в которой класс наблюдался", len(f))
	}
}

// TestInjectionScript_ShellNamedDomainInRegexIsSilent — ЗАКОННЫЙ БЛИЗНЕЦ той же
// экранированной формы: домен НАЗВАН, значит адресуется один член.
func TestInjectionScript_ShellNamedDomainInRegexIsSilent(t *testing.T) {
	t.Parallel()
	body := "#!/usr/bin/env bash\n" +
		`grep -c '` + root0() + `\.cloud\.compute\.v1' "$OUT"` + "\n"
	f, c := scriptFindings(t, "probe.sh", body)
	if c.ByLang[langShell].Lexemes == 0 {
		t.Fatalf("слов не выделено вовсе: %s — близнец вакуумен", c)
	}
	if len(f) != 0 {
		t.Fatalf("названный домен в той же экранированной форме объявлен находкой: %v", f)
	}
}

// TestInjectionScript_ShellTrustDomainIsSilent — ЗАКОННЫЙ БЛИЗНЕЦ из дерева.
//
// `spiffe://<корень>.cloud/...` — доменное имя ДОВЕРИЯ, а не дерево контрактов.
// Замер 2026-09-08: таких вхождений 24 файла, и НИ ОДНО не про контракты. Гейт,
// краснеющий на верном коде, отключают первым.
func TestInjectionScript_ShellTrustDomainIsSilent(t *testing.T) {
	t.Parallel()
	body := "#!/usr/bin/env bash\nSAN=\"spiffe://" + root0() + ".cloud/ns/x/sa/y\"\n" +
		"ISS=\"https://hydra.api." + root0() + ".cloud\"\n"
	f, c := scriptFindings(t, "probe.sh", body)
	if c.ByLang[langShell].Lexemes == 0 {
		t.Fatalf("слов не выделено вовсе: %s", c)
	}
	if len(f) != 0 {
		t.Fatalf("доменное имя ДОВЕРИЯ объявлено находкой: %v — гейт судит "+
			"написание, а не предмет", f)
	}
}

// TestInjectionScript_ShellCommentIsSilentAndTheSameWordIsNot — пара: тот же
// текст в комментарии молчит, в исполняемой позиции — находка.
func TestInjectionScript_ShellCommentIsSilentAndTheSameWordIsNot(t *testing.T) {
	t.Parallel()
	lit := "proto/" + root0() + "/cloud"

	f, _ := scriptFindings(t, "probe.sh", "#!/usr/bin/env bash\n# было: "+lit+"\ntrue\n")
	if len(f) != 0 {
		t.Fatalf("КОММЕНТАРИЙ объявлен находкой: %v — гейт краснел бы на объяснении "+
			"собственного класса", f)
	}
	f2, _ := scriptFindings(t, "probe.sh", "#!/usr/bin/env bash\nls "+lit+"\n")
	if len(f2) != 1 {
		t.Fatalf("положительный контроль пары не сработал: тот же путь вне "+
			"комментария дал находок %d", len(f2))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДПОСЫЛКИ И ПЕРЕПИСЬ
// ─────────────────────────────────────────────────────────────────────────────

// TestInjectionScript_EmptyTreeIsRefusedNotSilent — дерево без отслеживаемых
// файлов даёт ОТКАЗ, а не пустой зелёный.
func TestInjectionScript_EmptyTreeIsRefusedNotSilent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if out, err := gitenv.Command(root, "init", "-q", ".").CombinedOutput(); err != nil {
		t.Fatalf("фикстура не собрана: %v\n%s", err, out)
	}
	if _, _, err := AuditContractRootScripts(root); err == nil {
		t.Fatal("пустое дерево принято молча — «ноль находок» стало бы неотличимо " +
			"от «ноль прочитанного»")
	}
}

// TestInjectionScript_CensusCountsLanguagesApart — перепись ведётся ПО ЯЗЫКАМ
// порознь: одно суммарное число скрывает язык, чьи файлы перестали попадать в
// обход.
func TestInjectionScript_CensusCountsLanguagesApart(t *testing.T) {
	t.Parallel()
	root := synthScriptTree(t, "probe.py", "ROOT = \"proto/"+root0()+"/cloud\"\n")
	if err := os.WriteFile(filepath.Join(root, "scripts", "probe.sh"),
		[]byte("#!/usr/bin/env bash\ntrue\n"), 0o600); err != nil {
		t.Fatalf("фикстура не собрана: %v", err)
	}
	if out, err := gitenv.Command(root, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("фикстура не собрана: %v\n%s", err, out)
	}
	_, c, err := AuditContractRootScripts(root)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if c.ByLang[langPython].Findings != 1 {
		t.Errorf("находка не отнесена к питону: %s", c)
	}
	if c.ByLang[langShell].Findings != 0 {
		t.Errorf("находка приписана оболочке: %s", c)
	}
	if c.ByLang[langShell].Files != 1 || c.ByLang[langPython].Files != 1 {
		t.Errorf("файлы не разнесены по языкам: %s", c)
	}
	if !strings.Contains(c.String(), langShell) || !strings.Contains(c.String(), langPython) {
		t.Errorf("перепись не называет оба языка порознь: %s", c)
	}
}

// TestInjectionScript_SecondRootIsJudgedToo — оба объявленных корня судятся.
// Гейт, знающий один корень, повторял бы дефект, который чинит.
//
// # Один корень — законное состояние, и проба на нём ПРОХОДИТ (#2548)
//
// Здесь стоял `t.Skip("в дереве объявлен один корень — у оси нет предмета")`.
// Причина не была объявлена в `.github/scripts/gate-skips-allowed.txt`, а
// необъявленный пропуск у гейта дерева — находка прогонщика юнитов. То есть
// схлопывание дерева контрактов к одному корню — состояние, к которому разрез
// как раз и ведёт, — уронило бы прогон не на дефекте.
//
// Ось второго корня на одном корне беспредметна BY CONSTRUCTION: второго литерала
// не из чего построить. Это не повод пропускать, а повод СКАЗАТЬ переписью,
// сколько корней осмотрено. Пустой перечень корней при этом остаётся отказом:
// «корней ноль» означает, что источник корней не прочитан, а не что их нет.
func TestInjectionScript_SecondRootIsJudgedToo(t *testing.T) {
	t.Parallel()
	if len(contractroot.Roots) == 0 {
		t.Fatal("объявленных корней дерева контрактов ноль — предпосылка разбора не " +
			"выполнена: источник корней не прочитан. «Корней нет» и «корень один» — " +
			"разные состояния, и второе судится, а первое чинится")
	}
	if len(contractroot.Roots) < 2 {
		t.Logf("перепись: объявленных корней %d — ось второго корня беспредметна "+
			"by construction (второго литерала не из чего построить). Это ЦЕЛЬ "+
			"разреза, а не поломка: проба на ней проходит", len(contractroot.Roots))
		return
	}
	body := "ROOT = \"proto/" + contractroot.Roots[1] + "/cloud\"\n"
	f, _ := scriptFindings(t, "probe.py", body)
	if len(f) != 1 {
		t.Fatalf("литерал ВТОРОГО корня не найден: находок %d — гейт знает только "+
			"первый корень и повторяет чинимый дефект", len(f))
	}
}
