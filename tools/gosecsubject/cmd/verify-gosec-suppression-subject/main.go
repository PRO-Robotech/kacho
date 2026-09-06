// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verify-gosec-suppression-subject — точка входа гейта «у подавления gosec
// обязан быть предмет».
//
// Использование:
//
//	verify-gosec-suppression-subject [корень] [каталог артефактов скана]
//
// Артефакты оставляет scripts/gosec-scan-modules.sh: по модулю — отчёт с
// `-track-suppressions` и журнал скана, плюс перечень
// gosec-suppressions-manifest.txt, связывающий их с каталогами модулей.
//
// ИСХОДОВ ТРИ, А НЕ ДВА, и третий не засчитывается в зелёное:
//
//	0 — у каждой судимой директивы есть предмет либо её называет ведомость,
//	    и каждая запись ведомости свой предмет имеет;
//	1 — находки, перечисленные координатами;
//	2 — гейт не смог сделать свою работу: скан не оставил отчётов, журнал
//	    молчит, распознаватель разошёлся со сканером. «Не выполнилось» — не
//	    вердикт и не чистота.
//
// Перепись печатается ПЕРВОЙ в любом исходе: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
package main

import (
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/tools/gosecsubject"
)

func main() {
	root, artifacts := ".", "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if len(os.Args) > 2 {
		artifacts = os.Args[2]
	}

	rep, err := gosecsubject.Scan(gosecsubject.Options{
		Root:      root,
		Artifacts: artifacts,
		Ledger:    "tools/gosecsubject/known-inert.tsv",
	})
	gosecsubject.Print(rep, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "::error::gosec-suppression-subject: %v\n", err)
		os.Exit(2)
	}
	if len(rep.Uncovered) == 0 && len(rep.Stale) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "::error::gosec-suppression-subject: подавлений без предмета %d, "+
		"устаревших записей ведомости %d (координаты выше).\n",
		len(rep.Uncovered), len(rep.Stale))
	fmt.Fprintln(os.Stderr, "Исходы для директивы без предмета ровно три: удалить её "+
		"(она не подавляет ничего, а читается как принятое решение) · починить код по "+
		"существу, если правило перестало срабатывать случайно · внести в ведомость "+
		"tools/gosecsubject/known-inert.tsv с ПРИЧИНОЙ, если снятие идёт отдельной работой.")
	fmt.Fprintln(os.Stderr, "Запись ведомости, которой больше нечего исключать, удаляется "+
		"тем же изменением, которым исчез её предмет: послабление, пережившее свой "+
		"предмет, — это и есть класс, ради которого гейт заведён.")
	os.Exit(1)
}
