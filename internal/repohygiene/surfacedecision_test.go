// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// surfacedecision_test.go — РЕШЕНИЕ «у службы этой поверхности нет», записанное
// так, чтобы оно истекало само.
//
// # Предмет
//
// Два гейта дерева опираются на предпосылку «у КАЖДОЙ службы есть X» и сами
// говорят, что делать, когда она перестала быть верной:
//
//   - покрытие анализатором отбора списков
//     (TestCoverage_PremiseEveryServiceHasAListingSurface) — «either the service
//     really has no public list (record it), or its transport layout is not being
//     read»;
//   - единственная раскладка слоя бизнес-логики (TestUseCaseLayoutPremiseHolds) —
//     «Реши, где он живёт, и приведи к общему дому — либо пересмотри запрет».
//
// Служба без входящего RPC (notify: форма хоста без gRPC, NTF-1 Р1) не держит ни
// публичного списка, ни слоя use-case под `internal/apps/<сегмент>/api/` — ей
// нечего туда класть. Требовать от неё анализатор без предмета или пустой
// каталог-пустышку значило бы подменить решение формой. Поэтому решение
// записывается ЗДЕСЬ, а гейт его СУДИТ.
//
// # Что требуется от записи — и каждое закрывает свой исход
//
//  1. ПРИЧИНА — пустая решением не является;
//  2. НОМЕР ЗАДАЧИ — кто пересмотрит;
//  3. ДОКУМЕНТ — решение лежит прозой там, где его ищет человек, и документ
//     называет имя гейта, который его держит, — иначе он про что-то другое;
//  4. ПРЕДПОСЫЛКА — машинная, у каждого гейта своя: «List* в транспорте службы
//     ноль» и «каталога `internal/apps/` у службы нет».
//
// # Как запись истекает — и каждый исход роняет гейт
//
//  1. ПО УСПЕХУ: у службы появилась поверхность (List* или `internal/apps/`) —
//     записи больше нечего исключать, и гейт требует обычного исполнения;
//  2. ПО ИСЧЕЗНОВЕНИЮ ПРЕДМЕТА: службы нет в дереве.
//
// Освобождения «временно» здесь нет: запись о службе, у которой поверхность
// есть, — находка, а не запас.

// surfaceDecision — запись «у службы этой поверхности нет по решению».
type surfaceDecision struct {
	// Service — имя каталога службы (`services/<Service>`).
	Service string
	// Because — причина и предикат пересмотра. Пустая — находка.
	Because string
	// Issue — задача, где решение разобрано. Ноль — находка.
	Issue int
	// Record — документ дерева, где решение записано прозой. Обязан
	// существовать и называть имя гейта, который решение держит.
	Record string
}

// surfaceDecisionRecordFindings — суждение об ОБЩИХ полях записей одной
// ведомости: служба в дереве, причина, задача, документ, называющий гейт.
//
// Предпосылку каждого гейта судит сам гейт: она у них разная.
// Вынесено чистой функцией, чтобы доказательство способности упасть прогоняло
// ЕЁ, а не свою копию.
func surfaceDecisionRecordFindings(
	ledger []surfaceDecision,
	services []string,
	gate string,
	readRecord func(rel string) (string, error),
) []string {
	inTree := make(map[string]bool, len(services))
	for _, s := range services {
		inTree[s] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range ledger {
		if seen[e.Service] {
			out = append(out, fmt.Sprintf("ведомость решений гейта %s называет службу %q "+
				"дважды — какая запись действует, решал бы порядок", gate, e.Service))
			continue
		}
		seen[e.Service] = true
		if !inTree[e.Service] {
			out = append(out, fmt.Sprintf("ведомость решений гейта %s называет службу %q, "+
				"которой под services/ нет — записи больше нечего исключать, снимите её",
				gate, e.Service))
			continue
		}
		if strings.TrimSpace(e.Because) == "" {
			out = append(out, fmt.Sprintf("запись о службе %q (гейт %s) без причины — "+
				"«решили не делать» без основания следующий снимет как непонятное",
				e.Service, gate))
		}
		if e.Issue == 0 {
			out = append(out, fmt.Sprintf("запись о службе %q (гейт %s) не называет задачи "+
				"— обоснование объясняет, почему так сейчас, и молчит о том, кто это "+
				"пересмотрит", e.Service, gate))
		}
		if strings.TrimSpace(e.Record) == "" {
			out = append(out, fmt.Sprintf("запись о службе %q (гейт %s) не называет "+
				"документа — решение в коде ведомости прочтёт лишь тот, кто и так правит "+
				"гейт", e.Service, gate))
			continue
		}
		body, err := readRecord(e.Record)
		if err != nil {
			out = append(out, fmt.Sprintf("документ решения %s (служба %q, гейт %s) не "+
				"читается: %v — решения в дереве нет", e.Record, e.Service, gate, err))
			continue
		}
		if !strings.Contains(body, gate) {
			out = append(out, fmt.Sprintf("документ решения %s (служба %q) не называет "+
				"гейт %s — он про что-то другое, и решением для этого гейта не является",
				e.Record, e.Service, gate))
		}
	}
	sort.Strings(out)
	return out
}

// recordedServices — множество служб ведомости.
func recordedServices(ledger []surfaceDecision) map[string]bool {
	out := make(map[string]bool, len(ledger))
	for _, e := range ledger {
		out[e.Service] = true
	}
	return out
}

// readTreeRecord — чтение документа решения из дерева.
func readTreeRecord(root string) func(rel string) (string, error) {
	return func(rel string) (string, error) {
		body, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- из ведомости этого дерева
		if err != nil {
			return "", err
		}
		return string(body), nil
	}
}

// noListingSurface — службы без публичного списка ПО РЕШЕНИЮ. Держит
// TestCoverage_PremiseEveryServiceHasAListingSurface.
//
// Пуст: здесь стояла запись notify (kacho#2915, «ни один процесс каталога не
// служит List*»). Она истекла сама: развёртывание notify-api (kacho#2924) служит
// `NoticeService.List`, `ListByAccount` и `InternalNoticeService.List`, и гейт
// требует анализатор отбора списков службы, его цель Makefile и шаг конвейера.
var noListingSurface = []surfaceDecision{}

// noUseCaseLayer — службы без слоя бизнес-логики под
// `internal/apps/<сегмент>/api/` ПО РЕШЕНИЮ. Держит TestUseCaseLayoutPremiseHolds.
//
// Пуст: здесь стояла запись notify (kacho#2915, «у notify нет ресурса с
// use-case»). Она истекла сама, как и было записано: развёртывание notify-api
// (kacho#2924) принесло ресурс «извещение» и слой
// `internal/apps/kacho/api/{notice,publicnotice}`, и предпосылка судится как у
// всех служб.
var noUseCaseLayer = []surfaceDecision{}
