// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotaceilingcatalog_test.go — репо-широкий гейт: КАТАЛОГ ВЕЛИЧИН посадки
// домена равен каталогу, который СПИСЫВАЕТ его триггер, и страж мощности
// ПОЗВАН стражем старта этого домена.
//
// ЗАЧЕМ ОН СУЩЕСТВУЕТ. Величины потолков объявляет посадка домена (приёмка
// судьбы авторитета величин, исход «величину объявляет посадка», стадия S1), и
// страж мощности требует одного из двух: либо величин не объявлено ни одной,
// либо объявлен КАТАЛОГ ЦЕЛИКОМ. Слово «целиком» здесь имеет смысл ровно
// постольку, поскольку каталог посадки совпадает с тем, что домен списывает.
//
// ЧТО ПРОИСХОДИТ БЕЗ НЕГО, ВАРИАНТ ПЕРВЫЙ — ПРОПУЩЕННЫЙ ВИД. Вид, который
// триггер списывает, а таблица величин не объявляет, делает стража мощности
// ВАКУУМНЫМ для этого вида: оператор объявляет «каталог целиком», страж молчит,
// а вид остаётся без предела и не сообщает об этом ничем — ни отказом, ни
// записью, ни строкой витрины. Это ровно та половина пары, которая хуже
// отсутствия обеих, потому что ВЫГЛЯДИТ настроенной.
//
// ВАРИАНТ ВТОРОЙ — МЁРТВАЯ РУЧКА. Вид, объявленный таблицей и не списываемый
// никем, требует от оператора величины, которая не действует ни на что:
// невыполнимое условие старта ради вида, которого нет.
//
// ВАРИАНТ ТРЕТИЙ — СТРАЖ, КОТОРОГО НИКТО НЕ ЗОВЁТ. Проверка, не попавшая в
// агрегатор старта, есть ЛОВУШКА: она выглядит как часть «полной проверки
// старта», а частичный набор проезжает мимо неё молча.
//
// ЧЕГО ГЕЙТ НЕ ЛОВИТ, И ЭТО СКАЗАНО ЧЕСТНО. Виды он читает СТРОКОВЫМИ
// ЛИТЕРАЛАМИ таблицы величин: вид, собранный в рантайме, ему не виден. В файле
// таблицы литерал формы `<домен>.<вид>` не встречается ни в одной другой роли —
// ключи ручек односегментны, имена переменных выводятся, а доводы оператору
// написаны прозой, — и предпосылку эту гейт проверяет сам: пустой набор видов у
// любого домена он объявляет находкой, а не молчанием.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// quotaServicesDir — дерево служб платформы. ЕДИНСТВЕННАЯ выписанная
// координата: состав владельцев величин из неё ВЫВОДИТСЯ, а не перечисляется.
const quotaServicesDir = "services"

// quotaCeilingCatalogFile — имя файла таблицы величин. Владельцы находятся по
// нему, а не по списку имён: список разошёлся бы с деревом молча.
const quotaCeilingCatalogFile = "quota_ceilings.go"

var (
	// вид в объявлении триггера учёта: EXECUTE FUNCTION kacho_quota_count('vpc.network' …)
	//
	// Перенос строки ТЕРПИМ: объявление триггера переносит вид на следующую
	// строку у двух владельцев из пяти, и однострочная форма теряла бы их молча.
	quotaChargedKindRe = regexp.MustCompile(`kacho_quota_count\(\s*'([a-zA-Z0-9.]+)'`)
	// форма ВИДА: два и более сегмента, первый со строчной буквы.
	//
	// Ключ ручки односегментен (`network`), имя переменной пишется прописными,
	// довод оператору — проза с пробелами: ни одна из трёх форм под неё не
	// подпадает.
	quotaKindShapeRe = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*(\.[a-zA-Z0-9]+)+$`)
)

// quotaOwner — один владелец величин: что он списывает, что объявляет и зовёт ли
// стража мощности.
type quotaOwner struct {
	service     string
	charged     map[string]bool
	stated      map[string]bool
	catalogFile string
	guardCalled bool
	sqlFiles    int
	goFiles     int
}

func TestQuotaCeilingCatalogMatchesTheKindsTheDomainCharges(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	owners := quotaOwnersFromTree(t, root)
	if len(owners) == 0 {
		t.Fatalf("предпосылка гейта не выполнена: под %s не найдено ни одного владельца "+
			"величин — форма объявления триггеров учёта или имя файла таблицы "+
			"изменились, и гейт судит пустоту", quotaServicesDir)
	}

	totalCharged, totalStated, sqlSeen, goSeen := 0, 0, 0, 0
	names := make([]string, 0, len(owners))
	for name := range owners {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		o := owners[name]
		sqlSeen += o.sqlFiles
		goSeen += o.goFiles
		totalCharged += len(o.charged)
		totalStated += len(o.stated)

		if len(o.charged) == 0 {
			t.Errorf("%s: видов списывается НОЛЬ при найденной таблице величин %s — "+
				"форма объявления триггеров изменилась, и вердикт по этому владельцу "+
				"беспредметен", name, o.catalogFile)
			continue
		}
		if len(o.stated) == 0 {
			t.Errorf("%s: таблица величин %s не называет НИ ОДНОГО вида. Страж мощности "+
				"судил бы пустоту и был бы зелен при любой посадке", name, o.catalogFile)
			continue
		}

		var missing []string
		for kind := range o.charged {
			if !o.stated[kind] {
				missing = append(missing, kind)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s: триггер списывает вид, чью величину таблица посадки НЕ объявляет "+
				"(%d). Страж мощности для такого вида ВАКУУМЕН: оператор объявляет "+
				"«каталог целиком», страж молчит, а вид остаётся без предела и не "+
				"сообщает об этом ничем:\n  %s\n  таблица: %s",
				name, len(missing), strings.Join(missing, "\n  "), o.catalogFile)
		}

		var stray []string
		for kind := range o.stated {
			if !o.charged[kind] {
				stray = append(stray, kind)
			}
		}
		sort.Strings(stray)
		if len(stray) > 0 {
			t.Errorf("%s: таблица посадки объявляет вид, которого НЕ списывает ни один "+
				"триггер (%d). Ручка требует от оператора величины, не действующей ни "+
				"на что, — невыполнимое условие старта ради вида, которого нет:\n  %s\n"+
				"  таблица: %s", name, len(stray), strings.Join(stray, "\n  "), o.catalogFile)
		}

		if !o.guardCalled {
			t.Errorf("%s: страж мощности объявлен (%s) и НЕ ПОЗВАН ни одним не-тестовым "+
				"файлом службы. Проверка, не попавшая в агрегатор старта, есть ловушка: "+
				"она выглядит как часть «полной проверки старта», а частично объявленный "+
				"набор проезжает мимо неё молча", name, o.catalogFile)
		}
	}

	// Перепись осмотренного: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного».
	t.Logf("осмотрено: владельцев величин %d; миграций %d; файлов Go %d; "+
		"видов списывается %d; величин объявлено %d",
		len(owners), sqlSeen, goSeen, totalCharged, totalStated)
}

// quotaOwnersFromTree ВЫВОДИТ состав владельцев из дерева: владелец — служба, у
// которой есть и таблица величин, и миграция со списывающим триггером.
func quotaOwnersFromTree(t *testing.T, root string) map[string]*quotaOwner {
	t.Helper()

	goFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, quotaServicesDir), ".go")
	if err != nil {
		t.Fatalf("состав дерева под %s: %v", quotaServicesDir, err)
	}
	sqlFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, quotaServicesDir), ".sql")
	if err != nil {
		t.Fatalf("состав дерева под %s: %v", quotaServicesDir, err)
	}

	owners := map[string]*quotaOwner{}
	owner := func(service string) *quotaOwner {
		o, ok := owners[service]
		if !ok {
			o = &quotaOwner{
				service: service,
				charged: map[string]bool{},
				stated:  map[string]bool{},
			}
			owners[service] = o
		}
		return o
	}

	// Сторона СПИСАНИЯ: все миграции службы, а не одна названная.
	for _, path := range sqlFiles {
		service, ok := quotaServiceOf(root, path)
		if !ok {
			continue
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", path, rerr)
		}
		hits := quotaChargedKindRe.FindAllStringSubmatch(string(b), -1)
		if len(hits) == 0 {
			continue
		}
		o := owner(service)
		o.sqlFiles++
		for _, m := range hits {
			o.charged[m[1]] = true
		}
	}

	// Сторона ОБЪЯВЛЕНИЯ и вызов стража.
	for _, path := range goFiles {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		service, ok := quotaServiceOf(root, path)
		if !ok {
			continue
		}
		isCatalog := filepath.Base(path) == quotaCeilingCatalogFile
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", path, rerr)
		}
		if !isCatalog && !strings.Contains(string(b), "ValidateQuotaCeilings") {
			continue
		}
		file, perr := parser.ParseFile(token.NewFileSet(), path, b, 0)
		if perr != nil {
			t.Fatalf("разбор %s: %v", path, perr)
		}
		o := owner(service)
		o.goFiles++
		rel, _ := filepath.Rel(root, path)
		if isCatalog {
			o.catalogFile = filepath.ToSlash(rel)
			for _, kind := range quotaKindLiterals(file) {
				o.stated[kind] = true
			}
		}
		if quotaGuardIsCalled(file) {
			o.guardCalled = true
		}
	}

	// Владелец — тот, у кого есть ОБЕ стороны. Служба без таблицы величин пока
	// не владелец посадки, и судить её этим гейтом нечем.
	for name, o := range owners {
		if o.catalogFile == "" {
			delete(owners, name)
		}
	}
	return owners
}

// quotaServiceOf — имя службы по пути внутри дерева служб.
func quotaServiceOf(root, path string) (string, bool) {
	rel, err := filepath.Rel(filepath.Join(root, quotaServicesDir), path)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") {
		return "", false
	}
	i := strings.IndexByte(rel, '/')
	if i <= 0 {
		return "", false
	}
	return rel[:i], true
}

// quotaKindLiterals — виды, названные СТРОКОВЫМИ ЛИТЕРАЛАМИ файла.
//
// Судится узел разбора, а не текст: имя вида встречается и в прозе доводов, и в
// комментариях, и проверка по подстроке краснела бы на собственном объяснении.
func quotaKindLiterals(file *ast.File) []string {
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if quotaKindShapeRe.MatchString(v) {
			out = append(out, v)
		}
		return true
	})
	return out
}

// quotaGuardIsCalled — страж мощности ПОЗВАН: в файле есть вызов, а не только
// объявление метода.
func quotaGuardIsCalled(file *ast.File) bool {
	called := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "ValidateQuotaCeilings" {
			called = true
		}
		return true
	})
	return called
}
