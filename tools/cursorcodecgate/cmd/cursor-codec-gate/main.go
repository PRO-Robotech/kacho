// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command cursor-codec-gate проверяет, что формат курсора страницы объявлен в одном
// месте. Исходов три, и они различимы по коду возврата: 0 — находок нет; 1 — находки
// есть либо послабление истекло; 2 — предпосылка гейта не выполнена (он ничего не
// прочитал или ничего не распознал), то есть вердикта НЕТ.
package main

import (
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/tools/cursorcodecgate"
)

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"services", "gateway", "pkg"}
	}
	rep, err := cursorcodecgate.Analyse(roots...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cursor-codec-gate:", err)
		os.Exit(2)
	}
	fmt.Println(rep.String())

	if !rep.PremiseHolds() {
		fmt.Fprintln(os.Stderr,
			"ОТКАЗ: предпосылка не выполнена — гейт не прочитал дерево либо не распознал ни одного вызова base64.")
		fmt.Fprintln(os.Stderr,
			"       «ноль находок» в этом состоянии не значит ничего; проверьте корни и распознаватель.")
		os.Exit(2)
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(os.Stderr, "НАХОДКА %s:%d (%s): %s\n", f.File, f.Line, f.Func, f.Why)
	}
	for _, s := range rep.StaleExclusions {
		fmt.Fprintf(os.Stderr,
			"ИСТЁКШЕЕ ПОСЛАБЛЕНИЕ %s: файл больше не объявляет формат курсора — снимите запись из Remaining\n", s)
	}
	if len(rep.Findings) > 0 || len(rep.StaleExclusions) > 0 {
		os.Exit(1)
	}
	fmt.Println("OK: формат курсора объявлен в", cursorcodecgate.Home)
}
