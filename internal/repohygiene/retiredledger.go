// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RetiredLedgerName — имя надгробия рядом с миграциями сервиса. Объявлено ЗДЕСЬ
// в единственном экземпляре: второй потребитель (проверка свежести документации
// в воркспейсе) читает то же имя, и разойтись эти два объявления могут только
// молча — поэтому имя названо константой, а не выписано по месту.
const RetiredLedgerName = "retired.json"

// RetiredLedger — надгробие сведённых миграций сервиса.
//
// Это НЕ архив и не история: историю несёт git, а состав сведения — заголовок
// самой сводной миграции. Надгробие есть ВЕДОМОСТЬ ПОСЛАБЛЕНИЯ: оно существует
// ровно затем, чтобы проверка свежести документации не считала находкой имя
// миграции, которое живой документ называет как исторический след. Отсюда два
// его свойства, и оба проверяются:
//
//   - запись не вправе называть миграцию, которая в каталоге ЕСТЬ, — такая
//     запись прикрывала бы живую координату (гейт ниже);
//   - запись не вправе пережить свои цитаты — запись, которую не называет ни
//     один живой документ, есть послабление без предмета (проверка свежести,
//     она одна видит корпус обоих репозиториев).
type RetiredLedger struct {
	// Service — имя сервиса; обязано совпадать с каталогом, в котором лежит
	// надгробие. Расхождение означает надгробие, приехавшее из чужого сервиса.
	Service string `json:"service"`
	// ConsolidatedInto — имя сводной миграции, поглотившей перечисленные.
	// Обязано существовать в том же каталоге: надгробие без своей сводной
	// миграции утверждает сведение, которого не было.
	ConsolidatedInto string `json:"consolidated_into"`
	// Retired — имена файлов миграций, которых в каталоге больше нет.
	Retired []string `json:"retired"`
}

// ReadRetiredLedger читает надгробие каталога миграций.
//
// Отсутствие файла — НЕ ошибка и не находка: каталог, который не сводили,
// надгробия не несёт, и это норма. Возвращается (nil, nil).
func ReadRetiredLedger(dir string) (*RetiredLedger, error) {
	// #nosec G304 — путь собирается из каталога, названного вызывающим, и КОНСТАНТЫ
	// имени надгробия: пользовательской части в нём нет by construction.
	b, err := os.ReadFile(filepath.Join(dir, RetiredLedgerName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l RetiredLedger
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", RetiredLedgerName, err)
	}
	return &l, nil
}

// RetiredLedgerViolations судит одно надгробие против каталога, в котором оно
// лежит. `present` — имена файлов миграций, которые в каталоге ЕСТЬ.
func RetiredLedgerViolations(svc string, l *RetiredLedger, present map[string]bool) []string {
	if l == nil {
		return nil
	}
	var out []string
	if l.Service != svc {
		out = append(out, fmt.Sprintf(
			"%s: %s объявляет сервис %q — надгробие лежит не в своём каталоге",
			svc, RetiredLedgerName, l.Service))
	}
	if l.ConsolidatedInto == "" {
		out = append(out, fmt.Sprintf(
			"%s: %s не называет сводную миграцию — надгробие утверждает сведение, которого нельзя проверить",
			svc, RetiredLedgerName))
	} else if !present[l.ConsolidatedInto] {
		out = append(out, fmt.Sprintf(
			"%s: %s называет сводную миграцию %q, которой в каталоге нет",
			svc, RetiredLedgerName, l.ConsolidatedInto))
	}
	if len(l.Retired) == 0 {
		out = append(out, fmt.Sprintf(
			"%s: %s не называет ни одной снятой миграции — послабление без предмета",
			svc, RetiredLedgerName))
	}
	seen := map[string]bool{}
	names := append([]string(nil), l.Retired...)
	sort.Strings(names)
	for _, n := range names {
		if seen[n] {
			out = append(out, fmt.Sprintf("%s: %s называет %q дважды", svc, RetiredLedgerName, n))
			continue
		}
		seen[n] = true
		if n == l.ConsolidatedInto {
			out = append(out, fmt.Sprintf(
				"%s: %s числит сводную миграцию %q среди снятых", svc, RetiredLedgerName, n))
			continue
		}
		if present[n] {
			out = append(out, fmt.Sprintf(
				"%s: %s числит снятой миграцию %q, а она в каталоге ЕСТЬ — "+
					"эта запись прикрыла бы живую координату, то есть стала бы маской",
				svc, RetiredLedgerName, n))
		}
	}
	return out
}
