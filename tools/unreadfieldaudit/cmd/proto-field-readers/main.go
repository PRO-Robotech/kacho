// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command proto-field-readers печатает JSON-индекс чтений полей proto-сообщений,
// атрибутированный ТИПОМ ПОЛУЧАТЕЛЯ вызова (см. пакет protofieldreaders).
//
// Индекс потребляет `tools/unreadfieldaudit/unread_field_audit.py`; он же его и
// запускает, поэтому руками команду звать нужно только для отладки:
//
//	go run ./tools/unreadfieldaudit/cmd/proto-field-readers [пакетный шаблон …]
//
// Коды возврата: 0 — индекс построен; 2 — предпосылка не выполнена (обход не
// состоялся либо какой-то пакет не протипизировался, то есть его чтения
// невидимы). Кода «частично годен» нет намеренно: индекс с невидимыми чтениями
// превращает молчание в находки.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/tools/unreadfieldaudit/protofieldreaders"
)

func main() {
	ix, err := protofieldreaders.Build(os.Args[1:]...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "предпосылка не выполнена:", err)
		os.Exit(2)
	}
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(ix); err != nil {
		fmt.Fprintln(os.Stderr, "не удалось напечатать индекс:", err)
		os.Exit(2)
	}
	if len(ix.Errors) > 0 {
		for _, e := range ix.Errors {
			fmt.Fprintln(os.Stderr, "НЕ ПРОТИПИЗИРОВАН (чтения невидимы):", e)
		}
		os.Exit(2)
	}
}
