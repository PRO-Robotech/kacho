// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// adjudicate-declared-breaks — точка входа конвейера для гейта объявленных ломающих
// изменений контракта.
//
// Использование: buf breaking … --error-format=json | adjudicate-declared-breaks <перечень>
//
// Кодов возврата ТРИ, а не два, потому что «гейт ничего не нашёл» и «гейт ничего не
// прочитал» не должны выглядеть одинаково:
//
//	0 — каждый разрыв объявлен, и у каждого объявления есть предмет;
//	1 — находки, перечислены по координатам;
//	2 — гейт не смог сделать свою работу (вывод buf не разобран, перечень не прочитан).
//
// Каким бы исход ни был, ПЕРЕПИСЬ печатается первой: вердикт без объёма, стоящего за
// ним, — тот самый класс утверждений, который этот гейт и ловит.
package main

import (
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/tools/declaredbreak"
)

func main() {
	list := "proto/declared-breaks.yaml"
	if len(os.Args) > 1 && os.Args[1] != "" {
		list = os.Args[1]
	}

	findings, err := declaredbreak.ParseFindings(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "declared-break: гейт не смог прочитать вывод buf: %v\n", err)
		os.Exit(2)
	}
	decls, err := declaredbreak.LoadDeclarations(list)
	if err != nil {
		fmt.Fprintf(os.Stderr, "declared-break: гейт не смог прочитать перечень: %v\n", err)
		os.Exit(2)
	}

	res := declaredbreak.Adjudicate(findings, decls)

	// Самопротиворечивый вердикт — третий исход, а не находки: гейт не вправе
	// утверждать о контракте, которого не сопоставил. Проверяется ДО печати отчёта,
	// иначе читатель получит перечень «необъявленных разрывов», не проверенных ничем.
	if res.Incoherent() {
		fmt.Fprintf(os.Stderr,
			"declared-break: гейт не смог работать — вердикт противоречит сам себе: "+
				"осмотрено находок %d, записей перечня %d, сопоставлено 0, и при этом ВСЕ %d "+
				"записи объявлены истёкшими. Обе половины разом верны быть не могут.\n"+
				"Наиболее вероятная причина — форма путей: `buf breaking` из корня репозитория "+
				"печатает путь с сегментом каталога контрактов впереди, изнутри каталога — без "+
				"него, а перечень написан во второй форме. Запускай так же, как конвейер: из "+
				"каталога контрактов.\n",
			res.FindingsRead, res.DeclarationsRead, res.DeclarationsRead)
		os.Exit(2)
	}

	fmt.Print(res.Report())
	if !res.Clean() {
		os.Exit(1)
	}
}
