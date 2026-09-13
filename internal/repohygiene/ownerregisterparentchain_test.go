// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kaname/pkg/ownerregister"
)

// parentChainField — имя поля цепи предков. Названо один раз: по нему ищет гейт,
// и на него же ссылаются обе проверки предпосылки ниже.
const parentChainField = "ParentChain"

// Предпосылка гейта, проверяемая КОМПИЛЯТОРОМ, а не прозой.
//
// Запрет ниже обоснован тем, что поле цепи существует у обеих форм — у запроса на
// проводе и у общей формы доставки. Переименуй или сними любое из них — и гейт,
// ищущий поле по имени, нашёл бы ноль вхождений и промолчал бы, оставаясь
// зелёным. Здесь он вместо этого перестаёт собираться, то есть отказывает громко.
func init() {
	var onTheWire iamv1.RegisterResourceRequest
	_ = onTheWire.ParentChain
	var delivered ownerregister.Registration
	_ = delivered.ParentChain
}

// TestEveryRegisterResourceProducerCarriesParentChain — каждый производитель
// регистрации ресурса у владельца прав обязан НАЗВАТЬ цепь предков объекта.
//
// # Что ломается без этого свойства
//
// Принимающая сторона держит цепь предков объекта отдельными рёбрами и заменяет
// набор рёбер ЦЕЛИКОМ на каждой применённой регистрации: цепь — это состояние, а
// не приращение, и звено, которого владелец больше не называет, обязано исчезнуть,
// иначе право переживёт перенос объекта.
//
// У этой замены есть вторая сторона, и она и есть предмет гейта: производитель,
// который цепи не называет, на КАЖДОЙ перерегистрации своего объекта стирает уже
// записанные рёбра и не ставит новых. Дальше вопрос о доступе к такому объекту
// поднимается по цепи, цепи нет, ответ — «нет прав», и этот отказ НЕОТЛИЧИМ от
// честного: та же форма, тот же код, ни одного симптома. Наблюдать нечего именно
// потому, что сломанное молчит.
//
// # Почему гейт по дереву, а не проба у каждого производителя
//
// Свойство принадлежит КАЖДОМУ производителю, включая шестого, которого заведут
// завтра. Проба у одного закрепляет ответ этого одного. Ровно так расхождение и
// прожило: цепь слал один сервис из пяти, у остальных поле оставалось пустым, и
// ни один прогон не краснел.
//
// # Два измерения, и второе не выводится из первого
//
//	(A) НАЗВАНО ЛИ ПОЛЕ — на каждом литерале запроса и каждой строке доставки.
//	    Пропущенное поле — это молчаливое «предков нет» от того, у кого они есть.
//	(B) ПРОИЗВОДИТСЯ ЛИ ЦЕПЬ — у каждого сервиса-потребителя хотя бы одно место,
//	    где цепь СОБИРАЕТСЯ (вызов или непустой литерал), а не пробрасывается
//	    дальше полем чужой структуры. Без (B) весь сервис проходит (A), проставив
//	    везде проброс поля, которое никто никогда не заполняет, — форма без
//	    содержания.
//
// # Что делать, если гейт сработал — три исхода, четвёртого нет
//
//  1. это путь регистрации ⇒ назвать цепь предков объекта (эталон формы —
//     `ownerregister.ParentChain` модуля github.com/PRO-Robotech/kaname: явная
//     цепь владельца, иначе вывод из области, объявленной ЭТОЙ ЖЕ доставкой.
//     Пакет уехал в репозиторий службы доступа решением владельца kacho#2616,
//     исход C, 2026-09-13 — координаты `pkg/ownerregister` в ЭТОМ дереве больше
//     нет, и посылать читателя по ней значило бы посылать его в пустоту);
//  2. у объекта предка нет по построению ⇒ поле всё равно НАЗЫВАЕТСЯ, а пустым
//     его делает вычисление: «предков нет» обязано быть УТВЕРЖДЕНИЕМ владельца, а
//     не следствием того, что строку не дописали;
//  3. распознавание промахнулось ⇒ сузить предикат ниже, а не заводить список
//     прощённых: списка у этого гейта нет намеренно.
//
// Проверено инъекцией в обе стороны — см. ownerregisterparentchain_injection_test.go.
func TestEveryRegisterResourceProducerCarriesParentChain(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	var missingField []string
	scannedFiles := 0
	requestLiterals := 0
	deliveryLiterals := 0
	consumers := map[string]int{}
	producedIn := map[string]int{}

	forEachOwnerRegisterFile(t, root, func(rel string, fset *token.FileSet, file *ast.File) {
		scannedFiles++
		svc := registerConsumerOfPath(rel)
		local := ownerRegisterLocalName(file)

		for _, lit := range parentChainBearingLiterals(file, local) {
			switch lit.kind {
			case litRequest:
				requestLiterals++
				if svc != "" {
					consumers[svc]++
				}
			case litDelivery:
				deliveryLiterals++
			}
			if !lit.namesChain {
				missingField = append(missingField, rel+":"+
					strconv.Itoa(fset.Position(lit.pos).Line)+" ("+lit.kind+" без "+parentChainField+")")
			}
		}
		if svc != "" {
			producedIn[svc] += chainProductionSites(file)
		}
	})

	// «Ноль находок» обязано быть отличимо и от «ноль прочитанного», и от «ноль
	// распознанного»: сломанный обход и сгнивший предикат дают одинаково зелёный
	// гейт, если не утверждать объём осмотренного.
	if scannedFiles == 0 {
		t.Fatalf("гейт не прочитал ни одного прод-файла в %v — предпосылка обхода сломана, "+
			"молчание ничего не доказывает", ownerRegisterScanRoots)
	}
	if requestLiterals == 0 {
		t.Fatalf("гейт не нашёл НИ ОДНОГО литерала запроса регистрации в %d прод-файлах — "+
			"распознавание производителя сломано, молчание ничего не доказывает", scannedFiles)
	}
	if len(consumers) == 0 {
		t.Fatalf("гейт не отнёс ни один литерал запроса к сервису-потребителю "+
			"(литералов %d) — разбор пути сломан, измерение (B) вакуумно", requestLiterals)
	}
	if deliveryLiterals == 0 {
		t.Fatalf("гейт не нашёл НИ ОДНОЙ строки общей доставки в %d прод-файлах — "+
			"форма доставки распознаётся по ПАРЕ (пакет, тип), и пустое имя пакета "+
			"выключает половину измерения (A) МОЛЧА. Именно так переезд пакета в "+
			"модуль службы доступа (kacho#2616) оставил гейт зелёным: литерал пути "+
			"в ownerRegisterLocalName называл прежнее написание, не совпадал ни с "+
			"одним файлом, и ось доставки не читалась. Проверьте путь пакета в "+
			"ownerRegisterLocalName против оператора импорта в дереве", scannedFiles)
	}

	t.Logf("осмотрено: файлов %d, литералов запроса %d, строк доставки %d, "+
		"сервисов-потребителей %d %v, мест сборки цепи по сервисам %v",
		scannedFiles, requestLiterals, deliveryLiterals, len(consumers),
		sortedCountKeys(consumers), producedIn)

	var noProducer []string
	for svc := range consumers {
		if producedIn[svc] == 0 {
			noProducer = append(noProducer, svc)
		}
	}

	if len(missingField) > 0 {
		sort.Strings(missingField)
		t.Errorf("регистрация ресурса не называет цепь предков (%d):\n  %s\n\n"+
			"принимающая сторона заменяет набор рёбер объекта целиком, поэтому "+
			"неназванная цепь СТИРАЕТ уже записанных предков и не ставит новых; "+
			"дальше вопрос о доступе поднимается по цепи, цепи нет, и отказ "+
			"неотличим от честного. Эталон формы — ownerregister.ParentChain "+
			"модуля github.com/PRO-Robotech/kaname.",
			len(missingField), strings.Join(missingField, "\n  "))
	}
	if len(noProducer) > 0 {
		sort.Strings(noProducer)
		t.Errorf("сервис-потребитель регистрации не СОБИРАЕТ цепь предков ни в одном "+
			"месте (%d): %s\n\n"+
			"поле, которое только пробрасывается дальше и нигде не заполняется, "+
			"проходит проверку (A) и не несёт ни одного предка. Цепь обязана "+
			"вычисляться в сервисе-владельце: только он знает свою иерархию.",
			len(noProducer), strings.Join(noProducer, ", "))
	}
}

// ── распознавание ──────────────────────────────────────────────────────────

const (
	litRequest  = "запрос регистрации"
	litDelivery = "строка доставки"
)

type chainLit struct {
	kind       string
	namesChain bool
	pos        token.Pos
}

// ownerRegisterLocalName — под каким именем файл видит пакет общей доставки.
// Читается из импортов, а не предполагается: предикат по слову «ownerregister»
// разошёлся бы с деревом на первом же алиасе.
//
// ПУТЬ ПАКЕТА ПЕРЕЕХАЛ В ЧУЖОЙ МОДУЛЬ, и переезд этот литерал не задел. Решением
// владельца kacho#2616 (исход C, 2026-09-13) пакет общей доставки уехал в
// репозиторий службы доступа: дерево импортирует
// `github.com/PRO-Robotech/kaname/pkg/ownerregister` — 35 файлов, — а здесь
// стояло прежнее написание с именем платформы. Правка импортов до литерала не
// доходит by construction: она трогает операторы импорта, а не строки внутри
// тела функции, — и именно поэтому этот файл собирался и был зелёным, ища путь,
// которого в дереве нет.
//
// ЦЕНА ЭТОГО РАСХОЖДЕНИЯ — МОЛЧАНИЕ, А НЕ ОТКАЗ: ни один файл не совпадал, имя
// пакета возвращалось пустым, форма доставки не распознавалась ни разу, и ось
// строк доставки выпала из наблюдения целиком. Требование ниже (строк доставки
// ноль — отказ) заведено тем же изменением: без него следующее такое расхождение
// снова прошло бы зелёным.
func ownerRegisterLocalName(file *ast.File) string {
	const path = `"github.com/PRO-Robotech/kaname/pkg/ownerregister"`
	for _, imp := range file.Imports {
		if imp.Path == nil || imp.Path.Value != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "ownerregister"
	}
	return ""
}

// parentChainBearingLiterals — литералы, обязанные называть цепь: запрос на
// проводе и строка общей доставки.
//
// Запрос распознаётся по ИМЕНИ ТИПА, а не по имени пакета-алиаса: алиас у каждого
// сервиса свой (iamv1, iampb), и предикат по алиасу разошёлся бы с деревом при
// первом же новом импорте. Строка доставки — наоборот, по ПАРЕ (пакет, тип):
// «Registration» само по себе слишком общее слово, чтобы по нему судить.
//
// Форма снятия регистрации сюда НЕ входит и входить не должна: снятие адресуется
// объектом, цепь предков ему не нужна, и требовать её значило бы запрещать то,
// ради чего гейт не писался.
func parentChainBearingLiterals(file *ast.File, ownerRegisterLocal string) []chainLit {
	var out []chainLit
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := literalTypeSelector(lit)
		if !ok {
			return true
		}
		kind := ""
		switch {
		case sel.Sel.Name == "RegisterResourceRequest":
			kind = litRequest
		case sel.Sel.Name == "Registration" && ownerRegisterLocal != "":
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == ownerRegisterLocal {
				kind = litDelivery
			}
		}
		if kind == "" {
			return true
		}
		for _, one := range literalsOfKind(lit) {
			// Литерал, не называющий НИ ОДНОГО поля, — это нулевое значение, а не
			// производитель: так возвращают «ничего» рядом с ошибкой. Требовать от
			// него цепь значило бы требовать поле у пустоты, и первый же такой
			// ложный срабат снял бы гейт целиком.
			if len(one.Elts) == 0 {
				continue
			}
			out = append(out, chainLit{kind: kind, namesChain: litNamesChain(one), pos: one.Lbrace})
		}
		return true
	})
	return out
}

// literalTypeSelector — имя типа литерала, включая ЭЛЕМЕНТ среза с опущенным
// типом (`[]ownerregister.Registration{{…}}`).
//
// Форма с опущенным типом — не редкость и не экзотика: именно так строят строку
// доставки подставные репозитории двух сервисов. Пропусти её гейт — и дублёр
// оказался бы СНИСХОДИТЕЛЬНЕЕ продукта ровно в том, ради чего его подставляют:
// проба use-case зеленела бы на доставке без предков, которой в проде не бывает.
func literalTypeSelector(lit *ast.CompositeLit) (*ast.SelectorExpr, bool) {
	switch typ := lit.Type.(type) {
	case *ast.SelectorExpr:
		return typ, true
	case *ast.ArrayType:
		if sel, ok := typ.Elt.(*ast.SelectorExpr); ok {
			return sel, true
		}
	}
	return nil, false
}

// literalsOfKind — сами литералы значения: либо один (именованный тип), либо
// элементы среза с опущенным типом.
func literalsOfKind(lit *ast.CompositeLit) []*ast.CompositeLit {
	if _, isSlice := lit.Type.(*ast.ArrayType); !isSlice {
		return []*ast.CompositeLit{lit}
	}
	var out []*ast.CompositeLit
	for _, elt := range lit.Elts {
		if inner, ok := elt.(*ast.CompositeLit); ok {
			out = append(out, inner)
		}
	}
	return out
}

// litNamesChain — назван ли в литерале ключ цепи со значением, отличным от
// голого nil.
//
// Голый nil — это не «предков нет», это «строку не дописали»: у владельца, чей
// объект лежит под проектом, отсутствие предков есть утверждение, которое обязано
// быть ВЫЧИСЛЕНО, а не набрано константой.
func litNamesChain(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != parentChainField {
			continue
		}
		if id, isIdent := kv.Value.(*ast.Ident); isIdent && id.Name == "nil" {
			return false
		}
		return true
	}
	return false
}

// chainProductionSites — сколько раз в файле цепь СОБИРАЕТСЯ, а не передаётся
// дальше.
//
// Сборкой считается непустой литерал среза (цепь выписана явно) либо вызов
// С АРГУМЕНТАМИ (цепь вычислена из чего-то). Два вида передачи сборкой НЕ
// считаются, и оба встречаются в дереве:
//
//   - проброс поля чужой структуры (`intent.ParentChain`) — по нему измерение (A)
//     удовлетворялось бы вакуумно у сервиса, который цепи не заполняет нигде;
//   - вызов БЕЗ аргументов (`in.GetParentChain()`) — читатель поля, а не
//     вычислитель: он может лишь отдать то, что решил кто-то другой. Именно так
//     принимающая сторона и читает пришедшую цепь, и засчитывать ей производство
//     значило бы считать сборкой любое чтение.
func chainProductionSites(file *ast.File) int {
	n := 0
	count := func(value ast.Expr) {
		switch v := value.(type) {
		case *ast.CallExpr:
			if len(v.Args) > 0 {
				n++
			}
		case *ast.CompositeLit:
			if len(v.Elts) > 0 {
				n++
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.KeyValueExpr:
			if key, ok := stmt.Key.(*ast.Ident); ok && key.Name == parentChainField {
				count(stmt.Value)
			}
		case *ast.AssignStmt:
			for i, lhs := range stmt.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != parentChainField || i >= len(stmt.Rhs) {
					continue
				}
				count(stmt.Rhs[i])
			}
		}
		return true
	})
	return n
}

// registerConsumerOfPath — имя сервиса из относительного пути, либо "" вне services/.
// Общий пакет доставки (`ownerregister` модуля github.com/PRO-Robotech/kaname)
// сервисом не является: он ничего не знает о чужой иерархии и обязан лишь
// передать названное вызывающим. В этом дереве его исходников нет вовсе
// (kacho#2616, исход C), поэтому под `services/` он не попадает by construction.
func registerConsumerOfPath(rel string) string {
	parts := strings.Split(rel, string('/'))
	if len(parts) < 2 || parts[0] != "services" {
		return ""
	}
	return parts[1]
}

func sortedCountKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
