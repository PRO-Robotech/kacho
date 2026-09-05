// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// ownerRegisterScanRoots — где ищем производителей регистрации у владельца прав.
var ownerRegisterScanRoots = []string{"services", "pkg"}

// deliveryClockCalls — вызовы, читающие часы В МОМЕНТ ДОСТАВКИ. Именно они
// запрещены в функции, которая собирает запрос регистрации.
//
// Ключ — вызов как он пишется, значение — почему он здесь и что бы сломалось,
// будь он законен. Словарь читают обе стороны: гейт — чтобы искать, и проба
// TestDeliveryClockDictionaryHasSubject — чтобы доказать, что каждое имя в нём
// вообще существует как вызов в дереве (иначе словарь сгниёт молча и «ноль
// находок» станет неотличимо от «ноль распознанного»).
var deliveryClockCalls = map[string]string{
	"time.Now":        "часы процесса в момент доставки",
	"timestamppb.Now": "то же самое, обёрнутое в proto-тип",
}

// TestOwnerRegistrationCarriesWriterTxVersion — маркер версии, который несёт
// регистрация ресурса у владельца прав, обязан приходить ИЗ WRITER-ТРАНЗАКЦИИ,
// а не читаться с часов в момент доставки.
//
// # Что это за свойство и почему оно не косметика
//
// Каждая регистрация доезжает до принимающей стороны ДВАЖДЫ: синхронным
// вызовом после коммита и повтором того же durable-намерения из очереди.
// Принимающая сторона гасит повторную доставку СТРОГИМ монотонным сравнением
// версий, и от того, откуда взялась версия, зависит, сработает ли гашение:
//
//   - версия проштампована БД ВНУТРИ writer-транзакции ⇒ обе доставки несут
//     ОДНО значение ⇒ вторая не меняет ни строки ⇒ гашение срабатывает
//     НЕЗАВИСИМО от того, какая пришла первой;
//   - версия прочитана с часов в момент доставки ⇒ синхронный вызов строго
//     новее ⇒ когда очередь успевает первой, синхронный вызов выглядит новее
//     состоянием и заставляет пересчитывать материализацию заново — на самом
//     горячем пути создания ресурса.
//
// То есть часы делают выбор входа материализации зависимым от ГОНКИ, а гонка в
// аварии складывается против нас: чем хуже дела у принимающей стороны, тем чаще
// очередь приходит первой и тем дороже обходится каждое создание.
//
// # Почему гейт по дереву, а не проба у каждого регистратора
//
// Свойство «версия не выдумана здесь» — свойство КАЖДОГО производителя запроса,
// включая тех, которых ещё не написали. Проба у одного регистратора закрепляет
// ответ этого регистратора и молчит про шестого, которого заведут завтра.
// Именно так расхождение и прожило незамеченным: пять форм, и у каждой свой
// комментарий, объясняющий, почему она такая.
//
// # Что делать, если гейт сработал — три исхода, четвёртого нет
//
//  1. это путь регистрации ⇒ протащить в него штамп, который БД поставила
//     намерению внутри writer-транзакции, и передать его параметром (эталон
//     формы — pkg/ownerregister.Registration.SourceVersion);
//  2. функция собирает запрос регистрации, но часы ей нужны для чего-то ДРУГОГО
//     (не для версии) ⇒ вынести сборку запроса в отдельную функцию, а не
//     заводить список исключений: списка у этого гейта нет намеренно;
//  3. распознавание промахнулось ⇒ сузить предикат ниже.
//
// Проверено инъекцией в обе стороны (см. ownerregisterversion_injection_test.go):
// возврат часов в путь регистрации красит гейт и печатает координату, а
// законный близнец той же формы — сборка того же запроса с версией из параметра
// — его не задевает.
func TestOwnerRegistrationCarriesWriterTxVersion(t *testing.T) {
	root := repoRoot(t)

	var hits []string
	scannedFiles := 0
	requestLiterals := 0
	producerFuncs := 0

	forEachOwnerRegisterFile(t, root, func(rel string, fset *token.FileSet, file *ast.File) {
		scannedFiles++
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			lits := countRegisterRequestLiterals(fn)
			if lits == 0 {
				continue
			}
			requestLiterals += lits
			producerFuncs++
			for _, c := range deliveryClocksIn(fn) {
				hits = append(hits, rel+":"+strconv.Itoa(fset.Position(c.pos).Line)+
					" ("+funcName(fn)+" читает "+c.call+" — "+deliveryClockCalls[c.call]+")")
			}
		}
	})

	// «Ноль находок» обязано быть отличимо и от «ноль прочитанного», и от «ноль
	// распознанного»: сломанный обход и сгнивший предикат дают одинаково зелёный
	// гейт, если не утверждать объём осмотренного.
	if scannedFiles == 0 {
		t.Fatalf("гейт не прочитал ни одного прод-файла в %v — предпосылка обхода сломана, "+
			"молчание ничего не доказывает", ownerRegisterScanRoots)
	}
	if producerFuncs == 0 {
		t.Fatalf("гейт не нашёл НИ ОДНОЙ функции, собирающей RegisterResourceRequest, "+
			"в %d прод-файлах — распознавание производителя сломано, молчание ничего "+
			"не доказывает", scannedFiles)
	}

	t.Logf("осмотрено: файлов %d, функций-производителей запроса %d, литералов запроса %d",
		scannedFiles, producerFuncs, requestLiterals)

	if len(hits) > 0 {
		sort.Strings(hits)
		t.Fatalf("маркер версии регистрации выведен из часов в момент доставки (%d):\n  %s\n\n"+
			"версию обязана нести writer-транзакция: только тогда обе доставки одной "+
			"регистрации несут ОДНО значение и повторная гасится независимо от того, "+
			"какая пришла первой. См. pkg/ownerregister.",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// TestDeliveryClockDictionaryHasSubject — у каждого имени словаря есть предмет в
// дереве. Проверка СВОЕЙ предпосылки: запрет обоснован тем, что такие вызовы
// вообще пишут; если имя перестало встречаться где бы то ни было, словарь гниёт,
// и «ноль находок» перестаёт что-либо значить.
//
// Считаем по ВСЕМУ дереву (не только по путям регистрации): предмет словаря —
// «так читают часы в Go», а не «так их читают здесь».
func TestDeliveryClockDictionaryHasSubject(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]int{}
	files := 0

	walkOwnerRegisterGoFiles(t, root, ownerRegisterScanRoots, func(_ string, body []byte) {
		files++
		for call := range deliveryClockCalls {
			seen[call] += strings.Count(string(body), call+"(")
		}
	})

	if files == 0 {
		t.Fatalf("перепись не прочитала ни одного файла — предпосылка сломана")
	}
	for call, why := range deliveryClockCalls {
		if seen[call] == 0 {
			t.Fatalf("словарь называет %q (%s), но в %d файлах дерева такого вызова НЕТ — "+
				"запись, которой больше нечего распознавать, есть находка: она унаследует "+
				"следующую слепую зону", call, why, files)
		}
	}
	t.Logf("предпосылка словаря: прочитано файлов %d, вхождений %v", files, seen)
}

// ── распознавание ──────────────────────────────────────────────────────────

type clockHit struct {
	call string
	pos  token.Pos
}

// countRegisterRequestLiterals — сколько раз функция собирает запрос регистрации
// ресурса. Распознаётся по ИМЕНИ ТИПА композитного литерала, а не по имени
// пакета-алиаса: алиас у каждого сервиса свой (`iamv1`, `iampb`), и предикат по
// алиасу разошёлся бы с деревом при первом же новом импорте.
func countRegisterRequestLiterals(fn *ast.FuncDecl) int {
	n := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "RegisterResourceRequest" {
			n++
		}
		return true
	})
	return n
}

// deliveryClocksIn — вызовы часов внутри тела функции.
func deliveryClocksIn(fn *ast.FuncDecl) []clockHit {
	var out []clockHit
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		name := pkg.Name + "." + sel.Sel.Name
		if _, banned := deliveryClockCalls[name]; banned {
			out = append(out, clockHit{call: name, pos: call.Lparen})
		}
		return true
	})
	return out
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		return "метод " + fn.Name.Name
	}
	return fn.Name.Name
}

// forEachOwnerRegisterFile — обход прод-файлов с разбором AST.
func forEachOwnerRegisterFile(t *testing.T, root string, visit func(rel string, fset *token.FileSet, file *ast.File)) {
	t.Helper()
	walkOwnerRegisterGoFiles(t, root, ownerRegisterScanRoots, func(rel string, body []byte) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		visit(rel, fset, file)
	})
}

// walkOwnerRegisterGoFiles — прод-файлы (.go, не _test.go) под указанными
// корнями.
//
// Состав берётся у ИНДЕКСА git (pkg/treecorpus), а не обходом диска.
// Разница не в стиле: правила игнорирования действуют на любой глубине, и под
// services/ на всякой машине, где поднимали стенд или собирали фронтенд, лежат
// распаковки чартов, сборочные каталоги и отчёты прогонов. Обход диска
// прочитал бы их и дал бы находки, зависящие от содержимого чужого рабочего
// каталога. Первая редакция этой функции ходила по диску — её поймал tree-wide
// гейт TestTreeWalkersAskTheIndex, здесь и сработавший по назначению.
func walkOwnerRegisterGoFiles(t *testing.T, root string, roots []string, visit func(rel string, body []byte)) {
	t.Helper()
	for _, sub := range roots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		files, err := treecorpus.UnderWithSuffix(base, ".go")
		if err != nil {
			t.Fatalf("состав дерева под %s: %v", base, err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			body, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("чтение %s: %v", path, rerr)
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				t.Fatalf("относительный путь %s: %v", path, rerr)
			}
			visit(rel, body)
		}
	}
}
