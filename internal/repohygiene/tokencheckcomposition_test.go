// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokencheckcomposition_test.go — перечень обязательных проверок токена объявлен
// РОВНО В ОДНОМ месте, и каждая поверхность приёма, входящая в область фазы, им
// пользуется (приёмка F1, сценарий F1-28).
//
// # Предмет
//
// Приём нашего токена исполняют РАЗНЫЕ реализации на разных поверхностях: одна
// разбирает токен библиотекой, другая — своим кодом над crypto. Пока состав
// обязательных проверок живёт у каждой свой, различие между ними НЕ ВЫРАЖЕНО и
// потому не может покраснеть ни у кого: одна перестанет требовать срок, другая
// тип, и об этом не узнает никто, потому что спрашивать не у чего.
//
// Отсюда форма требования. Перечень объявляется один раз (pkg/tokenpolicy), а
// реализация ОБЪЯВЛЯЕТ, какие проверки она исполняет. Тогда расхождение
// становится предметом: `tokenpolicy.MissingChecks(объявленное)` непусто.
//
// # Гейт СВЕРЯЕТСЯ С ОДНИМ ОБЪЯВЛЕНИЕМ, а не со своей копией
//
// Обязательный перечень берётся вызовом `tokenpolicy.MandatoryChecks()`, а
// сопоставление «имя константы → её значение» читается из объявления констант
// того же пакета. Выписать перечень здесь означало бы завести второе место об
// одном предмете внутри гейта, который этот класс и запрещает.
//
// # Что входит в ОБЛАСТЬ, а что нет — и почему это сказано, а не умолчано
//
// Построений проверяющего в дереве два: плоскость данных реестра и КРАЙ. В
// область под-фазы F1 переводится первое. Край в этой фазе НЕ переводится —
// решение объёма, а не недосмотр, — и потому вынесен из области ЯВНО, записью в
// словаре ниже.
//
// Гейт, зелёный оттого, что он ни на что не смотрит, здесь запрещён так же, как
// в продукте. Поэтому у послабления три свойства сразу:
//
//   - оно ИМЕНУЕТ предикат, которым край будет втянут в область позже (поле
//     Predicate записи словаря): пока предикат не выполнен, послабление живо, и
//     видно, чего именно ждут;
//   - оно САМОИСТЕКАЕТ: как только пакет края объявит свой состав проверок,
//     запись становится находкой — «послаблению больше нечего исключать»;
//   - оно НЕ снимает переписи: построение края всё равно ищется в дереве, и
//     запись словаря без построения тоже находка.
//
// # Что этот гейт НЕ проверяет
//
// Он судит ОБЪЯВЛЕНИЕ состава, а не его правдивость: константа, названная и не
// исполняемая, разбору по дереву неотличима от исполняемой. Правдивость держит
// проба самого пакета — она вызывает проверяющего и подаёт ему токен, у которого
// не хватает ровно одного признака. Гейт закрывает другой класс: состав, который
// вообще НЕ ВЫРАЖЕН.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

const (
	// tokenCheckModulePath — путь модуля. Нужен, чтобы привести путь импорта к
	// каталогу дерева и обратно.
	tokenCheckModulePath = "github.com/PRO-Robotech/kacho"
	// tokenCheckPolicyImport — единственный дом перечня.
	tokenCheckPolicyImport = tokenCheckModulePath + "/pkg/tokenpolicy"
	// tokenCheckListName — имя объявления обязательного перечня.
	tokenCheckListName = "MandatoryChecks"
	// tokenCheckDeclName — имя, которым реализация объявляет свой состав.
	tokenCheckDeclName = "DeclaredChecks"
	// tokenCheckTypeName — тип константы перечня.
	tokenCheckTypeName = "Check"
	// tokenCheckCensusFloor — порог переписи: ниже него «ноль находок» означает
	// «ноль прочитанного».
	tokenCheckCensusFloor = 500
)

// tokenVerifierProducer — запись словаря производителей проверяющего.
type tokenVerifierProducer struct {
	// InScope — переводится ли эта поверхность на объявленный перечень в этой
	// под-фазе.
	InScope bool
	// Why — чем эта поверхность является. Нужен в тексте отказа: по имени
	// функции читатель не восстановит, о чём речь.
	Why string
	// Predicate — что должно стать верным, чтобы поверхность вошла в область.
	// Заполняется только у записей вне области: послабление без названного
	// предиката снятия бессрочно, и снять его будет некому.
	Predicate string
}

// tokenVerifierProducers — закрытый словарь построений проверяющего.
//
// Ключ — ПОЛНЫЙ путь импорта плюс имя функции: псевдоним пакета задаёт
// вызывающий, и по имени в исходнике две разные реализации неразличимы.
//
// Словарь, а не поиск по признаку: «построить проверяющего» синтаксического
// признака не имеет — одна реализация зовёт конструктор, другая собирает
// структуру полями. Перечень из двух записей читается и опровергается целиком, а
// признак, придуманный под две записи, дал бы уверенность, которой нет.
var tokenVerifierProducers = map[string]tokenVerifierProducer{
	tokenCheckModulePath + "/services/registry/internal/clients/jwks.New": {
		InScope: true,
		Why:     "плоскость данных реестра: проверяет предъявленный токен на каждом обращении docker-клиента",
	},
	tokenCheckModulePath + "/gateway/internal/middleware.NewJWTVerifier": {
		InScope: false,
		Why:     "край: одна конфигурация обслуживает обе его поверхности — REST и нативную gRPC",
		Predicate: "край входит в область, когда его пакет объявит свой состав проверок " +
			"(функция " + tokenCheckDeclName + ", возвращающая срез " + tokenCheckTypeName +
			" пакета политики). До тех пор запись живёт; как только объявление появится, " +
			"эта запись становится находкой сама — снимать её памятью не придётся",
	},
}

// tokenCheckPackage — что дерево знает о пакете-производителе.
type tokenCheckPackage struct {
	Dir      string
	Decls    []TokenCheckSite
	Namings  []CheckNaming
	Files    int
	Declared []tokenpolicy.Check
}

// TestMandatoryTokenChecksAreDeclaredOnceAndConsumed — сам гейт.
func TestMandatoryTokenChecksAreDeclaredOnceAndConsumed(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	read := func(rel string) ([]byte, bool) {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		return b, err == nil
	}

	producerSet := map[string]bool{}
	for k := range tokenVerifierProducers {
		producerSet[k] = true
	}

	var (
		parsed        int
		listDecls     []TokenCheckSite
		constants     = map[string]string{}
		constructions []VerifierConstruction
		calls         int
	)
	policyDir := strings.TrimPrefix(tokenCheckPolicyImport, tokenCheckModulePath+"/")

	for _, rel := range rels {
		src, ok := read(rel)
		if !ok {
			continue
		}
		parsed++

		decls, _, err := ScanCheckListDeclarations(rel, src, tokenCheckListName)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		listDecls = append(listDecls, decls...)

		if strings.HasPrefix(rel, policyDir+"/") {
			consts, err := ScanCheckConstants(rel, src, tokenCheckTypeName)
			if err != nil {
				t.Fatalf("разбор констант %s: %v", rel, err)
			}
			for k, v := range consts {
				constants[k] = v
			}
		}

		found, census, err := ScanVerifierConstructions(rel, src, producerSet)
		if err != nil {
			t.Fatalf("разбор построений %s: %v", rel, err)
		}
		calls += census.Calls
		constructions = append(constructions, found...)
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, вызовов осмотрено %d, "+
		"объявлений перечня найдено %d, констант перечня прочитано %d, построений "+
		"проверяющего найдено %d, записей словаря %d",
		parsed, calls, len(listDecls), len(constants), len(constructions), len(tokenVerifierProducers))

	if parsed < tokenCheckCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — "+
			"«ноль находок» на таком объёме означало бы «ноль прочитанного»",
			parsed, tokenCheckCensusFloor)
	}
	if len(constants) == 0 {
		t.Fatalf("не прочитано ни одной константы перечня из %s — сопоставить названный "+
			"состав с обязательным нечем, и молчание гейта сказано ни о чём", policyDir)
	}

	// (1) Перечень объявлен РОВНО ОДИН раз.
	if len(listDecls) != 1 {
		var where []string
		for _, d := range listDecls {
			where = append(where, fmt.Sprintf("%s:%d", d.File, d.Line))
		}
		sort.Strings(where)
		t.Fatalf("объявлений перечня %q в дереве %d, обязано быть 1: %s\n\n"+
			"Ноль означает, что предмета нет вовсе и сверять состав не с чем. Больше "+
			"одного — два места об одном предмете: они разойдутся на первой же правке, и "+
			"различие между ними не станет ничьей находкой, потому что не будет выражено "+
			"ни у одной поверхности.",
			tokenCheckListName, len(listDecls), strings.Join(where, ", "))
	}

	// (2) Каждая запись словаря обязана иметь предмет в дереве.
	seen := map[string]int{}
	for _, c := range constructions {
		seen[c.Producer]++
	}
	var stale []string
	for producer, spec := range tokenVerifierProducers {
		if seen[producer] == 0 {
			stale = append(stale, fmt.Sprintf("%s (%s)", producer, spec.Why))
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("записи словаря производителей, у которых в дереве нет ни одного построения — %d:\n  %s\n\n"+
			"Запись без предмета — это не порядок, а находка: она выглядит действующей, "+
			"и следующую слепую зону унаследует именно она. Исходов два: производитель "+
			"переехал (привести запись за ним) либо поверхность снята (снять запись).",
			len(stale), strings.Join(stale, "\n  "))
	}

	// (3) Состав, объявленный пакетом каждого производителя.
	packages := map[string]*tokenCheckPackage{}
	for producer := range tokenVerifierProducers {
		importPath := producer[:strings.LastIndex(producer, ".")]
		dir := strings.TrimPrefix(importPath, tokenCheckModulePath+"/")
		if _, ok := packages[dir]; ok {
			continue
		}
		pkg := &tokenCheckPackage{Dir: dir}
		for _, rel := range rels {
			if filepath.ToSlash(filepath.Dir(rel)) != dir {
				continue
			}
			src, ok := read(rel)
			if !ok {
				continue
			}
			pkg.Files++
			decls, namings, _, err := ScanCheckComposition(rel, src, tokenCheckPolicyImport, tokenCheckDeclName)
			if err != nil {
				t.Fatalf("разбор состава %s: %v", rel, err)
			}
			pkg.Decls = append(pkg.Decls, decls...)
			pkg.Namings = append(pkg.Namings, namings...)
		}
		unique := map[tokenpolicy.Check]bool{}
		for _, n := range pkg.Namings {
			if v, ok := constants[n.Ident]; ok {
				unique[tokenpolicy.Check(v)] = true
			}
		}
		for c := range unique {
			pkg.Declared = append(pkg.Declared, c)
		}
		sort.Slice(pkg.Declared, func(i, j int) bool { return pkg.Declared[i] < pkg.Declared[j] })
		packages[dir] = pkg
	}

	mandatory := map[tokenpolicy.Check]bool{}
	for _, c := range tokenpolicy.MandatoryChecks() {
		mandatory[c] = true
	}

	for producer, spec := range tokenVerifierProducers {
		importPath := producer[:strings.LastIndex(producer, ".")]
		dir := strings.TrimPrefix(importPath, tokenCheckModulePath+"/")
		pkg := packages[dir]
		if pkg.Files == 0 {
			t.Errorf("пакет производителя %s не дал ни одного не-тестового файла — "+
				"о его составе сказать нечего, и молчание про него не является утверждением", dir)
			continue
		}

		if !spec.InScope {
			// ПОСЛАБЛЕНИЕ САМОИСТЕКАЕТ: как только поверхность объявит свой
			// состав, запись становится находкой.
			if len(pkg.Decls) > 0 {
				t.Errorf("поверхность %s объявлена ВНЕ области, но её пакет уже объявляет "+
					"состав проверок (%s:%d) — послаблению больше нечего исключать.\n"+
					"Снятие: перевести запись словаря в область (InScope: true) и снять "+
					"поле Predicate. Запись, которой нечего исключать, унаследует следующая "+
					"слепая зона.", dir, pkg.Decls[0].File, pkg.Decls[0].Line)
				continue
			}
			t.Logf("вне области: %s — %s. Предикат втягивания: %s", dir, spec.Why, spec.Predicate)
			continue
		}

		if len(pkg.Decls) == 0 {
			t.Errorf("поверхность %s входит в область, но её пакет НЕ объявляет состав "+
				"проверок (искалась функция %s, возвращающая срез %s.%s; прочитано файлов %d).\n\n"+
				"Пока состав не объявлен, различие между поверхностями не выражено и покраснеть "+
				"не может ни у одной: одна перестанет требовать срок, другая тип, и спросить об "+
				"этом будет нечего.\n"+
				"Снятие: объявить состав рядом с реализацией и сверять его "+
				"`tokenpolicy.MissingChecks(...)` в пробе самого пакета.",
				dir, tokenCheckDeclName, filepath.Base(tokenCheckPolicyImport), tokenCheckTypeName, pkg.Files)
			continue
		}

		if missing := tokenpolicy.MissingChecks(pkg.Declared); len(missing) > 0 {
			var names []string
			for _, m := range missing {
				names = append(names, string(m))
			}
			t.Errorf("поверхность %s объявляет состав (%s:%d), но в нём НЕ ХВАТАЕТ обязательных "+
				"проверок — %d: %s\n"+
				"Объявлено: %v\n"+
				"Каждая недостающая — это признак, который поверхность не спрашивает у "+
				"предъявителя. На положительном пути её отсутствие невидимо: токен выпускается, "+
				"проверяется, запрос проходит.",
				dir, pkg.Decls[0].File, pkg.Decls[0].Line, len(missing),
				strings.Join(names, ", "), pkg.Declared)
			continue
		}

		// (4) Проверка СВЕРХ перечня обязана нести причину.
		var unreasoned []string
		for _, n := range pkg.Namings {
			val, ok := constants[n.Ident]
			if !ok {
				continue
			}
			if mandatory[tokenpolicy.Check(val)] || n.Reasoned {
				continue
			}
			unreasoned = append(unreasoned, fmt.Sprintf("%s:%d %s", n.File, n.Line, n.Ident))
		}
		sort.Strings(unreasoned)
		if len(unreasoned) > 0 {
			t.Errorf("поверхность %s объявляет проверки СВЕРХ обязательного перечня, не назвав "+
				"причины — %d:\n  %s\n\n"+
				"Расхождение поверхностей законно, пока оно объявлено: тогда следующий читатель "+
				"видит, чем эта поверхность отличается и почему. Молчаливое расхождение выглядит "+
				"как общий случай и переезжает копией на соседнюю поверхность.",
				dir, len(unreasoned), strings.Join(unreasoned, "\n  "))
			continue
		}

		t.Logf("в области: %s — %s; состав объявлен (%s:%d), проверок названо %d, "+
			"обязательных не хватает 0",
			dir, spec.Why, pkg.Decls[0].File, pkg.Decls[0].Line, len(pkg.Declared))
	}
}
