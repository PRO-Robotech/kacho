// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// usecaselayout_test.go — гейт против ВТОРОЙ раскладки слоя бизнес-логики.
//
// Правило архитектуры знает ОДИН дом для use-case:
// `internal/apps/<сегмент>/api/<resource>/`, где сегмент объявлен ОДНАЖДЫ —
// `internal/servicelayout` (сегодня у всех шести служб дерева это `kacho`).
// Написан он здесь через подстановку намеренно: литерал в прозе разошёлся бы с
// объявлением молча, а разойтись ему есть на чём — служба, назвавшая свои
// каталоги собственным именем, уже была. Второй раскладки нет — ни как
// задокументированного отступления, ни как ссылки сервиса на своё исключение.
// Следствие расхождения сугубо практическое: тот, кто ищет use-case по правилу,
// в расходящемся сервисе не находит НИЧЕГО, а найдя случайно — не понимает,
// какая из двух раскладок теперь верна.
//
// Измерено по дереву на момент написания (7 сервисов; счёт — пакеты с .go под
// `internal/apps/<сегмент>/api/`): geo 2, vpc 8, storage 4, nlb 7, registry 1,
// служба доступа 22, compute 2. Каталог слоя вне `apps/` остался ровно один — у
// службы доступа, и он уехал вместе с ней; служб в дереве теперь шесть.
// Storage переехал 2026-07-28 (9785e9cc), compute — коммитом непосредственно
// перед этим.
//
// ПЕРЕЧЕНЬ ОСВОБОЖДЁННЫХ ПУСТ, и это цель, а не поломка. Единственная запись
// (`services/iam/internal/service`) истекла вместе со своим предметом: служба
// доступа вынесена отдельным продуктом, каталога в дереве нет. Истечение
// сработало ровно как задумано — `TestUseCaseLayoutExemptionsStillHaveSubject`
// назвал запись, у которой больше нечего освобождать, и потребовал её снять.
// Пустой перечень означает, что вторая раскладка сегодня не освобождена НИ У
// ОДНОГО сервиса; перепись обеих проб печатает его длину, поэтому «ноль
// освобождённых» отличимо от «перечень не прочитан».
//
// ЧЕГО ГЕЙТ НЕ ЗАПРЕЩАЕТ, и почему это существенно (иначе он ловит форму, а не
// существо, и его снимут при первом ложном срабатывании):
//
//   - `internal/apps/<сегмент>/services/` у vpc (addressref / networkinternal /
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
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/servicelayout"
)

// useCaseLayerDirNames — имена, которыми называют каталог слоя бизнес-логики.
// Именно каталог: гейт смотрит на ПАКЕТ, а не на имена файлов внутри него, —
// `*_service.go` в домене или в адаптере под запрет не попадает и попадать не
// должен.
var useCaseLayerDirNames = []string{"service", "services", "usecase", "usecases"}

// useCaseLayoutHandoff — сервисы, где вторая раскладка ещё жива, потому что её
// разбор — отдельная работа, а НЕ потому что она признана допустимой.
//
// Сегодня перечень ПУСТ. Единственная его запись — `services/iam/internal/service`
// — снята вместе со своим предметом: служба доступа вынесена отдельным
// продуктом, и каталога в дереве нет. Запись держалась не снисхождением, а
// объёмом работы (26 файлов, из них порты границы транзакции и outbox-эмиттеров,
// на которые ссылались 107 файлов вне пакета), и вместе со службой этот объём
// ушёл туда же.
//
// Пустой перечень — законное и желаемое состояние, а не повод завести запись
// «на всякий случай»: запас здесь есть слепая зона, выданная вперёд. Всякая
// новая запись обязана нести предмет, и он проверяется
// TestUseCaseLayoutExemptionsStillHaveSubject: запись, которой больше нечего
// освобождать, роняет прогон.
var useCaseLayoutHandoff []string

// TestUseCaseLayerHasOneLayout — слой бизнес-логики не живёт вне
// `internal/apps/`.
//
// Что делать, если гейт сработал, — законные исходы, пятого нет:
//
//  1. это use-case ресурса -> переложить в
//     `internal/apps/<сегмент>/api/<resource>/` (перекладка, не рефакторинг: имена
//     пакетов, имена тестов и содержимое файлов сохраняются);
//  2. это не-resource service слоя apps (внутренняя операция, sync-обвязка над
//     ресурсом) -> он живёт ВНУТРИ `apps/`, как `apps/<сегмент>/services/` у vpc, и
//     обязан сказать это в своём godoc;
//  3. это доменный сервис (правила предметной области без портов и транспорта)
//     -> `internal/domain/`, и назвать пакет по тому, что в нём лежит;
//  4. это горизонтальный helper, нужный 2+ сервисам -> `pkg/`.
//
// Каталог с именем слоя, лежащий вне слоя, ни одному из четырёх исходов не
// отвечает.
func TestUseCaseLayerHasOneLayout(t *testing.T) {
	t.Parallel()
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
			"`internal/apps/<сегмент>/api/<resource>/`:\n  %s\n\n"+
			"Исходы: переложить в `apps/<сегмент>/api/<resource>/` (если это use-case) / "+
			"внести внутрь `apps/` с честным godoc (если это не-resource service слоя "+
			"apps, как `apps/<сегмент>/services/` у vpc) / перенести в `internal/domain/` "+
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
	t.Parallel()
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
// `internal/apps/<сегмент>/api/`. Пока это так, «второй дом» ни для чего не нужен.
// Если сервис появится (или окажется) без этого каталога, утверждение «дом
// один» перестанет быть описанием дерева, и запрет начнёт требовать переезда
// туда, куда в этом сервисе никто не переезжал.
//
// Без этой проверки запрет пережил бы своё обоснование молча. Гейт обязан
// падать на смене предпосылки, а не продолжать требовать своё. Замечание на
// будущее: до перекладки compute (коммит перед этим) эта проверка была КРАСНОЙ
// — ровно на compute, и именно так и должна была себя вести.
func TestUseCaseLayoutPremiseHolds(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	svcs := serviceDirs(t, root)

	shapes := make(map[string]useCaseLayerShape, len(svcs))
	for _, svc := range svcs {
		shapes[svc] = readUseCaseLayerShape(root, svc)
	}

	names := make([]string, 0, len(svcs))
	for _, svc := range svcs {
		names = append(names, filepath.Base(svc))
	}
	const gate = "TestUseCaseLayoutPremiseHolds"
	for _, f := range surfaceDecisionRecordFindings(noUseCaseLayer, names, gate, readTreeRecord(root)) {
		t.Error(f)
	}
	findings, total := useCasePremiseFindings(svcs, shapes, noUseCaseLayer)
	for _, f := range findings {
		t.Error(f)
	}

	// Перепись: предпосылка проверена по КАЖДОМУ сервису, и это число названо —
	// иначе «предпосылка держится» неотличимо от «сервисов не нашлось». Записи
	// решения названы отдельным числом: «освобождено решением» не сливается с
	// «осмотрено».
	t.Logf("перепись: сервисов осмотрено %d, пакетов слоя в них суммарно %d, "+
		"служб без слоя по решению (noUseCaseLayer) %d",
		len(svcs), total, len(noUseCaseLayer))
}

// useCaseLayerShape — что у службы есть из дома слоя бизнес-логики.
type useCaseLayerShape struct {
	apps bool // каталог `internal/apps/` существует
	api  bool // каталог `internal/apps/<сегмент>/api/` существует
	pkgs int  // пакетов с .go непосредственно под `api/`
}

// readUseCaseLayerShape — снять форму с дерева. Ничего не решает.
func readUseCaseLayerShape(root, svc string) useCaseLayerShape {
	var shape useCaseLayerShape
	if info, err := os.Stat(filepath.Join(root, svc, "internal", "apps")); err == nil && info.IsDir() {
		shape.apps = true
	}
	// Сегмент берётся у службы, а не выписывается: одна строка на всех
	// молча обходила бы не тот каталог у той, что назвалась своим именем.
	api := filepath.Join(root, svc, "internal", "apps",
		servicelayout.UseCaseSegment(filepath.Base(svc)), "api")
	entries, err := os.ReadDir(api)
	if err != nil {
		return shape
	}
	shape.api = true
	for _, e := range entries {
		if e.IsDir() && countGoFiles(filepath.Join(api, e.Name())) > 0 {
			shape.pkgs++
		}
	}
	return shape
}

// useCasePremiseFindings — РЕШЕНИЕ о предпосылке, чистой функцией: доказательство
// способности упасть (surfacedecision_injection_test.go) прогоняет ЕЁ.
//
// Служба, записанная в noUseCaseLayer, держит предпосылку иначе: у неё НЕТ
// `internal/apps/` вовсе. Появился каталог — запись пережила предмет, и это
// находка, а не молчаливое продолжение освобождения. Запрет второй раскладки
// (TestUseCaseLayerHasOneLayout) записанную службу по-прежнему обходит: решение
// снимает требование дома, а не запрет второго.
func useCasePremiseFindings(svcs []string, shapes map[string]useCaseLayerShape,
	ledger []surfaceDecision,
) (findings []string, total int) {
	recorded := recordedServices(ledger)
	for _, svc := range svcs {
		shape := shapes[svc]
		if recorded[filepath.Base(svc)] {
			if shape.apps {
				findings = append(findings, svc+": запись noUseCaseLayer пережила свой предмет — "+
					"у службы появился `internal/apps/`. Снимите запись: дальше предпосылка "+
					"судится как у всех служб")
			}
			continue
		}
		switch {
		case !shape.api:
			findings = append(findings, svc+": нет `internal/apps/<сегмент>/api/`. Предпосылка "+
				"запрета второй раскладки (TestUseCaseLayerHasOneLayout) больше не выполняется: "+
				"этот сервис держит слой бизнес-логики где-то ещё. Реши, где он живёт, и приведи "+
				"к общему дому — либо, если у службы нет входящего RPC и класть некуда, запиши "+
				"решение в noUseCaseLayer")
		case shape.pkgs == 0:
			findings = append(findings, svc+": `internal/apps/<сегмент>/api/` есть, но ни одного "+
				"пакета с .go в нём нет. Предпосылка запрета второй раскладки не выполняется — "+
				"use-case этого сервиса лежит не там, где утверждает правило.")
		}
		total += shape.pkgs
	}
	sort.Strings(findings)
	return findings, total
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
