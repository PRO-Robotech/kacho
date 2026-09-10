// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listfilteroperatorapplied_injection_test.go — доказательство того, что
// распознаватель «владелец потерял оператор фильтра» УМЕЕТ находить и УМЕЕТ
// молчать.
//
// # Зачем оно заведено ИМЕННО СЕЙЧАС
//
// До выноса службы доступа гейт держал свою предпосылку числом мест на настоящем
// дереве: «мест ноль ⇒ вердикт недействителен». Единственное место чтения
// `.Value` принадлежало её обработчику перечисления выдач и уехало вместе с
// нею, — и предпосылка стала падать на ДОСТИЖЕНИИ цели гейта.
//
// Заменить её можно было двумя способами, и один из них запрещён: снять проверку
// предпосылки вовсе (тогда «ноль находок» стало бы неотличимо от «ноль
// прочитанного»). Взят второй — перенести доказательство способности падать на
// СИНТЕТИКУ, которую строит сама проба. Тогда ноль мест на дереве есть факт о
// дереве, а не слепота, и это утверждение проверяемо.
//
// # Почему зовётся ТА ЖЕ функция
//
// Инъекция подаёт синтетический вход тем же путём, каким гейт подаёт настоящий:
// обе стороны зовут collectFilterOperatorSitesIn. Копия распознавателя
// доказывала бы свойство своей копии.
package repohygiene

import (
	"os"
	"path/filepath"
	"testing"
)

// injFilterOwnerLosingTheOperator — владелец, забирающий ЗНАЧЕНИЕ и теряющий
// вместе с ним оператор: предикат с зашитым `=` ответит равенством на запрос
// подстроки.
const injFilterOwnerLosingTheOperator = `package owner

import "github.com/PRO-Robotech/kacho/pkg/filter"

type params struct{ Name string }

func List(expr string, p *params) error {
	ast, err := filter.Parse(expr, []string{"name"})
	if err != nil {
		return err
	}
	if ast != nil {
		p.Name = ast.Value
	}
	return nil
}
`

// injFilterOwnerHonouringTheOperator — ЗАКОННЫЙ БЛИЗНЕЦ: то же чтение значения,
// но оператор применён. Форма отличается ОДНИМ фактом — вызовом ToSQLOn.
const injFilterOwnerHonouringTheOperator = `package owner

import "github.com/PRO-Robotech/kacho/pkg/filter"

type params struct {
	Name string
	SQL  string
}

func List(expr string, p *params) error {
	ast, err := filter.Parse(expr, []string{"name"})
	if err != nil {
		return err
	}
	if ast != nil {
		p.Name = ast.Value
		p.SQL, _ = ast.ToSQLOn("name", 1)
	}
	return nil
}
`

// injFilterOwnerViaHelper — разбор через ПОМОЩНИК пакета: помощник отдаёт узел
// и решения не принимает, решает вызывающий. Без этой стороны гейт имел бы
// слепое пятно ровно там, где класс и прятался.
const injFilterOwnerViaHelperParse = `package owner

import "github.com/PRO-Robotech/kacho/pkg/filter"

func parseIt(expr string) (*filter.FilterAST, error) {
	return filter.Parse(expr, []string{"name"})
}
`

const injFilterOwnerViaHelperUse = `package owner

type params struct{ Name string }

func List(expr string, p *params) error {
	ast, err := parseIt(expr)
	if err != nil {
		return err
	}
	if ast != nil {
		p.Name = ast.Value
	}
	return nil
}
`

// injFilterTree раскладывает названные файлы во временное дерево и отдаёт корень
// вместе с перечнем относительных путей.
func injFilterTree(t *testing.T, files map[string]string) (string, []string) {
	t.Helper()
	root := t.TempDir()
	var rels []string
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("каталог для %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("запись %s: %v", rel, err)
		}
		rels = append(rels, rel)
	}
	return root, rels
}

// TestFilterOperatorRecogniserRedensOnAnOwnerThatDropsTheOperator — КРАСНОЕ на
// внесённом дефекте, с координатой.
func TestFilterOperatorRecogniserRedensOnAnOwnerThatDropsTheOperator(t *testing.T) {
	t.Parallel()
	root, rels := injFilterTree(t, map[string]string{
		"services/demo/internal/repo/list.go": injFilterOwnerLosingTheOperator,
	})

	sites, filesRead := collectFilterOperatorSitesIn(t, root, rels)
	if filesRead != 1 {
		t.Fatalf("прочитано файлов %d, ожидался 1 — вход подан не тот", filesRead)
	}
	if len(sites) != 1 {
		t.Fatalf("мест найдено %d, ожидалось 1: распознаватель не видит чтения значения", len(sites))
	}
	if sites[0].honoursOp {
		t.Fatalf("место признано применяющим оператор, а оно его теряет: %+v", sites[0])
	}
	if sites[0].file != "services/demo/internal/repo/list.go" || sites[0].line == 0 {
		t.Fatalf("находка не называет координату: %+v", sites[0])
	}
}

// TestFilterOperatorRecogniserIsSilentOnAnOwnerThatHonoursIt — ЗАКОННЫЙ
// БЛИЗНЕЦ: одно отличие от дефекта — оператор применён.
//
// Без этой стороны распознаватель мог бы считать находкой ЛЮБОЕ чтение
// значения, и первый же ложный срабат снял бы гейт.
func TestFilterOperatorRecogniserIsSilentOnAnOwnerThatHonoursIt(t *testing.T) {
	t.Parallel()
	root, rels := injFilterTree(t, map[string]string{
		"services/demo/internal/repo/list.go": injFilterOwnerHonouringTheOperator,
	})

	sites, _ := collectFilterOperatorSitesIn(t, root, rels)
	if len(sites) != 1 {
		t.Fatalf("мест найдено %d, ожидалось 1: близнец обязан быть ВИДЕН распознавателю, "+
			"иначе его молчание означает «не нашёл», а не «нарушения нет»", len(sites))
	}
	if !sites[0].honoursOp {
		t.Fatalf("законный близнец признан теряющим оператор — гейт краснел бы на верном коде: %+v",
			sites[0])
	}
}

// TestFilterOperatorRecogniserFollowsThePackageHelper — косвенный разбор
// прослеживается, а сам помощник находкой не является.
func TestFilterOperatorRecogniserFollowsThePackageHelper(t *testing.T) {
	t.Parallel()
	root, rels := injFilterTree(t, map[string]string{
		"services/demo/internal/repo/parse.go": injFilterOwnerViaHelperParse,
		"services/demo/internal/repo/list.go":  injFilterOwnerViaHelperUse,
	})

	sites, filesRead := collectFilterOperatorSitesIn(t, root, rels)
	if filesRead != 2 {
		t.Fatalf("прочитано файлов %d, ожидалось 2", filesRead)
	}
	if len(sites) != 1 {
		t.Fatalf("мест найдено %d, ожидалось 1 (вызывающий помощника, но НЕ сам помощник): %+v",
			len(sites), sites)
	}
	if sites[0].fn != "List" {
		t.Fatalf("находка названа на %q, а решение принимает List: сам помощник решения не "+
			"принимает и находкой быть не должен", sites[0].fn)
	}
	if sites[0].viaHelper != "parseIt" {
		t.Fatalf("косвенный разбор не прослежен: viaHelper=%q", sites[0].viaHelper)
	}
	if sites[0].honoursOp {
		t.Fatalf("место признано применяющим оператор, а оно его теряет: %+v", sites[0])
	}
}

// TestFilterOperatorRecogniserIsSilentOnAnEmptyWorld — контроль третьей
// стороны: на дереве без разбора мест ноль, и это НЕ находка.
//
// Он же и есть то, чем ноль на настоящем дереве отличается от слепоты: слепой
// распознаватель дал бы ноль и на дефекте выше.
func TestFilterOperatorRecogniserIsSilentOnAnEmptyWorld(t *testing.T) {
	t.Parallel()
	root, rels := injFilterTree(t, map[string]string{
		"services/demo/internal/repo/list.go": "package owner\n\nfunc List() {}\n",
	})

	sites, filesRead := collectFilterOperatorSitesIn(t, root, rels)
	if filesRead != 1 {
		t.Fatalf("прочитано файлов %d, ожидался 1", filesRead)
	}
	if len(sites) != 0 {
		t.Fatalf("мест найдено %d, ожидалось 0: распознаватель считает находкой то, где разбора нет: %+v",
			len(sites), sites)
	}
}
