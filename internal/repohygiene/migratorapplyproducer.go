// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"sort"
	"strings"
)

// migratorApplyProofPkg — пакет, в котором живёт доказательство наката: он
// собирает КАЖДУЮ точку наката из дерева и гоняет её против живой базы.
//
// Путь назван здесь один раз. Он же стоит в PG_OUTSIDE_SELECTION_PKGS корневого
// Makefile и в shortGatedRunByOwnCIStep — три места об одном предмете, и
// расхождение между вторым и третьим уже держит pgoutsideselection_test.go.
// Здесь держится то, чего не держит никто: что предмет вообще есть и что у него
// есть производитель.
const migratorApplyProofPkg = "internal/migratorapply"

// judgeMigratorApplyProducer — чистый предикат гейта.
//
// migrators — точки наката, найденные обходом дерева (rel-пути вида
// `services/<svc>/cmd/migrator`). proofPkgTests — сколько файлов проб несёт
// пакет доказательства (ноль означает «пакета нет либо он пуст»).
// producerPkgs — перечень PG_OUTSIDE_SELECTION_PKGS корневого Makefile.
//
// Возвращает находки. Пустой срез — свойство держится.
func judgeMigratorApplyProducer(migrators []string, proofPkgTests int, producerPkgs []string) []string {
	var out []string

	named := false
	for _, p := range producerPkgs {
		if p == migratorApplyProofPkg {
			named = true
			break
		}
	}

	// Предпосылка. Гейт обоснован тем, что точки наката в дереве ЕСТЬ; если их
	// нет, он ничего не прочитал — и молчание было бы неотличимо от свойства.
	if len(migrators) == 0 {
		out = append(out, "точек наката в дереве НЕ НАЙДЕНО — обход пуст, вердикт беспредметен. "+
			"Это отказ, а не «нечего проверять»: гейт судит о производителе доказательства, "+
			"а доказывать оказалось нечего.")
		// Единственная законная причина отсутствия предмета у записи производителя —
		// исчезновение самих точек наката. Тогда запись обязана уйти следом.
		if named {
			out = append(out, fmt.Sprintf(
				"PG_OUTSIDE_SELECTION_PKGS называет %s, но точек наката в дереве нет — "+
					"запись пережила свой предмет и подлежит снятию вместе с пакетом.",
				migratorApplyProofPkg))
		}
		return out
	}

	sorted := append([]string(nil), migrators...)
	sort.Strings(sorted)

	switch {
	case proofPkgTests == 0 && named:
		// Тише всех остальных случаев и потому хуже: цель гонит пакет, которого
		// нет, — и `go test` на несуществующем пакете роняет саму цель. Называем
		// прямо, чтобы отказ цели не разбирали как поломку конвейера.
		out = append(out, fmt.Sprintf(
			"PG_OUTSIDE_SELECTION_PKGS называет %s, но проб в этом пакете НЕТ (%d файлов). "+
				"Производитель зовёт пустоту: цель отказывает, а выглядит это поломкой конвейера, "+
				"а не снятым доказательством.",
			migratorApplyProofPkg, proofPkgTests))

	case proofPkgTests == 0:
		out = append(out, fmt.Sprintf(
			"точек наката %d, а доказательства наката нет вовсе: пакета %s в дереве нет либо он без проб. "+
				"Отказ тракта миграций означает «сервис не разворачивается», и сегодня его не проверяет "+
				"ни один процесс конвейера (задача #1637). Точки наката: %s",
			len(sorted), migratorApplyProofPkg, strings.Join(sorted, ", ")))

	case !named:
		// ГЛАВНАЯ находка задачи #1637: проба есть, гонять её некому.
		out = append(out, fmt.Sprintf(
			"пакет %s в дереве есть (%d файлов проб), но PG_OUTSIDE_SELECTION_PKGS корневого "+
				"Makefile его НЕ называет — у доказательства наката нет производителя. "+
				"Проба, которую никто не гоняет, зелена всегда, потому что не исполняется никогда: "+
				"это форма без содержания, а не сеть, которая поймала бы регрессию.",
			migratorApplyProofPkg, proofPkgTests))
	}

	return out
}
