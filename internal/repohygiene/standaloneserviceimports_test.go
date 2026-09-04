// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// standaloneserviceimports_test.go — сервис, которому предстоит стать отдельным
// модулем, импортирует из этого модуля ТОЛЬКО общий фундамент `pkg/` и себя.
//
// # Предмет
//
// Решением владельца 2026-09-04 `services/iam` выносится отдельным репозиторием
// и отдельным продуктом, устанавливаемым в чужом облаке. Фундамент `pkg/` и
// контракты `proto/` остаются в `kacho`; вынесенный сервис ссылается на них как
// на внешний versioned-модуль. То есть после разреза законный вход у него ровно
// один — публичная поверхность `kacho/pkg/...` (стабы `pkg/api/...` — её часть),
// плюс собственное поддерево.
//
// # Оснований у находки ДВА, и они разной силы — это надо знать, читая отказ
//
// Смешивать их нельзя: правило, объявляющее одно основание для обоих случаев,
// наполовину ложно, и следующий читатель снимет гейт как непонятный.
//
//	раздел модуля     что будет после разреза               основание
//	internal/**       СБОРКА ОТКАЗЫВАЕТ                     правило языка Go
//	всё прочее        соберётся, но зависимость не та       решение владельца
//
// Первое доказано опытом, а не цитатой правила. Отдельный модуль
// `example.com/xmod` с `replace` на это дерево; меняется РОВНО один факт — путь
// импорта:
//
//	import _ ".../internal/pgtest"      → use of internal package … not allowed   rc=1
//	import _ ".../internal/treecorpus"  → use of internal package … not allowed   rc=1
//	import _ ".../pkg/ids"              → (пусто)                                 rc=0
//
// Второе основание проверено ТЕМ ЖЕ опытом и в обратную сторону — и здесь
// посылка «корневой `tools/` тоже перестанет разрешаться» ОПРОВЕРГНУТА:
//
//	import _ ".../tools/listfiltergate"   → (пусто)   rc=0
//	import _ ".../tools/authzformbench"   → (пусто)   rc=0
//	import _ ".../tools/modulemanifests"  → (пусто)   rc=0
//
// `tools/` не элемент `internal`, поэтому язык его не закрывает: внешний модуль
// импортирует такой путь без единой жалобы. Основание для него другое и слабее
// компилятора, но названо: `tools/` — оснастка сборки САМОГО монорепо (там же
// `tools.go`, пинящий плагины генерации под признаком `tools`). Вынесенный
// продукт, тянущий чужую оснастку сборки, получает внешнюю зависимость, которую
// нельзя ни объявить, ни версионировать честно: она не про его предмет.
//
// # Почему предикат ПОЛОЖИТЕЛЬНЫЙ, а не перечень запрещённого
//
// Перечень запрещённого закрывает то, что уже случилось, и молчит о том, чего
// ещё нет: первый же импорт `services/vpc/...` или `gateway/...` прошёл бы мимо
// него, а это ровно та же беда — зависимость, которой после разреза не будет.
// Поэтому гейт объявляет РАЗРЕШЁННОЕ (`pkg/` и собственное поддерево), а
// находкой считает всё остальное из этого модуля. Замер на ревизии заведения:
// разделов модуля, которые iam импортирует, четыре — `services/iam` 3065 рёбер,
// `pkg` 1074, `internal` 216, `tools` 4.
//
// # Чего гейт НЕ утверждает
//
// Он судит ПРЯМЫЕ импорты, а не транзитивные, и это осознанно. Транзитивная
// цепочка `iam → kacho/pkg/migratorrun → kacho/internal/dropguard` законна by
// construction: ребро внутрь `internal/` проведено кодом, который САМ лежит в
// `kacho`, и правило языка его не касается. Требовать от `pkg/` не иметь
// внутренних зависимостей — другой предмет и другой владелец.
//
// Он не утверждает и того, что список вынесенных сервисов полон: список —
// решение владельца, а не свойство дерева. Он лишь требует, чтобы каждое
// объявленное имя резолвилось к каталогу с Go-кодом, иначе гейт молча судил бы
// пустоту.
package repohygiene

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// standaloneBoundServices — сервисы, которым решением владельца предстоит стать
// самостоятельным продуктом в отдельном репозитории.
//
// Список выписан, а не выведен: «станет отдельным продуктом» — решение владельца,
// у дерева такого признака нет. Зато его ЭЛЕМЕНТЫ проверяются деревом: имя, под
// которым нет Go-кода, роняет гейт, иначе снятый или переименованный сервис
// оставил бы здесь запись, которой нечего судить.
var standaloneBoundServices = []string{"services/iam"}

// sharedFoundationPrefixes — то, что остаётся в `kacho` и приезжает к
// вынесенному сервису ВНЕШНЕЙ зависимостью. Пути даны относительно модуля.
//
// `pkg/api/...` отдельной записи не требует: это подкаталог `pkg/`.
var sharedFoundationPrefixes = []string{"pkg/"}

// standaloneImportFinding — один импорт, которого после разреза не будет.
//
// Координата и основание едут вместе: чинить находку читатель идёт по
// координате, а РЕШАЕТ он по основанию — «сборка откажет» и «зависимость не та»
// требуют разного действия.
type standaloneImportFinding struct {
	Service string
	File    string // rel-путь от корня дерева
	Line    int
	Import  string
	Ground  string
}

func (f standaloneImportFinding) String() string {
	return fmt.Sprintf("%s:%d — %s (%s)", f.File, f.Line, f.Import, f.Ground)
}

// standaloneImportCensus — объём осмотренного.
//
// Печатается всегда и по осям: «ноль находок» обязано быть отличимо от «ноль
// прочитанного», а «ноль импортов своего модуля» означает сломанный разбор, а не
// чистый сервис.
type standaloneImportCensus struct {
	Services      int
	Files         int
	Imports       int
	OwnModule     int
	Allowed       int
	FindingFiles  int
	FindingEdges  int
	LanguageBound int // из них те, что откажет компилятор
}

func (c standaloneImportCensus) String() string {
	return fmt.Sprintf("сервисов %d; файлов Go %d; импортов %d, из них своего модуля %d "+
		"(в разрешённом фундаменте и своём поддереве %d); находок — файлов %d, рёбер %d "+
		"(из них сборка откажет на %d)",
		c.Services, c.Files, c.Imports, c.OwnModule, c.Allowed,
		c.FindingFiles, c.FindingEdges, c.LanguageBound)
}

// standaloneGroundLanguage / standaloneGroundForeign — основания находки.
const (
	standaloneGroundLanguage = "правило языка: внешний модуль такой путь не импортирует"
	standaloneGroundForeign  = "вне общего фундамента: после разреза этого пути у сервиса нет"
)

// standaloneImportGround — основание, по которому путь назван находкой.
//
// Различие несёт исход, поэтому вычисляется здесь один раз, а не пересказывается
// в каждом сообщении: элемент пути `internal` закрывает компилятор, всё
// остальное — решение о составе внешней зависимости.
func standaloneImportGround(rel string) string {
	for _, seg := range strings.Split(rel, "/") {
		if seg == "internal" {
			return standaloneGroundLanguage
		}
	}
	return standaloneGroundForeign
}

// scanStandaloneServiceImports разбирает импорты файлов сервиса и возвращает те,
// которых после разреза не будет.
//
// Разбор, а не поиск по образцу: путь пакета встречается и в комментариях, и в
// строковых литералах, и предикат по слову считал бы их — в этом же дереве такие
// литералы есть у гейтов, судящих раскладку.
func scanStandaloneServiceImports(
	service string,
	files []string, // rel-пути от корня дерева
	read func(rel string) ([]byte, error),
	module string,
) ([]standaloneImportFinding, standaloneImportCensus, error) {
	census := standaloneImportCensus{Services: 1}
	allowed := make([]string, 0, len(sharedFoundationPrefixes)+1)
	for _, p := range sharedFoundationPrefixes {
		allowed = append(allowed, module+"/"+p)
	}
	allowed = append(allowed, module+"/"+service+"/")

	var findings []standaloneImportFinding
	seenFiles := map[string]bool{}
	fset := token.NewFileSet()
	for _, rel := range files {
		body, err := read(rel)
		if err != nil {
			return nil, census, fmt.Errorf("%s: %w — непрочитанный файл обязан быть "+
				"отказом, а не молчаливым нулём", rel, err)
		}
		census.Files++
		f, err := parser.ParseFile(fset, rel, body, parser.ImportsOnly)
		if err != nil {
			return nil, census, fmt.Errorf("%s: разбор импортов: %w", rel, err)
		}
		for _, spec := range f.Imports {
			census.Imports++
			path := strings.Trim(spec.Path.Value, `"`)
			if !strings.HasPrefix(path, module+"/") {
				continue // чужой модуль и stdlib — не предмет
			}
			census.OwnModule++
			ok := false
			for _, a := range allowed {
				if strings.HasPrefix(path, a) {
					ok = true
					break
				}
			}
			if ok {
				census.Allowed++
				continue
			}
			inner := strings.TrimPrefix(path, module+"/")
			ground := standaloneImportGround(inner)
			if ground == standaloneGroundLanguage {
				census.LanguageBound++
			}
			census.FindingEdges++
			seenFiles[rel] = true
			findings = append(findings, standaloneImportFinding{
				Service: service,
				File:    rel,
				Line:    fset.Position(spec.Path.Pos()).Line,
				Import:  path,
				Ground:  ground,
			})
		}
	}
	census.FindingFiles = len(seenFiles)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, census, nil
}

// ── гейт на дереве ───────────────────────────────────────────────────────────

func TestStandaloneServiceImportsOnlyTheSharedFoundation(t *testing.T) {
	root := repoRoot(t)
	module := moduleImportPath(t, root)

	if len(standaloneBoundServices) == 0 {
		t.Fatalf("перечень вынесенных сервисов пуст — гейту нечего судить. Либо решение "+
			"о выносе отозвано (тогда гейт снимается ВМЕСТЕ с предметом), либо запись "+
			"потеряна: молчание на пустом перечне неотличимо от чистого дерева.\n"+
			"Разрешённый фундамент: %v", sharedFoundationPrefixes)
	}
	if len(sharedFoundationPrefixes) == 0 {
		t.Fatalf("разрешённый фундамент пуст — тогда находкой становится КАЖДЫЙ импорт " +
			"своего модуля, и гейт краснеет на верном дереве")
	}

	var all []standaloneImportFinding
	total := standaloneImportCensus{}
	for _, svc := range standaloneBoundServices {
		abs, err := treecorpus.UnderWithSuffix(filepath.Join(root, svc), ".go")
		if err != nil {
			t.Fatalf("обход %s: %v — состав дерева взять неоткуда; «ноль находок» на "+
				"«ноль прочитанного» неотличимо от чистого сервиса. Проверь, не переехал "+
				"ли каталог и не снят ли он из перечня вынесенных", svc, err)
		}
		rels := make([]string, 0, len(abs))
		for _, p := range abs {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				t.Fatalf("относительный путь для %s: %v", p, err)
			}
			rels = append(rels, filepath.ToSlash(rel))
		}
		osRoot, err := os.OpenRoot(root)
		if err != nil {
			t.Fatalf("открыть корень %s: %v", root, err)
		}
		findings, census, err := scanStandaloneServiceImports(svc, rels, osRoot.ReadFile, module)
		_ = osRoot.Close()
		if err != nil {
			t.Fatalf("обход %s: %v", svc, err)
		}
		all = append(all, findings...)
		total.Services++
		total.Files += census.Files
		total.Imports += census.Imports
		total.OwnModule += census.OwnModule
		total.Allowed += census.Allowed
		total.FindingFiles += census.FindingFiles
		total.FindingEdges += census.FindingEdges
		total.LanguageBound += census.LanguageBound
	}
	t.Logf("перепись: %s", total)

	if total.Files == 0 {
		t.Fatalf("обход пуст: ни одного файла Go под %v — вердикт беспредметен", standaloneBoundServices)
	}
	if total.OwnModule == 0 {
		t.Fatalf("ни один из %d импортов не принадлежит модулю %s — так не бывает у "+
			"сервиса этого дерева: сломан разбор либо неверно прочитан путь модуля",
			total.Imports, module)
	}

	if len(all) > 0 {
		var b strings.Builder
		shown := all
		if len(shown) > 40 {
			shown = shown[:40]
		}
		for _, f := range shown {
			b.WriteString("\n  " + f.String())
		}
		if len(all) > len(shown) {
			b.WriteString(fmt.Sprintf("\n  … и ещё %d", len(all)-len(shown)))
		}
		t.Fatalf("%d импортов в %d файлах ведут за пределы общего фундамента:%s\n\n"+
			"Сервис из перечня %v станет отдельным модулем; законный вход у него — "+
			"публичная поверхность %s (стабы `pkg/api/...` — её часть) и собственное "+
			"поддерево. Общее, нужное не только ему, переезжает в `pkg/`; частное, "+
			"нужное только ему, — внутрь сервиса.\nПерепись: %s",
			total.FindingEdges, total.FindingFiles, b.String(),
			standaloneBoundServices, module+"/pkg/...", total)
	}
}
