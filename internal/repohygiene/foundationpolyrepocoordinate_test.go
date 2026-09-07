// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// foundationpolyrepocoordinate_test.go — проза ФУНДАМЕНТА не посылает читателя
// по координате прежней полирепо-топологии.
//
// # Предмет
//
// Шапка пакета и godoc порта — это указание «иди сюда за реализацией». Пока
// части продукта лежали в отдельных репозиториях, такое указание писалось их
// именами: `kacho-corelib/backoff`, `kacho-proto/proto/...`, клиентский адаптер
// в дереве отдельного сервиса. Сегодня это каталоги ОДНОГО дерева, и ни одна из
// тех координат не резолвится.
//
// Устаревание молчит by construction: комментарий не собирается, не
// импортируется и конфликта при слиянии не даёт. Читатель уходит искать файл,
// которого нет, и — что хуже — заводит его заново там, где указано.
//
// # Почему именно фундамент, а не всё дерево
//
// Популяция выбрана по ЦЕНЕ ошибки, а не по удобству. `pkg/` — общий фундамент:
// его godoc читают шесть потребителей, и он единственное место, где написано,
// какой пакет реализует какой порт. Ложное указание здесь тиражируется на всех.
//
// `proto/` добавлен к популяции по ТОЙ ЖЕ цене, и она там ВЫШЕ (#2199).
// Комментарий поля контракта не остаётся в контракте: генерация переносит его
// ДОСЛОВНО в стаб каждого потребителя, а стаб править нельзя — на нём клеймо
// «DO NOT EDIT». Значит ложная координата в контракте тиражируется машинно и
// чинится только в своём источнике; судить её надо там, где она пишется.
//
// Судится ВИД файла, а не каталог целиком: под `pkg/` лежат порождённые стабы,
// под `proto/` — только `.proto`. Пара «каталог + вид» объявлена в
// foundationProseRoots, и её расширение — решение, а не правка регулярки.
//
// Соседние популяции НАЗВАНЫ, а не умолчаны, и «ноль» по ним означает «не
// искали»: дерево служб (там координата в чужом сервисе бывает законной
// ссылкой на образец), край, документы (их ведёт
// `TestFormerRepositoryNamesInDocsNameTheCurrentTree` со своим предикатом —
// «называешь прежнее имя, назови рядом нынешнее место»). Перепись печатает
// число осмотренных файлов фундамента, поэтому нулевая находка отличима от
// несостоявшегося обхода.
//
// # Что считается координатой
//
// Имя прежнего репозитория ВМЕСТЕ с путём за ним: `kacho-corelib/backoff`,
// `kacho-vpc/internal/clients/...`. Голое имя без пути координатой НЕ является и
// в предмет не входит — им называют службу в прозе («реестр пишет кортеж»), и
// требовать от такой фразы резолва значило бы судить слово вместо адреса. Это
// полоса, и она считается отдельным числом.
//
// Имена выведены из ЗАКРЫТОГО перечня прежних репозиториев. Перечень закрыт
// намеренно: `x-kacho-principal-id/type` — заголовок запроса, а не путь, и
// предикат «любое слово после `kacho-`» зачёл бы его координатой.
//
// # Резолв — суффиксом, а не точным равенством
//
// Координата в прозе пишется от корня СЛУЖБЫ, а не репозитория
// (`cmd/kacho-geo/serve.go`), поэтому совпадение ищется суффиксом пути. Это
// делает гейт мягче, и направление выбрано осознанно: он стережёт ВОЗВРАЩЕНИЕ
// мёртвого адреса, а ложная находка на живом отключила бы его первой же.
//
// # Почему это гейт, а не внимательность
//
// Предмет уже чинили. Изменение 534996d979 назвало адаптер по его нынешнему
// дому; массовый переезд контракта d46aaa7280, сделанный на ОТСТАВШЕЙ копии,
// вернул прежний текст — без конфликта, без красного, без единого признака.
// Ведомость полноты того отката считает ИМЕНА и прозаическую работу не видит by
// construction; нашла её вторая единица счёта, построенная вручную.
//
// Гейт закрывает ровно эту дыру: следующий такой откат вернёт в фундамент
// мёртвую координату, и она станет красной на первом же прогоне.
//
// # Порождённое не судится — это граница ПОПУЛЯЦИИ, а не послабление
//
// Файл с клеймом `Code generated … DO NOT EDIT.` править нельзя: его текст
// приходит из комментария контракта, и починка принадлежит контракту, а не
// фундаменту. Число пропущенных печатается, поэтому полоса видна.
//
// Прежде эта граница была ДЫРОЙ, и здесь стояло, что держателя у экземпляра нет
// ни с одной стороны: комментарий поля контракта реестра называл прежний
// репозиторий общего фундамента, стаб нёс ту же строку дословно, а гейт
// документов до `.proto` не достаёт — его популяция это отслеживаемая проза
// `.md`/`.mdx`. Дыра закрыта тем, что источник вошёл в популяцию (#2199): теперь
// пропуск порождённого означает «починка принадлежит контракту, и контракт
// судится», а не «не судится никем».
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// foundationProseScope — каталог И вид файла, чья проза судится. Вид назван
// рядом с каталогом намеренно: под `pkg/` лежат порождённые стабы и данные, под
// `proto/` — только контракт, и предикат «любой файл каталога» читал бы разное.
type foundationProseScope struct {
	prefix string
	ext    string
}

// foundationProseRoots — фундамент продукта в двух его половинах: общий Go-код
// и КОНТРАКТ. Перечень оставлен перечнем: расширение популяции обязано быть
// решением, а не правкой строки в регулярке.
var foundationProseRoots = [...]foundationProseScope{
	{prefix: "pkg/", ext: ".go"},
	{prefix: "proto/", ext: ".proto"},
}

// formerRepoSuffixes — вторые половины имён прежних репозиториев. Перечень
// ЗАКРЫТ: предикат «любое слово после приставки» зачёл бы за координату
// заголовок запроса и имя метрики.
var formerRepoSuffixes = [...]string{
	"proto", "corelib", "vpc", "nlb", "iam", "compute",
	"storage", "registry", "geo", "api-gateway", "deploy", "ui", "test",
}

// polyrepoCoordinateRe — имя прежнего репозитория, ЗА КОТОРЫМ идёт путь.
//
// Левая граница исключает продолжение чужого идентификатора
// (`x-kacho-principal-id`): перед приставкой не должно стоять ни буквы, ни
// цифры, ни дефиса. Хвост берётся жадно и затем усекается по знакам, которыми
// проза обрамляет координату.
var polyrepoCoordinateRe = regexp.MustCompile(
	`(?:^|[^A-Za-z0-9_-])(kacho-(?:` + strings.Join(formerRepoSuffixes[:], "|") + `)(?:/[A-Za-z0-9_.*-]+)+)`)

// generatedMarkerRe — клеймо порождённого файла в форме, которую ставит сам
// генератор.
var generatedMarkerRe = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

type polyrepoFinding struct {
	file  string
	line  int
	coord string
	text  string
}

func (f polyrepoFinding) String() string {
	return fmt.Sprintf("%s:%d: %s — %s", f.file, f.line, f.coord, strings.TrimSpace(f.text))
}

type polyrepoCensus struct {
	filesRead      int // файлов фундамента прочитано
	filesGenerated int // пропущено с клеймом порождённого
	coordinates    int // координат (имя + путь) распознано
	resolved       int // из них резолвится в дереве
	bareNames      int // имя без пути — полоса, координатой не является
}

func (c polyrepoCensus) String() string {
	return fmt.Sprintf("файлов фундамента прочитано %d · порождённых пропущено %d · "+
		"координат распознано %d (резолвится %d) · голых имён без пути %d",
		c.filesRead, c.filesGenerated, c.coordinates, c.resolved, c.bareNames)
}

// bareFormerNameRe — имя прежнего репозитория БЕЗ пути за ним. Считается
// отдельно, чтобы полоса была названа числом, а не умолчанием.
var bareFormerNameRe = regexp.MustCompile(
	`(?:^|[^A-Za-z0-9_-])kacho-(?:` + strings.Join(formerRepoSuffixes[:], "|") + `)(?:[^/A-Za-z0-9_-]|$)`)

// coordinateResolves — путь есть в дереве сам по себе либо суффиксом какого-то
// отслеживаемого пути. Суффикс нужен, потому что проза пишет координату от
// корня службы, а не репозитория.
func coordinateResolves(tree *treecorpus.Tree, coord string) bool {
	coord = strings.TrimRight(coord, ".,;:")
	if coord == "" {
		return false
	}
	if tree.HasFile(coord) || tree.HasDir(coord) {
		return true
	}
	tail := "/" + coord
	for rel := range tree.Files() {
		if strings.HasSuffix(rel, tail) || strings.Contains(rel, tail+"/") {
			return true
		}
	}
	return false
}

// scanFoundationProse разбирает ПРОИЗВОЛЬНОЕ дерево: настоящий репозиторий и
// синтетический корень инъекции проходят одну и ту же функцию, поэтому
// доказанное на втором верно для первого.
func scanFoundationProse(tree *treecorpus.Tree) (polyrepoCensus, []polyrepoFinding, error) {
	var census polyrepoCensus
	var findings []polyrepoFinding

	root := tree.Root()
	for _, rel := range tree.SortedFiles() {
		slash := filepath.ToSlash(rel)
		inFoundation := false
		for _, sc := range foundationProseRoots {
			if strings.HasPrefix(slash, sc.prefix) && strings.HasSuffix(slash, sc.ext) {
				inFoundation = true
				break
			}
		}
		if !inFoundation {
			continue
		}

		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return census, nil, fmt.Errorf("фундамент: файл %s не прочитан: %w", rel, err)
		}
		if generatedMarkerRe.Match(raw) {
			census.filesGenerated++
			continue
		}
		census.filesRead++

		for i, line := range strings.Split(string(raw), "\n") {
			census.bareNames += len(bareFormerNameRe.FindAllString(line, -1))
			for _, m := range polyrepoCoordinateRe.FindAllStringSubmatch(line, -1) {
				coord := m[1]
				census.coordinates++
				if coordinateResolves(tree, coord) {
					census.resolved++
					continue
				}
				findings = append(findings, polyrepoFinding{slash, i + 1, coord, line})
			}
		}
	}

	if census.filesRead == 0 {
		return census, nil, fmt.Errorf("фундамент: обход пуст — вердикт беспредметен (корень %q)", root)
	}
	return census, findings, nil
}

// TestFoundationProseNamesNoPolyrepoCoordinate — гейт класса.
func TestFoundationProseNamesNoPolyrepoCoordinate(t *testing.T) {
	tree, err := treecorpus.NewTree(repoRoot(t))
	if err != nil {
		t.Fatalf("состав дерева не собран — вердикт беспредметен: %v", err)
	}

	census, findings, err := scanFoundationProse(tree)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	t.Logf("перепись: %s", census)

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("проза фундамента посылает по %d координатам прежней полирепо-топологии, "+
			"которых в дереве нет:%s\n\nсегодня это каталоги ОДНОГО дерева: `kacho-corelib/<пакет>` "+
			"— это `pkg/<пакет>`, `kacho-proto/proto/...` — это `proto/` и стабы `pkg/api/...`, "+
			"а клиентский адаптер порта проверки живёт в `pkg/authz/authziam`.\nПерепись: %s",
			len(findings), b.String(), census)
	}
}
