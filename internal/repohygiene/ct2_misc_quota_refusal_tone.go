// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_misc_quota_refusal_tone.go — тон отказа по пределу один на всех владельцев
// учёта, и держится он ВЫВОДОМ префикса из sentinel'а, а не перечнем префиксов.
//
// ПРЕДМЕТ (задача продукта #1658). Отказ по исчерпанию предела производит ОДИН
// производитель на платформу (`pkg/quota/refusal.sql.tmpl`): он называет
// носителя, предел и вид одним предложением, и это предложение — контракт
// (`api-conventions.md` §Error-format: тексты меняются только осознанно). Мост
// SQLSTATE→sentinel у каждого владельца свой и оборачивает предложение
// sentinel'ом своего домена; мапперу наружу положено снять префикс, чтобы
// клиент увидел предложение и ничего сверх него.
//
// ЧТО БЫЛО НЕВЕРНО. Один владелец снимал префикс по ЗАКРЫТОМУ ПЕРЕЧНЮ префиксов,
// выписанных строковыми литералами. Перечень — ВТОРОЕ место о sentinel'ах
// домена, и оно разошлось с первым молча: две полосы учёта завелись позже, в
// перечень не попали, и отказ уезжал клиенту с приклеенным именем внутреннего
// sentinel'а — при том что пять остальных владельцев отдавали то же предложение
// чистым. Клиент, ключующийся на текст, читал два разных продукта, и увидеть
// это можно было только положив шесть текстов рядом, чего не делает ни обзор
// изменения, ни сборка.
//
// ЧТО ГЕЙТ ДЕРЖИТ. Не «сегодня тексты совпали» — это свойство ПРОГОНА, и по
// дереву оно недоказуемо (пакеты владельцев лежат под `services/<svc>/internal`
// и вне своего сервиса не импортируются by construction, поэтому свести шесть
// мапперов в одну пробу нечем). Гейт держит СТРУКТУРНЫЕ причины совпадения, и
// осей у него ТРИ — каждая закрывает свой способ разойтись молча:
//
//  1. ПРЕФИКС ВЫВОДИТСЯ из sentinel'а (`<err>.Error()`), а не выписан строкой.
//     Тогда седьмой sentinel добавить в обход нечего — снимается ровно тот
//     префикс, который назвал распознавший вызывающий.
//  2. ПУСТОЙ ОСТАТОК ОГРАЖДЁН. Снятие устроено как «отрезать префикс и отдать
//     остаток»; на обёртке без текста остаток пуст, и клиент получает КОД БЕЗ
//     ЕДИНОГО СЛОВА о том, что делать дальше — в журнале это неотличимо от
//     потери сообщения. Ось нужна отдельно от первой: вывод префикса из
//     sentinel'а этот случай не закрывает, он его ПОРОЖДАЕТ.
//  3. СЛОВАРЬ SENTINEL'ОВ ОДИН. Текст sentinel'а — то, чем снимается префикс,
//     поэтому расхождение делает шесть мапперов несравнимыми. Клиенту оно не
//     видно (префикс снимается), и потому тихо: обзор изменения его не поймает.
//
// ПОЧЕМУ ЭТО НЕ СУЖЕНИЕ ДО ОДНОЙ ФОРМЫ ЗАПИСИ. Законных форм в дереве две, и обе
// выводят префикс: «сними префикс НАЗВАННОГО sentinel'а» (пять владельцев) и
// «сними префикс любого из перечня SENTINEL'ОВ» (шестой, перечень `[]error`).
// Находкой является только перечень ПРЕФИКСОВ-СТРОК: у него нет связи с
// sentinel'ами вовсе, поэтому расходится он молча.
//
// ПОЧЕМУ РАЗБОР СИНТАКСИСА, А НЕ ПОИСК ПО ТЕКСТУ. Имена и строки, которые здесь
// ищутся, стоят и в комментариях — в том числе в этой самой шапке. Проверка по
// подстроке краснела бы на собственном объяснении и зеленела бы на
// закомментированном коде (`testing.md` §«Гейт на класс», п.4).
//
// ЧТО ОБХОД НЕ ВИДИТ — названо, а не спрятано:
//
//  1. ПРАВДИВОСТЬ текста — что предложение производителя доехало дословно, и
//     что ограждение пустого остатка ВЕРНО, а не просто присутствует. Это
//     свойства ВЫЗОВА, и держат их пробы владельцев: `empty_refusal_test.go`
//     рядом с каждым стриппером (пара «обычная обёртка доезжает дословно» ·
//     «обёртка без текста не даёт отказа без сообщения») и
//     `quota_metadata_test.go`. Ось 2 ниже проверяет НАЛИЧИЕ ограждения, а не
//     его правильность, — и это сказано, а не подразумевается.
//  2. КОСВЕННЫЙ вызов стриппера — значение функции, положенное в переменную или
//     поле: разбор судит вызов по имени, а не по потоку значений. Такой владелец
//     попадёт в находку «стриппер не разрешён», а не в молчание.
//  3. Стриппер, объявленный в ЧУЖОМ модуле. В дереве таких нет: `pkg/` общего
//     стриппера не несёт, и каждый владелец снимает префикс у себя.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

const (
	// ct2QuotaExceededReason — признак полосы учёта на пути наружу. Им
	// опознаётся маппер ИМЕННО этого отказа, а не любой другой `ErrorInfo`.
	ct2QuotaExceededReason = "QUOTA_EXCEEDED"
	// ct2StatusNewFunc — то, чем собирается статус отказа.
	ct2StatusNewFunc = "New"
	// ct2GRPCStatusPkg — дом `status.New`.
	ct2GRPCStatusPkg = "google.golang.org/grpc/status"
	// ct2ErrorMethod — метод, из которого выводится снимаемый префикс.
	ct2ErrorMethod = "Error"
	// ct2SentinelSeparator — разделитель, которым sentinel приклеен к тексту.
	ct2SentinelSeparator = ": "
	// ct2CutPrefixFunc — срезка префикса; её первое значение и есть остаток,
	// который уезжает клиенту.
	ct2CutPrefixFunc = "CutPrefix"
	// ct2QuotaExceededSentinel — общий словарь шести владельцев учёта.
	ct2QuotaExceededSentinel = "resource count quota exceeded"
	// ct2QuotaNotProvisionedSentinel — вторая половина того же словаря.
	ct2QuotaNotProvisionedSentinel = "resource count quota not provisioned"
	// ct2QuotaExceededVar / ct2QuotaNotProvisionedVar — имена, под которыми
	// словарь объявлен у каждого владельца.
	ct2QuotaExceededVar       = "ErrQuotaExceeded"
	ct2QuotaNotProvisionedVar = "ErrQuotaNotProvisioned"
)

// ct2ToneOwnerFacts — что найдено в прод-коде ОДНОГО владельца учёта.
//
// Хранятся координаты, а не флаги: находка обязана называть файл, иначе
// читателю остаётся искать самому (`testing.md` §«Гейт на класс», п.8 —
// диагностика есть часть свойства).
type ct2ToneOwnerFacts struct {
	// outwardFile — файл, собирающий статус отказа полосы учёта.
	outwardFile string
	// stripperName — имя функции, производящей клиентский текст.
	stripperName string
	// stripperFile — где эта функция объявлена.
	stripperFile string
	// derivesPrefix — префикс выводится из `<err>.Error()`.
	derivesPrefix bool
	// literalPrefixFile — где найден перечень префиксов-СТРОК.
	literalPrefixFile string
	// literalPrefix — первый такой префикс, для диагностики.
	literalPrefix string
	// guardsEmptyRemainder — стриппер ограждает пустой остаток.
	guardsEmptyRemainder bool
	// sentinelFile — где объявлен словарь sentinel'ов учёта.
	sentinelFile string
	// sentinelTexts — «имя переменной» → объявленный текст.
	sentinelTexts map[string]string
}

// ct2ToneCensus — перепись обхода. Печатается ВСЕГДА: «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
type ct2ToneCensus struct {
	Owners     []string
	Files      int
	Parsed     int
	Facts      map[string]*ct2ToneOwnerFacts
	Outward    int
	Resolved   int
	Conforming int
	Guarding   int
	Vocabulary int
}

// collectQuotaRefusalTone обходит прод-код названных владельцев.
//
// Состав дерева берётся у ИНДЕКСА git, а не у диска: под `services/` на машине,
// где поднимали стенд, лежат распаковки чартов и отчёты прогонов, и вердикт,
// собранный обходом файловой системы, стал бы свойством рабочего каталога, а не
// коммита.
func collectQuotaRefusalTone(tree *treecorpus.Tree, owners []string) (ct2ToneCensus, error) {
	c := ct2ToneCensus{
		Owners: append([]string(nil), owners...),
		Facts:  make(map[string]*ct2ToneOwnerFacts, len(owners)),
	}
	for _, o := range c.Owners {
		c.Facts[o] = &ct2ToneOwnerFacts{}
	}

	// Разбор идёт в ДВА прохода: сперва находится маппер наружу и имя стриппера,
	// затем — объявление этой функции. Один проход не годится: объявление
	// стриппера обычно лежит в другом файле, и порядок обхода этого не решает.
	type parsedFile struct {
		rel   string
		owner string
		file  *ast.File
	}
	var parsed []parsedFile

	for _, rel := range tree.SortedFiles() {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		owner := ct2ToneOwnerOfPath(rel, c.Owners)
		if owner == "" {
			continue
		}
		c.Files++

		src, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			return c, fmt.Errorf("чтение %s: %w", rel, err)
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, rel, src, 0)
		if perr != nil {
			// Неразбираемый файл пропускается молча только здесь: дерево
			// собирается, значит такого файла в нём нет, а синтетика инъекции
			// свои файлы пишет валидными.
			continue
		}
		c.Parsed++
		parsed = append(parsed, parsedFile{rel: rel, owner: owner, file: f})
	}

	// ПРОХОД 1 — маппер наружу и имя функции, производящей клиентский текст.
	for _, pf := range parsed {
		facts := c.Facts[pf.owner]
		if facts.outwardFile != "" {
			continue
		}
		imports := ct2ToneImportAliases(pf.file)
		name, found := ct2ToneOutwardStripper(pf.file, imports)
		if !found {
			continue
		}
		facts.outwardFile = pf.rel
		facts.stripperName = name
	}

	// ПРОХОД 2 — объявление стриппера и то, откуда он берёт префикс.
	for _, pf := range parsed {
		facts := c.Facts[pf.owner]
		if facts.stripperName == "" || facts.stripperFile != "" {
			continue
		}
		decl := ct2ToneFuncDecl(pf.file, facts.stripperName)
		if decl == nil {
			continue
		}
		facts.stripperFile = pf.rel
		facts.derivesPrefix = ct2ToneDerivesPrefix(decl)
		facts.guardsEmptyRemainder = ct2ToneGuardsEmptyRemainder(decl)
		if lit, ok := ct2ToneLiteralPrefixList(decl); ok {
			facts.literalPrefixFile = pf.rel
			facts.literalPrefix = lit
		}
	}

	// ПРОХОД 3 — словарь sentinel'ов учёта: где объявлен и каким текстом.
	for _, pf := range parsed {
		facts := c.Facts[pf.owner]
		if facts.sentinelFile != "" {
			continue
		}
		texts := ct2ToneSentinelTexts(pf.file)
		if len(texts) == 0 {
			continue
		}
		facts.sentinelFile = pf.rel
		facts.sentinelTexts = texts
	}

	for _, o := range c.Owners {
		f := c.Facts[o]
		if f.outwardFile != "" {
			c.Outward++
		}
		if f.stripperFile != "" && f.guardsEmptyRemainder {
			c.Guarding++
		}
		if ct2ToneVocabularyAgrees(f) {
			c.Vocabulary++
		}
		if f.stripperFile != "" {
			c.Resolved++
		}
		if f.stripperFile != "" && f.derivesPrefix && f.literalPrefixFile == "" {
			c.Conforming++
		}
	}
	return c, nil
}

// ct2ToneOwnerOfPath отвечает, чей это прод-файл; "" — ничей из названных.
func ct2ToneOwnerOfPath(rel string, owners []string) string {
	for _, o := range owners {
		if strings.HasPrefix(rel, "services/"+o+"/internal/") {
			return o
		}
	}
	return ""
}

// ct2ToneImportAliases — «имя, под которым пакет виден в файле» → путь импорта.
//
// Приведение к ПОЛНОМУ пути, а не к имени пакета: имя задаёт вызывающий, и
// одноимённый чужой помощник иначе стал бы нашим.
func ct2ToneImportAliases(f *ast.File) map[string]string {
	out := make(map[string]string, len(f.Imports))
	for _, im := range f.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			continue
		}
		name := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			name = p[i+1:]
		}
		if im.Name != nil {
			if im.Name.Name == "." || im.Name.Name == "_" {
				continue
			}
			name = im.Name.Name
		}
		out[name] = p
	}
	return out
}

// ct2ToneOutwardStripper — имя функции, производящей клиентский текст отказа
// учёта в этом файле.
//
// Файл засчитывается ТОЛЬКО если в нём есть признак полосы учёта: у владельца
// есть и другие `status.New`, и требовать вывода префикса от них значило бы
// краснеть на исправном коде.
func ct2ToneOutwardStripper(f *ast.File, imports map[string]string) (string, bool) {
	if !ct2ToneHasReason(f) {
		return "", false
	}
	var name string
	var found bool
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !ct2ToneCallIs(call, imports, ct2GRPCStatusPkg, ct2StatusNewFunc) {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		// Второй аргумент `status.New` — то, что увидит клиент.
		inner, isCall := call.Args[1].(*ast.CallExpr)
		if !isCall {
			// Текст собран НА МЕСТЕ, без вызова. Стриппером тогда считается сама
			// объемлющая функция — её и разбирает проход 2 по имени.
			return true
		}
		switch fn := inner.Fun.(type) {
		case *ast.Ident:
			name, found = fn.Name, true
		case *ast.SelectorExpr:
			name, found = fn.Sel.Name, true
		}
		return true
	})
	return name, found
}

// ct2ToneHasReason — в файле стоит строковый литерал признака полосы учёта.
//
// Литерал В КОДЕ: комментарии в `*ast.BasicLit` не попадают by construction —
// ради этого и взят разбор синтаксиса.
func ct2ToneHasReason(f *ast.File) bool {
	var ok bool
	ast.Inspect(f, func(n ast.Node) bool {
		if ok {
			return false
		}
		lit, isLit := n.(*ast.BasicLit)
		if !isLit || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err == nil && s == ct2QuotaExceededReason {
			ok = true
		}
		return true
	})
	return ok
}

// ct2ToneFuncDecl — объявление функции (или метода) с этим именем в файле.
func ct2ToneFuncDecl(f *ast.File, name string) *ast.FuncDecl {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name != nil && fn.Name.Name == name && fn.Body != nil {
			return fn
		}
	}
	return nil
}

// ct2ToneDerivesPrefix — снимаемый префикс выводится из `<err>.Error()`.
//
// Признаётся ОБЕ законные записи, и это не щедрость, а требование п.7
// §«Гейт на класс»: форма, о которой распознаватель не знает, попадает не в
// нарушители, а ВНЕ наблюдения.
//
//	prefix := sentinel.Error() + ": "        — склейка на месте;
//	fallback = sentinel.Error(); … fallback+": " — через переменную.
func ct2ToneDerivesPrefix(fn *ast.FuncDecl) bool {
	// Переменные, которым присвоено значение выражения с `.Error()`.
	derived := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range as.Rhs {
			if i >= len(as.Lhs) || !ct2ToneContainsErrorCall(rhs) {
				continue
			}
			if id, isID := as.Lhs[i].(*ast.Ident); isID {
				derived[id.Name] = true
			}
		}
		return true
	})

	var ok bool
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if ok {
			return false
		}
		bin, isBin := n.(*ast.BinaryExpr)
		if !isBin || bin.Op != token.ADD {
			return true
		}
		if !ct2ToneIsSeparatorLiteral(bin.Y) && !ct2ToneIsSeparatorLiteral(bin.X) {
			return true
		}
		for _, side := range []ast.Expr{bin.X, bin.Y} {
			if ct2ToneContainsErrorCall(side) {
				ok = true
				return false
			}
			if id, isID := side.(*ast.Ident); isID && derived[id.Name] {
				ok = true
				return false
			}
		}
		return true
	})
	return ok
}

// ct2ToneContainsErrorCall — выражение зовёт метод `Error()`.
func ct2ToneContainsErrorCall(e ast.Expr) bool {
	var ok bool
	ast.Inspect(e, func(n ast.Node) bool {
		if ok {
			return false
		}
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == ct2ErrorMethod {
			ok = true
		}
		return true
	})
	return ok
}

// ct2ToneIsSeparatorLiteral — строковый литерал-разделитель sentinel'а.
func ct2ToneIsSeparatorLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == ct2SentinelSeparator
}

// ct2ToneLiteralPrefixList — перечень префиксов, выписанных СТРОКАМИ.
//
// Это и есть находка: у такого перечня нет связи с sentinel'ами домена, поэтому
// расходится он молча. Перечень `[]error` находкой НЕ является — из его
// элементов префикс всё равно выводится вызовом `Error()`.
func ct2ToneLiteralPrefixList(fn *ast.FuncDecl) (string, bool) {
	var found string
	var ok bool
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if ok {
			return false
		}
		lit, isLit := n.(*ast.BasicLit)
		if !isLit || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil || s == ct2SentinelSeparator || !strings.HasSuffix(s, ct2SentinelSeparator) {
			return true
		}
		found, ok = s, true
		return false
	})
	return found, ok
}

// quotaRefusalToneFindings — расхождения, каждое с координатой.
func quotaRefusalToneFindings(c ct2ToneCensus) []string {
	var out []string
	for _, o := range c.Owners {
		f := c.Facts[o]
		if f.outwardFile == "" {
			out = append(out, fmt.Sprintf(
				"%s: маппер отказа учёта наружу не найден — тон этого владельца "+
					"вне наблюдения, и «нарушений нет» о нём сказать нельзя", o))
			continue
		}
		if f.stripperFile == "" {
			out = append(out, fmt.Sprintf(
				"%s: маппер наружу (%s) производит клиентский текст вызовом %q, "+
					"объявления которого в прод-дереве владельца нет — вывод префикса "+
					"проверить нечем", o, f.outwardFile, f.stripperName))
			continue
		}
		if f.literalPrefixFile != "" {
			out = append(out, fmt.Sprintf(
				"%s: снимаемый префикс выписан строкой (%s, %q) — перечень префиксов "+
					"есть второе место о sentinel'ах домена и расходится с ним молча; "+
					"выводи префикс из sentinel'а, который распознал вызывающий",
				o, f.literalPrefixFile, f.literalPrefix))
			continue
		}
		if !f.derivesPrefix {
			out = append(out, fmt.Sprintf(
				"%s: %s.%s префикса из sentinel'а не выводит — клиент увидит "+
					"внутреннее имя sentinel'а либо не увидит предложения производителя",
				o, f.stripperFile, f.stripperName))
			continue
		}
		if !f.guardsEmptyRemainder {
			out = append(out, fmt.Sprintf(
				"%s: %s.%s не ограждает ПУСТОЙ остаток — на обёртке без текста "+
					"клиент получит код и ни слова о том, что делать дальше; "+
					"в журнале это неотличимо от потери сообщения",
				o, f.stripperFile, f.stripperName))
		}
	}
	// Словарь sentinel'ов — ось ОБЩАЯ для владельцев, поэтому судится отдельным
	// проходом: находка называет не «у тебя не так», а «вас двое об одном».
	for _, o := range c.Owners {
		f := c.Facts[o]
		if f.sentinelFile == "" {
			out = append(out, fmt.Sprintf(
				"%s: объявления словаря sentinel'ов учёта (%s / %s) в прод-дереве "+
					"владельца не найдено — ось вне наблюдения",
				o, ct2QuotaExceededVar, ct2QuotaNotProvisionedVar))
			continue
		}
		for name, want := range map[string]string{
			ct2QuotaExceededVar:       ct2QuotaExceededSentinel,
			ct2QuotaNotProvisionedVar: ct2QuotaNotProvisionedSentinel,
		} {
			got, ok := f.sentinelTexts[name]
			if !ok {
				out = append(out, fmt.Sprintf(
					"%s: %s не объявляет %s — словарь учёта неполон",
					o, f.sentinelFile, name))
				continue
			}
			if got != want {
				out = append(out, fmt.Sprintf(
					"%s: %s объявляет %s текстом %q, общий словарь шести — %q; "+
						"этим текстом снимается префикс, поэтому расхождение делает "+
						"мапперы несравнимыми, а клиенту оно не видно и потому тихо",
					o, f.sentinelFile, name, got, want))
			}
		}
	}
	return out
}

// ct2ToneCallIs — вызов есть `<pkg>.<fn>`, где pkg приведён к пути импорта.
//
// Приведение к пути, а не к имени: `status` — распространённое имя, и
// одноимённый чужой помощник иначе стал бы нашим.
func ct2ToneCallIs(c *ast.CallExpr, imports map[string]string, pkgPath, fn string) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != fn {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return imports[id.Name] == pkgPath
}

// ct2ToneGuardsEmptyRemainder — стриппер ограждает ПУСТОЙ ОСТАТОК.
//
// Судится не «есть ли в теле сравнение с пустой строкой», а сравнение ИМЕННО
// ТОГО значения, которое отдаётся клиенту, — остатка, связанного срезкой
// префикса. Различие несущее, и оно измерено: прежняя, широкая редакция считала
// огражденным дофиксовый nlb, у которого сравнение с пустой строкой было, но
// относилось ко ВСЕМУ сообщению, а не к остатку. Ложное «ограждено» — ошибка в
// опасную сторону: гейт объявлял бы свойство там, где его нет.
//
// Законных форм записи ДВЕ, и обе распознаются (п.7 §«Гейт на класс»):
//
//	if rest == "" { … }        — сравнение со строкой;
//	if len(rest) == 0 { … }    — сравнение длины.
//
// ГРАНИЦА НАЗВАНА: гейт судит НАЛИЧИЕ ограждения, а не его правильность —
// «функция не может вернуть пустую строку на непустом входе» статически
// неразрешимо. Правильность держат парные пробы владельцев
// (`empty_refusal_test.go`).
func ct2ToneGuardsEmptyRemainder(fn *ast.FuncDecl) bool {
	// Остаток — первое из значений, связанных вызовом срезки префикса.
	remainders := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, isAs := n.(*ast.AssignStmt)
		if !isAs || len(as.Rhs) != 1 || len(as.Lhs) < 1 {
			return true
		}
		call, isCall := as.Rhs[0].(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != ct2CutPrefixFunc {
			return true
		}
		if id, isID := as.Lhs[0].(*ast.Ident); isID {
			remainders[id.Name] = true
		}
		return true
	})
	if len(remainders) == 0 {
		return false
	}

	var ok bool
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if ok {
			return false
		}
		bin, isBin := n.(*ast.BinaryExpr)
		if !isBin || (bin.Op != token.EQL && bin.Op != token.NEQ) {
			return true
		}
		for a, b := range map[int]int{0: 1, 1: 0} {
			sides := []ast.Expr{bin.X, bin.Y}
			if ct2ToneIsEmptyOf(sides[a], remainders) && ct2ToneIsEmptyMark(sides[b]) {
				ok = true
				return false
			}
		}
		return true
	})
	return ok
}

// ct2ToneIsEmptyOf — выражение есть сам остаток либо его длина.
func ct2ToneIsEmptyOf(e ast.Expr, remainders map[string]bool) bool {
	if id, isID := e.(*ast.Ident); isID {
		return remainders[id.Name]
	}
	call, isCall := e.(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return false
	}
	fn, isID := call.Fun.(*ast.Ident)
	if !isID || fn.Name != "len" {
		return false
	}
	arg, isArg := call.Args[0].(*ast.Ident)
	return isArg && remainders[arg.Name]
}

// ct2ToneIsEmptyMark — пустая строка либо нулевая длина.
func ct2ToneIsEmptyMark(e ast.Expr) bool {
	lit, isLit := e.(*ast.BasicLit)
	if !isLit {
		return false
	}
	switch lit.Kind {
	case token.STRING:
		v, err := strconv.Unquote(lit.Value)
		return err == nil && v == ""
	case token.INT:
		return lit.Value == "0"
	default:
		return false
	}
}

// ct2ToneSentinelTexts — объявленный словарь sentinel'ов учёта в этом файле.
//
// Разбор УЗЛА, а не поиск по тексту: те же имена и те же строки стоят в
// комментариях рядом — в том числе в шапке этого файла.
func ct2ToneSentinelTexts(f *ast.File) map[string]string {
	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, isVS := n.(*ast.ValueSpec)
		if !isVS {
			return true
		}
		for i, name := range vs.Names {
			if name.Name != ct2QuotaExceededVar && name.Name != ct2QuotaNotProvisionedVar {
				continue
			}
			if i >= len(vs.Values) {
				continue
			}
			// Объявление вида `errors.New("…")` / `stderrors.New("…")`; алиас на
			// чужую переменную текста не несёт и потому пропускается — словарь
			// объявляет тот, кто его ЗАВОДИТ.
			call, isCall := vs.Values[i].(*ast.CallExpr)
			if !isCall || len(call.Args) != 1 {
				continue
			}
			lit, isLit := call.Args[0].(*ast.BasicLit)
			if !isLit || lit.Kind != token.STRING {
				continue
			}
			if v, err := strconv.Unquote(lit.Value); err == nil {
				out[name.Name] = v
			}
		}
		return true
	})
	return out
}

// ct2ToneVocabularyAgrees — словарь владельца совпал с общим.
func ct2ToneVocabularyAgrees(f *ct2ToneOwnerFacts) bool {
	if f.sentinelFile == "" {
		return false
	}
	for name, want := range map[string]string{
		ct2QuotaExceededVar:       ct2QuotaExceededSentinel,
		ct2QuotaNotProvisionedVar: ct2QuotaNotProvisionedSentinel,
	} {
		got, ok := f.sentinelTexts[name]
		if !ok || got != want {
			return false
		}
	}
	return true
}
