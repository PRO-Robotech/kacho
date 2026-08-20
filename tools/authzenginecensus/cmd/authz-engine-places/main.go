// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Команда authz-engine-places печатает перепись мест обращения к внешнему
// движку прав (гейт Г1, сценарий R7-3-01).
//
//	go run ./tools/authzenginecensus/cmd/authz-engine-places            # отчёт
//	go run ./tools/authzenginecensus/cmd/authz-engine-places -findings  # перечень мест
//	go run ./tools/authzenginecensus/cmd/authz-engine-places -json      # машинно
//
// Коды возврата РАЗЛИЧАЮТ исходы, потому что «мест 0» и «перепись негодна» —
// разные ответы, и молчаливый успех на втором был бы ровно тем классом, против
// которого перепись и заведена:
//
//	0 — перепись снята;
//	1 — перепись снята и в ней есть находка (метод без рода);
//	2 — перепись НЕГОДНА: предпосылка не выполнена (пакет не загрузился,
//	    якорный тип не найден или объявлен не один раз).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/tools/authzenginecensus/engineplaces"
)

func main() {
	var (
		root     = flag.String("root", ".", "корень дерева")
		asJSON   = flag.Bool("json", false, "машинный вывод")
		findings = flag.Bool("findings", false, "перечень мест построчно")
	)
	flag.Parse()

	patterns := flag.Args()
	c, err := engineplaces.Build(*root, patterns...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "перепись не построилась: %v\n", err)
		os.Exit(2)
	}

	switch {
	case *asJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(c); encErr != nil {
			fmt.Fprintf(os.Stderr, "кодирование: %v\n", encErr)
			os.Exit(2)
		}
	case *findings:
		fmt.Print(c.Findings())
	default:
		fmt.Print(c.Report())
	}

	if c.Void() {
		os.Exit(2)
	}
	if len(c.UnclassifiedMethods) > 0 {
		os.Exit(1)
	}
}
