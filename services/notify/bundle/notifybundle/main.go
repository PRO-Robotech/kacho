// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command notifybundle — шаг сборки шаблонов notify: цели
// `make -C services/notify bundle` (запись) и `bundle-check` (сверка) зовут
// его, логика — в пакете assemble.
//
//	notifybundle -root <корень репозитория> -out <каталог сборки> [-check]
//
// Код выхода: 0 — записано либо сборка равна пересборке; 1 — сборка
// устарела (каждое расхождение названо пространством и шаблоном); 2 — сборки
// нет (отказ шага) либо вызов вне формы.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/PRO-Robotech/kacho/services/notify/bundle/internal/assemble"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("notifybundle", flag.ContinueOnError)
	fl.SetOutput(stderr)
	root := fl.String("root", "", "корень репозитория")
	out := fl.String("out", "", "каталог сборки (services/notify/bundle)")
	check := fl.Bool("check", false, "сверить записанную сборку с пересборкой, ничего не писать")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if *root == "" || *out == "" || fl.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "notifybundle: вызов — notifybundle -root <корень> -out <каталог сборки> [-check]")
		return 2
	}
	o, err := assemble.Assemble(*root, assemble.Sources())
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	if !*check {
		if err := assemble.Write(*out, o); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		_, _ = fmt.Fprintf(stdout, "bundle: источников %d · шаблонов %d · файлов сборки записано %d\n",
			o.Sources, o.Templates, len(o.Files))
		return 0
	}
	findings, inspected, err := assemble.Check(*out, o)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	_, _ = fmt.Fprintf(stdout, "bundle-check: источников %d · шаблонов %d · файлов сборки ожидается %d · осмотрено %d · расхождений %d\n",
		o.Sources, o.Templates, len(o.Files), inspected, len(findings))
	if len(findings) == 0 {
		return 0
	}
	for _, f := range findings {
		_, _ = fmt.Fprintln(stdout, "  "+f.String())
	}
	_, _ = fmt.Fprintln(stdout, "bundle-check: сборка шаблонов устарела — пересоберите: make -C services/notify bundle")
	return 1
}
