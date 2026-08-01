// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// gosecdialect_test.go — гейт против подавления, которое ничего не подавляет.
//
// В этом дереве ДВА инструмента, и только один из них вообще читает подавления
// gosec:
//
//   - отдельный `gosec` (security-scan.yml, пин v2.28.0) читает ТОЛЬКО свою
//     форму `#nosec [ПРАВИЛО] -- причина`. Форму `//nolint:` он не разбирает
//     вовсе — это диалект golangci-lint, а не его;
//   - `golangci-lint` читает `//nolint:`, но в .golangci.yml стоит
//     `default: none`, и gosec в enable-списке НЕТ. Подавлять нечего.
//
// Отсюда: `//nolint:gosec` в этом репозитории инертен ПО ПОСТРОЕНИЮ — ни один
// инструмент его не прочитает. При этом читается он человеком как «здесь
// посмотрели и решили, что нормально», хотя не сделано ни первого, ни второго.
//
// Измерено на единственной находке, которая реально гейтит сборку
// (G101, services/iam/internal/handler/iamhooks/token_hook_handler.go):
//
//	без директивы            -> level=error: 1
//	с //nolint:gosec         -> level=error: 1   ← вердикт не изменился
//	с // #nosec G101         -> level=error: 0
//
// Так накопилось 28 штук (5 в vpc/nlb + 23 в iam/gateway/repohygiene), и 23 из
// них подавляли findings, которых НЕТ вовсе: с принудительно отключённой
// обработкой подавлений (`-nosec=true`) на том наборе файлов, который скан
// реально читает, по этим строкам ноль находок. Накопилось именно потому, что
// возразить было некому.
//
// nolintlint для этого класса НЕ годится: измерено, что он молчит и про
// неиспользованный `//nolint:gosec`, и про имя несуществующего линтера. Гейт,
// структурно неспособный увидеть свой предмет, — ровно тот класс, который здесь
// искореняют.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// nolintGosecRe — директива golangci-lint, перечисляющая gosec среди линтеров.
//
// Форма директивы жёсткая: между `//` и `nolint` пробела быть НЕ должно, иначе
// golangci-lint её не признаёт. Поэтому ищется именно слитная форма — упоминание
// в прозе (с пробелом, в бэктиках) под гейт не попадает.
var nolintGosecRe = regexp.MustCompile(`//nolint:[a-zA-Z0-9_,-]*\bgosec\b`)

// selfFile — файл, определяющий запрет, обязан называть то, что запрещает
// (в прозе и в тексте ошибки). Единственное исключение по построению.
const selfFile = "internal/repohygiene/gosecdialect_test.go"

// pendingHandoff — файлы, где директивы ещё живут, потому что их правит другой
// поток работы, а не потому что они признаны допустимыми.
//
// Разбор по ним такой же, как и по остальным 23: findings НЕТ вовсе. Замерено
// на пине CI (v2.28.0) во всех трёх режимах — обычном, с `-nosec=true` и с
// `-nosec=true -tests`: по этим строкам ноль находок. Правила SQL-инъекции
// (G201/G202) не матчат здешнюю форму — приёмник pgx, а `fmt.Sprintf` стоит
// инлайном в аргументе вызова.
//
// ВАЖНО: молчание сканера на этой форме — НЕ доказательство безопасности.
// Происхождение операнда, попадающего в текст запроса, надо устанавливать
// чтением, а не вердиктом сканера.
//
// Исключение самоистекающее: как только директив в файле не останется,
// TestPendingHandoffExemptionsStillHaveSubject упадёт и потребует удалить
// запись отсюда. Оно не может пережить свой предмет.
var pendingHandoff = []string{
	"services/compute/internal/clients/fga_reconcile_adapter.go",
}

// TestNoInertGosecSuppressions — ни один .go-файл не несёт `//nolint:gosec`.
//
// Что делать, если гейт сработал, — ровно три исхода, четвёртого нет:
//
//  1. находка реальна и это ложное срабатывание -> переписать в рабочую форму
//     `// #nosec ПРАВИЛО -- причина` (тогда подавление действительно применится);
//  2. находка реальна и это дефект -> починить по существу, строгим TDD;
//  3. находки нет -> удалить директиву. Грамотно записанное подавление пустоты
//     остаётся подавлением пустоты.
//
// Проверить, есть ли предмет вообще:
//
//	gosec -exclude-dir=pkg/api -nosec=true -fmt sarif -out g.sarif ./...
//
// `-nosec=true` отключает обработку подавлений, поэтому вопрос диалекта в этом
// прогоне не стоит: строка либо даёт находку, либо нет.
func TestNoInertGosecSuppressions(t *testing.T) {
	root := repoRoot(t)

	var hits []string
	walkGoFiles(t, root, func(rel string, body []byte) {
		if rel == selfFile || slices.Contains(pendingHandoff, rel) {
			return
		}
		for i, line := range strings.Split(string(body), "\n") {
			if nolintGosecRe.MatchString(line) {
				hits = append(hits, rel+":"+strconv.Itoa(i+1)+" — "+strings.TrimSpace(line))
			}
		}
	})

	if len(hits) > 0 {
		t.Errorf("найдено %d подавлений gosec в диалекте, который в этом репозитории "+
			"не читает НИКТО (gosec понимает только `#nosec`, а golangci-lint не гоняет "+
			"gosec вовсе). Такая строка ничего не подавляет:\n  %s\n\n"+
			"Исходы: переписать в `// #nosec ПРАВИЛО -- причина` (если находка реальна и "+
			"ложна) / починить по существу (если дефект настоящий) / удалить (если находки "+
			"нет). Проверить наличие предмета: gosec -nosec=true …",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// TestPendingHandoffExemptionsStillHaveSubject — исключение обязано умереть
// вместе со своим предметом.
//
// Освобождённый файл, из которого директивы уже убрали, — это тихая дыра в
// гейте: новую директиву в него можно будет внести, и никто не возразит.
// Поэтому пустое исключение здесь считается ошибкой, а не «просто больше не
// нужно».
func TestPendingHandoffExemptionsStillHaveSubject(t *testing.T) {
	root := repoRoot(t)

	for _, rel := range pendingHandoff {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("исключение %s: файл не читается (%v) — запись устарела, удали её "+
				"из pendingHandoff", rel, err)
			continue
		}
		if !nolintGosecRe.Match(body) {
			t.Errorf("исключение %s больше не нужно: директив в файле нет. Удали запись "+
				"из pendingHandoff — иначе файл останется вне гейта и туда можно будет "+
				"внести новую директиву незамеченной.", rel)
		}
	}
}

// TestGosecDialectBanPremiseHolds — гейт выше опирается на факт, который может
// перестать быть верным, и тогда сам гейт станет вредным.
//
// Запрет `//nolint:gosec` обоснован ТОЛЬКО тем, что golangci-lint не гоняет
// gosec: пока это так, директива не может ни на что повлиять. Если gosec когда-
// нибудь появится в enable-списке .golangci.yml, форма `//nolint:gosec` станет
// осмысленной, и запрет придётся снимать — иначе он начнёт запрещать рабочее
// подавление.
//
// Без этой проверки запрет пережил бы своё обоснование молча. Гейт обязан падать
// на смене предпосылки, а не продолжать требовать своё.
func TestGosecDialectBanPremiseHolds(t *testing.T) {
	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, ".golangci.yml"))
	if err != nil {
		t.Fatalf("не прочитан .golangci.yml: %v", err)
	}

	// Ищем gosec как элемент списка линтеров (`- gosec`), а не в комментариях:
	// весь смысл — включён ли он, а не упомянут ли.
	for i, line := range strings.Split(string(raw), "\n") {
		code := line
		if idx := strings.Index(code, "#"); idx >= 0 {
			code = code[:idx]
		}
		if strings.TrimSpace(code) == "- gosec" {
			t.Fatalf(".golangci.yml:%d включает gosec в golangci-lint. Предпосылка запрета "+
				"`//nolint:gosec` (TestNoInertGosecSuppressions) больше не выполняется: "+
				"теперь эта форма ЧТО-ТО подавляет. Пересмотри запрет — либо сними его, "+
				"либо реши, какой из двух диалектов в этом дереве канонический.", i+1)
		}
	}
}

// walkGoFiles — обход .go-файлов РЕПОЗИТОРИЯ (без vendor/node_modules и
// сгенерённых стабов pkg/api, которые скан тоже исключает).
//
// Состав берётся из индекса git (`newTrackedTree`, см. trackedtree_test.go), а
// не с диска. Обход диска от корня читал и рабочие копии дерева под
// `.claude/worktrees/`: гейт сообщал находки о файлах, которых в репозитории
// нет, и его вердикт становился свойством чужого рабочего каталога, а не
// коммита. Померено на 8c2eba3e: чистая копия — зелено; она же плюс ОДИН
// git-игнорируемый каталог с деревом внутри — 10 находок этого гейта, и каждая
// координата начинается с `.claude/worktrees/`.
func walkGoFiles(t *testing.T, root string, fn func(rel string, body []byte)) {
	t.Helper()
	tt := newTrackedTree(t, root)
	rels := make([]string, 0, tt.count())
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels) // порядок находок не должен зависеть от обхода карты
	for _, rel := range rels {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		if strings.HasPrefix(rel, "pkg/api/") ||
			strings.HasPrefix(rel, "vendor/") ||
			strings.HasPrefix(rel, "node_modules/") ||
			strings.HasPrefix(rel, "ui-future/") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		fn(rel, body)
	}
}
