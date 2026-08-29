// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Перечень режимов шифрования до собственной базы объявлен В ДЕРЕВЕ ОДИН РАЗ.
//
// # ПРЕДМЕТ
//
// «Какие значения `sslmode` считать безопасными» — правило безопасности: страж
// старта отказывает в пуске, когда до базы идёт открытый канал (`security.md`
// §Production-mode, CWE-319). Пока перечень объявлен у каждого сервиса своими
// руками, это не «второе место вызова», а второй ИСТОЧНИК: копии перечисляют
// значения сами, сойтись им нечем, и расходятся они молча.
//
// # ПОЧЕМУ ГЕЙТ, А НЕ ОБЗОР
//
// Замер на день заведения (задача продукта #1464): живых боевых проверок с
// собственным перечнем — ЧЕТЫРЕ, и ДВЕ из них лежали вне `services/*/internal`,
// то есть за границей предиката, которым класс искали. Обзор диффа этого не
// видит by construction: каждая копия по отдельности написана верно.
//
// # ЧТО ГЕЙТ СУДИТ, А ЧТО НЕТ
//
// Судит СТРОКОВЫЙ ЛИТЕРАЛ разобранного дерева. Комментарий, называющий режимы
// (`// require|verify-ca|verify-full`), законен и остаётся: поиск по подстроке
// краснел бы на собственном объяснении гейта.
//
// НЕ судит, ЧТО именно вызывающий делает с ответом предиката — только то, что
// перечень он не переписывает. Место проверки в порядке старта (до открытия
// пула или после) — предмет другой и здесь не решается.
func TestSSLModeAllowlistHasASingleSource(t *testing.T) {
	root := repoRoot(t)

	var files []string
	for _, sub := range []string{"services", "gateway", "pkg", "internal", "tools", "terraform"} {
		for _, abs := range trackedGoFiles(t, filepath.Join(root, sub)) {
			rel, err := filepath.Rel(root, abs)
			if err != nil {
				t.Fatalf("относительный путь %s: %v", abs, err)
			}
			files = append(files, filepath.ToSlash(rel))
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("предпосылка гейта не выполняется: не-тестовых файлов Go в индексе НОЛЬ — " +
			"обход смотрит не туда; чинить надо гейт, а не молча выходить успехом")
	}

	findings, cen, err := auditSSLModeSingleSource(root, files)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}

	var byValue []string
	for v, n := range cen.ByValue {
		byValue = append(byValue, v+"×"+strconv.Itoa(n))
	}
	sort.Strings(byValue)
	t.Logf("осмотрено файлов Go: %d; литералов словаря: %d; файлов дома (%s): %d; "+
		"мест вне дома: %d; по значениям: %s",
		cen.FilesRead, cen.LiteralsSeen, sslModeHomeDir, cen.HomeFiles,
		len(findings), strings.Join(byValue, " "))

	// СТРАЖ ПРЕДПОСЫЛКИ — САМОПРОВЕРКА РАСПОЗНАВАТЕЛЯ, А НЕ СЧЁТ НАХОДОК.
	//
	// Требовать непустоты находок нельзя: ноль — это ЦЕЛЬ гейта, и такое
	// требование обещало бы красное на её достижении. Годный страж спрашивает
	// другое: жив ли распознаватель. Он подаёт судящей функции синтетический
	// предмет по каждой оси и требует находки — это не зависит ни от состава
	// дерева, ни от того, сколько копий осталось.
	for _, probe := range []struct {
		axis string
		src  string
	}{
		{"однозначный литерал", `package p

func secure(mode string) bool { return mode == "verify-full" }
`},
		{"перечисление", `package p

func known(mode string) bool {
	switch mode {
	case "disable", "require":
		return true
	}
	return false
}
`},
	} {
		dir := t.TempDir()
		rel := "services/probe/x.go"
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(probe.src), 0o600); err != nil {
			t.Fatal(err)
		}
		got, _, perr := auditSSLModeSingleSource(dir, []string{rel})
		if perr != nil {
			t.Fatalf("самопроверка оси «%s»: %v", probe.axis, perr)
		}
		if len(got) == 0 {
			t.Fatalf("предпосылка гейта не выполняется: распознаватель ОСЛЕП по оси «%s» — "+
				"синтетический предмет этой оси не найден.\n«Ноль находок» по дереву в таком "+
				"состоянии не утверждает ничего.\nперепись по дереву: %+v", probe.axis, cen)
		}
	}

	// Вторая половина предпосылки: дом на месте. Ноль файлов дома означает, что
	// перечень уехал (или распознаватель перестал его видеть) — и тогда «ноль
	// находок вне дома» верно ровно потому, что предмета нет нигде.
	if cen.HomeFiles == 0 {
		t.Fatalf("предпосылка гейта не выполняется: в доме %s НЕТ ни одного файла со словарём "+
			"режимов. Либо перечень переехал, либо распознаватель его не видит; «ноль копий» "+
			"в таком состоянии не утверждает ничего", sslModeHomeDir)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n    " + f.Where + " — перечисляет " + strings.Join(f.Values, ", "))
		}
		t.Fatalf("перечень безопасных значений sslmode объявлен вне общего дома (%s).\n"+
			"Это правило БЕЗОПАСНОСТИ, а копия — второй его ИСТОЧНИК: сойтись копиям нечем, "+
			"расходятся они молча, и «эту ось здесь забыли» становится неотличимо от «эту ось "+
			"здесь решили не судить». Спрашивай предикат дома (`db.SSLModeSecure` / "+
			"`db.SSLModeConfigurable`), тексты собирай из `db.SecureSSLModes()` / "+
			"`db.ConfigurableSSLModes()`:%s", sslModeHomeDir, b.String())
	}
}
