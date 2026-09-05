// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// usecaselayout_test.go — гейт против ВТОРОЙ раскладки слоя бизнес-логики.
//
// Правило архитектуры знает ОДИН дом для use-case:
// `internal/apps/kaname/api/<resource>/`. Второй раскладки нет — ни как
// задокументированного отступления, ни как ссылки сервиса на своё исключение.
// Следствие расхождения сугубо практическое: тот, кто ищет use-case по правилу,
// в расходящемся сервисе не находит НИЧЕГО, а найдя случайно — не понимает,
// какая из двух раскладок теперь верна.
//
// Измерено по дереву на момент написания (7 сервисов; счёт — пакеты с .go под
// `internal/apps/kaname/api/`): geo 2, vpc 8, storage 4, nlb 7, registry 1,
// iam 22, compute 2. Каталог слоя вне `apps/` остался ровно один — iam, см.
// useCaseLayoutHandoff. Storage переехал 2026-07-28 (9785e9cc), compute —
// коммитом непосредственно перед этим.
//
// ЧЕГО ГЕЙТ НЕ ЗАПРЕЩАЕТ, и почему это существенно (иначе он ловит форму, а не
// существо, и его снимут при первом ложном срабатывании):
//
//   - `internal/apps/kaname/services/` у vpc (addressref / networkinternal /
//     nicinternal) — не-resource service'ы, которые сами про себя это говорят
//     («не относится ни к одному use-case в api/<resource>/»). Они лежат ВНУТРИ
//     `apps/`, то есть ровно в том слое, куда правило и помещает бизнес-логику.
//     Запрет здесь — про слой, оказавшийся ВНЕ своего единственного дома, а не
//     про слово «service» в имени каталога;
//   - доменный сервис в `internal/domain/` — ему в `api/` и не место;
//   - `internal/handler`, `internal/repo`, `internal/clients` — другие слои, у
//     них свои дома, и этот гейт про них ничего не утверждает.
package repohygiene

import (
	"github.com/PRO-Robotech/kacho/internal/servicelayout"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// useCaseLayerDirNames — имена, которыми называют каталог слоя бизнес-логики.
// Именно каталог: гейт смотрит на ПАКЕТ, а не на имена файлов внутри него, —
// `*_service.go` в домене или в адаптере под запрет не попадает и попадать не
// должен.
var useCaseLayerDirNames = []string{"service", "services", "usecase", "usecases"}

// useCaseLayoutHandoff — сервисы, где вторая раскладка ещё жива, потому что её
// разбор — отдельная работа, а НЕ потому что она признана допустимой.
//
// `services/iam/internal/service` — 26 файлов, и его собственный godoc называет
// себя «use-case-слой kaname (Clean Architecture, service-слой)», то есть
// расхождение здесь ровно того же класса, что было у storage и compute.
// Механической перекладкой оно не берётся: тот же пакет держит ПОРТЫ границы
// транзакции и outbox-эмиттеров (`Tx`, `TxBeginner`, `AuditOutboxEmitter`,
// `ResourceMirrorEmitter`, `RelationOutboxEmitter`), на которые завязаны 107
// файлов вне пакета — почти все `apps/kaname/api/*` и все pg-адаптеры. Разложить
// это по домам — решение о декомпозиции (какой use-case чей; что из общего
// уходит в `apps/kaname/shared`), а не переименование каталога, и делать его
// надо тогда, когда iam не правят пять потоков сразу.
//
// Исключение самоистекающее: как только каталога не станет,
// TestUseCaseLayoutExemptionsStillHaveSubject упадёт и потребует убрать запись
// отсюда. Пережить свой предмет оно не может — иначе в сервис можно будет тихо
// вернуть вторую раскладку, и никто не возразит.
var useCaseLayoutHandoff = []string{"services/iam/internal/service"}

// TestUseCaseLayerHasOneLayout — слой бизнес-логики не живёт вне
// `internal/apps/`.
//
// Что делать, если гейт сработал, — законные исходы, пятого нет:
//
//  1. это use-case ресурса -> переложить в
//     `internal/apps/kaname/api/<resource>/` (перекладка, не рефакторинг: имена
//     пакетов, имена тестов и содержимое файлов сохраняются);
//  2. это не-resource service слоя apps (внутренняя операция, sync-обвязка над
//     ресурсом) -> он живёт ВНУТРИ `apps/`, как `apps/kaname/services/` у vpc, и
//     обязан сказать это в своём godoc;
//  3. это доменный сервис (правила предметной области без портов и транспорта)
//     -> `internal/domain/`, и назвать пакет по тому, что в нём лежит;
//  4. это горизонтальный helper, нужный 2+ сервисам -> `pkg/`.
//
// Каталог с именем слоя, лежащий вне слоя, ни одному из четырёх исходов не
// отвечает.
func TestUseCaseLayerHasOneLayout(t *testing.T) {
	root := repoRoot(t)

	var hits []string
	for _, svc := range serviceDirs(t, root) {
		walkErr := filepath.WalkDir(filepath.Join(root, svc, "internal"),
			func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() || !slices.Contains(useCaseLayerDirNames, d.Name()) {
					return nil
				}
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					return relErr
				}
				rel = filepath.ToSlash(rel)
				// Внутри `apps/` слой на своём месте — см. преамбулу файла.
				if strings.Contains(rel, "/internal/apps/") {
					return nil
				}
				if slices.Contains(useCaseLayoutHandoff, rel) {
					return nil
				}
				hits = append(hits, rel+" (.go-файлов: "+strconv.Itoa(countGoFiles(path))+")")
				return nil
			})
		if walkErr != nil {
			t.Fatalf("обход %s: %v", svc, walkErr)
		}
	}

	if len(hits) > 0 {
		t.Errorf("найдена вторая раскладка слоя бизнес-логики — каталог слоя вне "+
			"`internal/apps/`, при том что правило архитектуры знает ровно один дом "+
			"`internal/apps/kaname/api/<resource>/`:\n  %s\n\n"+
			"Исходы: переложить в `apps/kaname/api/<resource>/` (если это use-case) / "+
			"внести внутрь `apps/` с честным godoc (если это не-resource service слоя "+
			"apps, как `apps/kaname/services/` у vpc) / перенести в `internal/domain/` "+
			"(если это доменный сервис) / вынести в `pkg/` (если это горизонтальный "+
			"helper). Каталог с именем слоя вне слоя — не исход.",
			strings.Join(hits, "\n  "))
	}

	// Перепись: «второй раскладки нет» значимо ровно настолько, насколько обход
	// вообще дошёл до сервисов. Ноль осмотренных дал бы тот же зелёный вердикт.
	t.Logf("перепись: сервисов осмотрено %d, каталогов-слоёв вне apps найдено %d, "+
		"освобождено записью %d", len(serviceDirs(t, root)), len(hits), len(useCaseLayoutHandoff))
}

// TestUseCaseLayoutExemptionsStillHaveSubject — исключение обязано умереть
// вместе со своим предметом.
//
// Освобождённый сервис, из которого каталог уже убрали, — тихая дыра в гейте:
// вторую раскладку туда можно будет вернуть, и никто не возразит. Поэтому
// пустое исключение здесь считается ошибкой, а не «просто больше не нужно».
func TestUseCaseLayoutExemptionsStillHaveSubject(t *testing.T) {
	root := repoRoot(t)

	for _, rel := range useCaseLayoutHandoff {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil || !info.IsDir() {
			t.Errorf("исключение %s больше не нужно: каталога нет. Удали запись из "+
				"useCaseLayoutHandoff — иначе сервис останется вне гейта и туда можно "+
				"будет незамеченно вернуть вторую раскладку.", rel)
			continue
		}
		if countGoFiles(filepath.Join(root, rel)) == 0 {
			t.Errorf("исключение %s пустое: .go-файлов в каталоге нет. Удали и каталог, "+
				"и запись из useCaseLayoutHandoff.", rel)
		}
	}

	// Перепись: пустой список исключений — законное состояние, но он обязан быть
	// отличим от списка, который забыли прочитать.
	t.Logf("перепись: записей исключений рассмотрено %d", len(useCaseLayoutHandoff))
}

// TestUseCaseLayoutPremiseHolds — запрет выше опирается на факт, который может
// перестать быть верным, и тогда сам запрет станет вредным.
//
// Запрет второй раскладки обоснован ТОЛЬКО тем, что первая — реальная: каждый
// сервис действительно держит слой бизнес-логики под
// `internal/apps/kaname/api/`. Пока это так, «второй дом» ни для чего не нужен.
// Если сервис появится (или окажется) без этого каталога, утверждение «дом
// один» перестанет быть описанием дерева, и запрет начнёт требовать переезда
// туда, куда в этом сервисе никто не переезжал.
//
// Без этой проверки запрет пережил бы своё обоснование молча. Гейт обязан
// падать на смене предпосылки, а не продолжать требовать своё. Замечание на
// будущее: до перекладки compute (коммит перед этим) эта проверка была КРАСНОЙ
// — ровно на compute, и именно так и должна была себя вести.
func TestUseCaseLayoutPremiseHolds(t *testing.T) {
	root := repoRoot(t)
	total := 0

	for _, svc := range serviceDirs(t, root) {
		// Сегмент берётся у службы, а не выписывается: одна строка на всех
		// молча обходила бы не тот каталог у той, что назвалась своим именем.
		api := filepath.Join(root, svc, "internal", "apps",
			servicelayout.UseCaseSegment(filepath.Base(svc)), "api")
		entries, err := os.ReadDir(api)
		if err != nil {
			t.Fatalf("%s: нет `internal/apps/<сегмент>/api/` (%v). Предпосылка запрета "+
				"второй раскладки (TestUseCaseLayerHasOneLayout) больше не выполняется: "+
				"этот сервис держит слой бизнес-логики где-то ещё. Реши, где он живёт, "+
				"и приведи к общему дому — либо пересмотри запрет.", svc, err)
		}
		pkgs := 0
		for _, e := range entries {
			if e.IsDir() && countGoFiles(filepath.Join(api, e.Name())) > 0 {
				pkgs++
			}
		}
		if pkgs == 0 {
			t.Fatalf("%s: `internal/apps/<сегмент>/api/` есть, но ни одного пакета с .go "+
				"в нём нет. Предпосылка запрета второй раскладки не выполняется — "+
				"use-case этого сервиса лежит не там, где утверждает правило.", svc)
		}
		total += pkgs
	}

	// Перепись: предпосылка проверена по КАЖДОМУ сервису, и это число названо —
	// иначе «предпосылка держится» неотличимо от «сервисов не нашлось».
	t.Logf("перепись: сервисов осмотрено %d, пакетов слоя в них суммарно %d",
		len(serviceDirs(t, root)), total)
}

// serviceDirs — каталоги сервисов (`services/<svc>`), у которых есть `internal`.
func serviceDirs(t *testing.T, root string) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatalf("чтение services/: %v", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, statErr := os.Stat(filepath.Join(root, "services", e.Name(), "internal")); statErr == nil && info.IsDir() {
			out = append(out, "services/"+e.Name())
		}
	}
	if len(out) == 0 {
		t.Fatal("под services/ не найдено ни одного сервиса с internal/ — гейт " +
			"смотрит не туда, чинить надо его, а не дерево")
	}
	return out
}

// countGoFiles — сколько .go-файлов лежит НЕПОСРЕДСТВЕННО в каталоге (пакет, а
// не поддерево).
func countGoFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			n++
		}
	}
	return n
}
