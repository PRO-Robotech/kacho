// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// fgaoutboxrowowner_injection_test.go — доказательство, что гейт формы строки
// `kaname.fga_outbox` СПОСОБЕН упасть, и что он падает на существе, а не на форме.
//
// Инъекция в ОБЕ стороны на СИНТЕТИЧЕСКОМ дереве: репозиторием оно не является, индекса
// у него нет, и состав берётся у `newSyntheticTree` — там файловая система и есть
// единственный авторитет. На настоящем дереве гейт читает индекс, и это не деталь: под
// корнем лежат каталоги, которых в репозитории нет, и обход диска сделал бы вердикт
// свойством чужой рабочей копии.
//
// Инъекция в ОБЕ стороны:
//
//   - вернуть нарушение (файл вне владельца, вставляющий строку и работающий с
//     кортежем как с типом) → гейт находит его и НАЗЫВАЕТ координату;
//   - положить рядом ЗАКОННОГО близнеца той же формы (вставка с константным
//     отношением; чтение той же таблицы; вставка внутри самого владельца) → гейт
//     молчит.
//
// Без второй половины гейт ловил бы «упоминание таблицы», а не «второго рендерера
// строки», и первый же ложный срабат его бы отключил.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeInjected — кладёт .go-файл по относительному пути внутри временного дерева.
func writeInjected(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// ownerFile — минимальный «владелец», без которого проверка предпосылки гейта
// справедливо роняет прогон (ей нечего защищать).
const ownerFile = `package fga_outbox

func emit() string {
	return ` + "`INSERT INTO kaname.fga_outbox (event_type, payload) VALUES ($1, $2)`" + `
}
`

func TestFGAOutboxRowOwnerGate_CatchesASecondRenderer(t *testing.T) {
	root := t.TempDir()
	writeInjected(t, root, filepath.Join(fgaOutboxRowOwnerDir, "emitter.go"), ownerFile)
	writeInjected(t, root, filepath.Join("services", "iam", "internal", "repo", "kaname", "pg", "rogue.go"),
		`package pg

import "github.com/PRO-Robotech/kacho-iam/internal/clients"

func emitRogue(tuples []clients.RelationTuple) string {
	_ = tuples
	return `+"`INSERT INTO kaname.fga_outbox (event_type, payload) VALUES ($1, $2::jsonb)`"+`
}
`)

	sc := scanFGAOutboxRenderers(t, newSyntheticTree(t, root))
	if len(sc.offenders) != 1 {
		t.Fatalf("возвращённое нарушение обязано быть найдено ровно одно, найдено %d: %v",
			len(sc.offenders), sc.offenders)
	}
	if !strings.Contains(sc.offenders[0], "rogue.go") {
		t.Fatalf("находка обязана НАЗЫВАТЬ координату виновника, получено %q", sc.offenders[0])
	}
}

func TestFGAOutboxRowOwnerGate_SilentOnLegitimateTwins(t *testing.T) {
	root := t.TempDir()
	writeInjected(t, root, filepath.Join(fgaOutboxRowOwnerDir, "emitter.go"), ownerFile)

	// Близнец 1: посев с КОНСТАНТНЫМ отношением — набора нет, расщеплять нечего.
	writeInjected(t, root, filepath.Join("services", "iam", "internal", "apps", "kaname", "seed", "seed.go"),
		`package seed

func seedOne() string {
	return `+"`INSERT INTO kaname.fga_outbox (event_type, payload) VALUES ('fga.tuple.write', jsonb_build_object('relation','system_admin'))`"+`
}
`)
	// Близнец 2: ЧТЕНИЕ той же таблицы кодом, который работает с кортежами (сканер
	// очереди, диагностика). Законно откуда угодно.
	writeInjected(t, root, filepath.Join("services", "iam", "internal", "diag", "scan.go"),
		`package diag

import "github.com/PRO-Robotech/kacho-iam/internal/clients"

func pending() (string, []clients.RelationTuple) {
	return `+"`SELECT count(*) FROM kaname.fga_outbox WHERE sent_at IS NULL`"+`, nil
}
`)

	sc := scanFGAOutboxRenderers(t, newSyntheticTree(t, root))
	if len(sc.offenders) != 0 {
		t.Fatalf("гейт обязан молчать на законных близнецах, получено: %v", sc.offenders)
	}
	if sc.ownerInserts == 0 {
		t.Fatalf("предпосылка гейта не воспроизведена: владелец обязан вставлять")
	}
	if len(sc.constantRelationInserts) != 1 {
		t.Fatalf("посев с константным отношением обязан попасть в перепись, а не в находки: %v",
			sc.constantRelationInserts)
	}
}
