// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// commentgateradius_test.go — гейт «прохибиционный список одного сервиса
// объявляет, что он СЛУЖЕБНЫЙ, а не держатель общего запрета».
//
// # Предмет — и почему это не вкус
//
// Инструмент, судящий комментарии по набору запретов, живёт сегодня в одном
// сервисе из семи. Читатель, не нашедший его у соседа, делает один из двух
// выводов, и они противоположны: «предмет узок» либо «предмет общий, просто до
// соседа не доехало». Ничто в дереве эти два случая не различает.
//
// Различить их может только сам инструмент — сказав о своём радиусе в шапке. А
// проверить, что он это сказал, может только гейт: «сказано вслух» без
// опознаваемой формы есть обещание, которое стареет молча.
//
// # Почему требование адресовано НЕ ВСЕМ спискам, а только служебным
//
// Список, лежащий в общем фундаменте (`internal/`, `tools/`, `pkg/`), тревиально
// общий: он обходит дерево целиком и радиуса не скрывает. Список под
// `services/<имя>/` виден одному сервису, и вот у него радиус спорен.
//
// # Почему требование адресовано НЕ ВСЕМ служебным спискам
//
// Только тем, чей набор запрещает форму, которую корпус ТРЕБУЕТ. Список,
// запрещающий одни лишь настоящие нарушения, поднимается на дерево даром, и
// объявлять ему нечего: разница между «узко» и «не доехало» у него не спорна.
//
// Проба этого — не чтение, а прогон: каждый образец набора подаётся корпусу
// нормативных форм, и совпадение означает «этот список нельзя поднять как есть».
//
// # Контроль в обратную сторону — ВНУТРИ гейта
//
// «Ни один список не запрещает нормативную форму» и «прогонщик сломан» дают
// одинаковое молчание. Поэтому рядом стоит контрольный корпус НАСТОЯЩИХ
// нарушений: хотя бы один образец обязан совпасть с ним. Не совпал — сломан
// прогонщик, и гейт говорит именно это, а не «дерево чистое».
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// radiusServiceLocal — префикс пути, делающий список СЛУЖЕБНЫМ.
	radiusServiceLocal = "services/"
	// radiusCensusFloor — порог переписи: ниже него «ноль находок» означало бы
	// «ноль прочитанного».
	radiusCensusFloor = 1000
)

// TestServiceLocalCommentProhibitionListDeclaresItsRadius — сам гейт.
func TestServiceLocalCommentProhibitionListDeclaresItsRadius(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed, literals, partial int
		lists                     []*CommentProhibitionList
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		list, census, err := ScanCommentProhibitionList(rel, src)
		if err != nil {
			// Файл, который не разбирается, судить нечем. Он остаётся посчитанным
			// переписью — иначе объём осмотренного завысился бы.
			parsed++
			continue
		}
		parsed++
		literals += census.Literals
		if census.SignalsMet > 0 && list == nil {
			partial++
		}
		if list != nil {
			lists = append(lists, list)
		}
	}

	var patterns int
	var serviceLocal, treeWide []*CommentProhibitionList
	for _, l := range lists {
		patterns += len(l.Patterns)
		if strings.HasPrefix(l.File, radiusServiceLocal) {
			serviceLocal = append(serviceLocal, l)
		} else {
			treeWide = append(treeWide, l)
		}
	}

	t.Logf("перепись: файлов Go разобрано %d, образцов сравнения встречено %d, "+
		"файлов с частью признаков %d, наборов запретов найдено %d "+
		"(служебных %d, общефундаментных %d), образцов в наборах %d; "+
		"нормативных форм в корпусе %d, контрольных нарушений %d",
		parsed, literals, partial, len(lists),
		len(serviceLocal), len(treeWide), patterns,
		len(MandatedCommentForms), len(ControlViolationForms))

	// (1) Предпосылки. Каждая отвечает на свой вопрос, и ни одна не выводится из
	// соседней.
	if parsed < radiusCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — на таком "+
			"объёме «ноль находок» означало бы «ноль прочитанного»", parsed, radiusCensusFloor)
	}
	if literals == 0 {
		t.Fatalf("встречено ноль образцов сравнения на %d файлах — разбор перестал видеть "+
			"предмет, и его молчание сказано ни о чём", parsed)
	}
	if len(lists) == 0 {
		t.Fatalf("наборов запретов в дереве НОЛЬ — либо последний снят, либо разбор " +
			"перестал их опознавать. Гейт беспредметен: отличить эти два случая по его " +
			"молчанию нельзя. Снят предмет — снимайте и этот гейт вместе с его инъекцией " +
			"и docs/architecture/comment-hygiene-radius.md")
	}
	if len(MandatedCommentForms) == 0 || len(ControlViolationForms) == 0 {
		t.Fatal("корпус проб пуст — прогонять нечего")
	}

	// (2) Контроль прогонщика: хотя бы один образец обязан находить настоящее
	// нарушение. Без него молчание на нормативных формах неотличимо от сломанного
	// прогонщика.
	controlHits := CountControlHits(lists, ControlViolationForms)
	if controlHits == 0 {
		t.Fatalf("ни один из %d образцов не нашёл ни одного из %d контрольных нарушений — "+
			"прогонщик ничего не измеряет, и его молчание на нормативных формах ничего "+
			"не значит", patterns, len(ControlViolationForms))
	}
	t.Logf("контроль прогонщика: совпадений с настоящими нарушениями %d", controlHits)

	// (3) Требование: служебный набор, запрещающий нормативную форму, объявляет
	// свой радиус. Судит ОДНА функция — та же, которой инъекция доказывает
	// способность гейта упасть.
	exists := func(rel string) bool { return tt.hasFile(rel) || tt.hasDir(rel) }
	var findings []string
	for _, l := range serviceLocal {
		finding, collisions := JudgeServiceLocalList(l, MandatedCommentForms, exists)
		if collisions == 0 {
			t.Logf("служебный набор %s нормативных форм не запрещает — объявления радиуса "+
				"не требует", l.File)
			continue
		}
		t.Logf("служебный набор %s запрещает нормативную форму в %d месте(ах)", l.File, collisions)
		if finding != "" {
			findings = append(findings, finding)
			continue
		}
		named, _ := DeclarationNamesAPath(l.Declaration, exists)
		t.Logf("служебный набор %s объявляет радиус и называет живую координату %s", l.File, named)
	}

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Errorf("прохибиционных списков без объявления радиуса — %d:\n  %s\n\n"+
			"Отсутствие такого списка у соседнего сервиса читается как РАЗРЕШЕНИЕ, пока "+
			"сам список не сказал, что он служебный. Решение и цена обеих сторон — "+
			"docs/architecture/comment-hygiene-radius.md",
			len(findings), strings.Join(findings, "\n  "))
	}

	for _, l := range treeWide {
		t.Logf("набор общего фундамента (радиуса не скрывает, объявления не требует): "+
			"%s, образцов %d", l.File, len(l.Patterns))
	}
}
