// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// credentialfieldsource_test.go — У ЧИТАЕМОГО ПОЛЯ ПРОВЕРЕННОГО УДОСТОВЕРЕНИЯ
// ОБЯЗАН БЫТЬ НАЗВАН ИСТОЧНИК (задача #1217, приёмка BAT-1 §5.2, сценарий
// BAT-1-68).
//
// # Предмет
//
// Проверенное удостоверение — единственный вход для отзыва, шага повышения
// уровня, вывода принципала и служебных метаданных края. Поле, которое полоса
// не заполнила, делает нижележащий контроль ПРОЙДЕННЫМ МИМО, а не успешно, — и
// это неотличимо от исправной работы: запрос проходит, контроль не сработал ни
// разу, и «ноль отказов за всю жизнь контроля» никто не заметил.
//
// Поэтому требование адресовано не значению, а ОБЪЯВЛЕНИЮ: у каждого поля,
// которое кто-то читает, назван источник. Названный источник — единственное
// место, где «здесь всегда пусто» отличимо от «здесь забыли заполнить».
//
// # ПОЧЕМУ ГЕЙТ, А НЕ ПРОБА — И ЭТО РОВНО ТО, ЧЕМ #1217 ЗАВЕДЕНА
//
// Свойство держала проба, перечислявшая поля поимённо
// (`TestBAT1_31_TheCredentialDeclaresItselfBearerAndCarriesNoBinding`). Проба
// закрепляет ОТВЕТ на известном входе: она знает ровно то, что в неё вписали.
// Поле, заведённое завтра, не краснит её никогда — то есть свойство
// существовало у перечисленных мест и не существовало у КЛАССА.
//
// Свойство «у каждого поля назван источник» есть свойство ДЕРЕВА, и держать его
// может только обход дерева.
//
// # ЧТО СЧИТАЕТСЯ ПОЛОСОЙ ПРЕДЪЯВЛЕНИЯ — предикат, а не список
//
// Полоса — метод, у которого ВСЁ ТРИ:
//
//   - имя `Verify`;
//   - вход `(context.Context, string)` — контекст и ПРЕДЪЯВЛЕННАЯ СТРОКА;
//   - результат `(<структурный тип>, error)` — проверенное удостоверение либо
//     отказ.
//
// # ГРАНИЦА НАЗВАНА: имя `Verify` — соглашение, и это ЗАМЕРЕНО, а не принято
//
// Предикат по имени мерит соглашение об именовании: полоса, названная иначе,
// проходит мимо. Имя оставлено не по недосмотру — без него предикат перестаёт
// измерять предмет. Замер на ревизии cb13d9367 тем же обходом со СНЯТЫМ
// условием на имя: методов формы `(context.Context, string) (структура, error)`
// в дереве **сотни** — репозитории, клиенты соседей, use-case'ы, — и почти все
// удостоверения не проверяют. Инструмент, у которого почти все находки ложные,
// перестают читать, а перестав читать, возвращаются к отсутствию проверки.
//
// Поэтому: имя сужает, а два структурных условия судят. Полоса, названная
// иначе, — предел гейта, и держит её обзор изменения края, а не этот обход.
//
// # ЧИТАЕМОЕ ПОЛЕ — ошибка предиката ОДНОСТОРОННЯЯ, и сторона выбрана
//
// Поле считается читаемым, если селектор `.Имя` встречается в НЕ-ТЕСТОВОМ файле
// дерева, отличном от файла объявления носителя. Разрешения типов здесь нет:
// одноимённое поле чужой структуры засчитывается тоже.
//
// Ошибка от этого возможна ровно в одну сторону — гейт потребует источника у
// поля, которого никто не читает. Сторона выбрана намеренно: предмет гейта —
// НАЗВАТЬ источник, а лишний названный источник вредом не является. Обратная
// ошибка (промолчать на читаемом поле) была бы той самой слепой зоной.
//
// # ИСТОЧНИК НАЗВАН — ДВЕ ЗАКОННЫЕ ФОРМЫ, И ВТОРАЯ ВЫЯСНИЛАСЬ ЗАМЕРОМ
//
// Первая редакция предиката читала только собственный комментарий поля — и
// объявила находкой `BasicVerifiedCredential.PrincipalID` и `.DisplayName`, у
// которых источник назван ГРУППОВЫМ комментарием на три поля сразу
// («PrincipalType / PrincipalID / DisplayName — из ответа авторитета»). Это
// законная форма, она в дереве живёт, и гейт, красный на ней, сняли бы первым
// же ложным срабатом.
//
// Поэтому источник считается названным, если у поля есть свой комментарий
// (doc либо строчный) ЛИБО ближайший предшествующий doc-комментарий внутри той
// же структуры НАЗЫВАЕТ это поле по имени. «Называет по имени», а не «стоит
// выше»: комментарий, перечисляющий три поля, не покрывает четвёртое,
// дописанное под ним, — а именно этот случай гейт и заводится ловить.
//
// # ЧЕГО ГЕЙТ НЕ ДЕРЖИТ — названо, чтобы его не сочли полным покрытием
//
//	ПРАВИЛЬНОСТЬ названного источника: комментарий, называющий не тот источник,
//	  неотличим от верного. Держит обзор изменения края;
//	ЗАПОЛНЕНО ЛИ поле в рантайме: гейт судит ОБЪЯВЛЕНИЕ. Что значение доезжает,
//	  утверждают пробы полосы (BAT-1-58 и соседние);
//	ПОЛОСА, НАЗВАННАЯ ИНАЧЕ: см. «граница названа» выше.

// credentialLaneRoots — корни обхода. Те же, что у прочих гейтов провязки: код,
// который исполняется. Полосы предъявления живут на крае и в сервисах.
var credentialLaneRoots = []string{"services", "gateway", "pkg"}

// credentialLaneMethod — имя метода, сужающего предикат полосы.
const credentialLaneMethod = "Verify"

// credentialLane — одна найденная обходом полоса предъявления.
type credentialLane struct {
	receiver   string // тип-полоса
	carrier    string // имя типа проверенного удостоверения
	carrierDir string // каталог пакета, где этот тип объявлен
	where      string // файл:строка объявления метода
}

// credentialField — одно поле носителя.
type credentialField struct {
	name        string
	where       string // файл:строка объявления поля
	sourceNamed bool
	read        bool
}

// credentialCarrier — носитель проверенного удостоверения с его полями.
type credentialCarrier struct {
	typ    string
	lane   string
	dir    string
	file   string
	fields []credentialField
}

// openCredentialFieldFinding — читаемое поле без названного источника,
// найденное обходом и оставленное ЧУЖОЙ полосе работы.
//
// Не прощение: запись обязана нести предмет и владельца, и гейт роняет прогон,
// как только прощать станет нечего, — В ОБЕ СТОРОНЫ. Поле снято — запись
// потеряла предмет; поле получило источник — запись потеряла предмет тоже.
type openCredentialFieldFinding struct {
	// carrier / field — координата ровно в той форме, в какой её печатает обход.
	carrier, field string
	// owner — кто чинит и по какому предмету.
	owner string
}

// openCredentialFieldFindings — ведомость читаемых полей, у которых источник
// ещё не назван и которые оставлены ЧУЖОЙ полосе работы.
//
// # СЕГОДНЯ ОНА ПУСТА, И ЭТО ЕЁ ЦЕЛЬ, А НЕ ПОЛОМКА
//
// Заводилась она непустой: первый же прогон гейта нашёл 11 полей носителя
// подписанного предъявителя (`JWTVerifier` → `VerifiedToken`) без названного
// источника, и они лежали в чужой области (край, `gateway/**`; полоса #1217 его
// не правит). Источник у всех одиннадцати назван задачей #1227, поэтому записи
// сняты: запись, которой нечего прощать, наследует слепую зону следующему.
//
// Пустая ведомость гейт НЕ роняет — сравните с полем `read`, где ноль означает
// ослепший предикат и потому объявлен отказом. Здесь ноль означает, что
// прощать больше нечего, и падение на нём подталкивало бы держать запись ради
// зелёного.
//
// # САМОИСТЕЧЕНИЕ РАБОТАЕТ В ОБЕ СТОРОНЫ
//
// Запись обязана нести предмет и владельца, и гейт роняет прогон, как только
// прощать станет нечего: поле снято — запись потеряла предмет; поле получило
// источник — тоже. Именно так эти одиннадцать и снялись: назвать источник и
// забыть про ведомость было НЕЛЬЗЯ — прогон краснел, пока запись стояла.
//
// Заводя новую запись, назовите номер задачи и предикат снятия в `owner`:
// «оставлено чужой полосе» без предмета — это прощение, а не ведомость.
var openCredentialFieldFindings []openCredentialFieldFinding

// id — координата поля одной строкой; ключ сверки с таблицей находок.
func (f openCredentialFieldFinding) id() string { return f.carrier + "." + f.field }

// TestBAT1_68_EveryReadCredentialFieldNamesItsSource — сам гейт.
func TestBAT1_68_EveryReadCredentialFieldNamesItsSource(t *testing.T) {
	facts, scanned := readCredentialTree(t)
	if scanned == 0 {
		t.Fatal("обход прочитал НОЛЬ файлов — вердикта нет ни одного. «Находок ноль» " +
			"здесь означало бы «прочитано ноль», а это разные вещи")
	}

	lanes := collectCredentialLanes(facts)
	carriers := collectCredentialCarriers(facts, lanes)
	markCredentialFieldReaders(facts, carriers)

	findings, stale, read, sourced, fieldsTotal := credentialFieldVerdict(carriers,
		openCredentialFieldFindings)

	// ПЕРЕПИСЬ — обе величины, а не одна: одно число скрывает ровно тот случай,
	// ради которого гейт заведён.
	t.Logf("осмотрено: файлов %d · полос %d · носителей %d · полей %d · читаемых %d · "+
		"с названным источником %d · записей ведомости %d",
		scanned, len(lanes), len(carriers), fieldsTotal, read, sourced,
		len(openCredentialFieldFindings))
	for _, l := range lanes {
		t.Logf("   полоса %s -> %s (%s)", l.receiver, l.carrier, l.where)
	}

	// ПРЕДПОСЫЛКИ. Ноль здесь — слепота предиката, а не благополучие дерева.
	if len(lanes) == 0 {
		t.Fatal("полос предъявления найдено НОЛЬ — предикат ослеп либо соглашение об " +
			"именовании сменилось. Гейт молчал бы и тогда, когда предмет исчез")
	}
	if len(carriers) == 0 {
		t.Fatal("носителей проверенного удостоверения НОЛЬ при непустом наборе полос — " +
			"объявление типа-результата не разрешается, и судить нечего")
	}
	if read == 0 {
		t.Fatal("читаемых полей НОЛЬ — распознавание читателей ослепло; гейт отработал " +
			"на пустом множестве и покраснеть не мог")
	}

	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("записи ведомости ПОТЕРЯЛИ ПРЕДМЕТ: %v.\n"+
			"Поле снято либо источник у него назван — прощать нечего. Запись, которой "+
			"нечего прощать, наследует слепую зону следующему: снимите её.", stale)
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("читаемые поля проверенного удостоверения БЕЗ названного источника: %v.\n"+
			"Поле, которое полоса не заполнила, делает нижележащий контроль ПРОЙДЕННЫМ "+
			"МИМО, а не успешно, и это неотличимо от исправной работы. Назовите источник "+
			"в объявлении поля (§5.2 приёмки BAT-1).", findings)
	}
}

// credentialFieldVerdict — суждение, отделённое от обхода: инъекция подаёт сюда
// корпус, собранный ею самой, и получает те же находки тем же кодом.
func credentialFieldVerdict(
	carriers []credentialCarrier,
	ledger []openCredentialFieldFinding,
) (findings, stale []string, read, sourced, fieldsTotal int) {
	forgiven := map[string]string{}
	for _, e := range ledger {
		forgiven[e.id()] = e.owner
	}
	seen := map[string]bool{}

	for _, c := range carriers {
		for _, f := range c.fields {
			fieldsTotal++
			if !f.read {
				continue
			}
			read++
			if f.sourceNamed {
				sourced++
				continue
			}
			id := c.typ + "." + f.name
			seen[id] = true
			if _, forgivenHere := forgiven[id]; forgivenHere {
				continue
			}
			findings = append(findings, fmt.Sprintf("%s (%s, полоса %s)", id, f.where, c.lane))
		}
	}

	for _, e := range ledger {
		if !seen[e.id()] {
			stale = append(stale, fmt.Sprintf("%s (%s)", e.id(), e.owner))
		}
	}
	return findings, stale, read, sourced, fieldsTotal
}

// readCredentialTree разбирает не-тестовое дерево С КОММЕНТАРИЯМИ и отдаёт факты
// по каждому файлу плюс объём осмотренного.
//
// Комментарии здесь — ПРЕДМЕТ проверки, а не украшение: без `parser.ParseComments`
// у каждого поля `Doc` пуст, и гейт объявил бы находкой всё дерево разом.
func readCredentialTree(t *testing.T) (facts []goFileFacts, scanned int) {
	t.Helper()
	return readCredentialTreeOverriding(t, nil)
}

// readCredentialTreeOverriding — тот же обход, в котором содержимое названных
// файлов подменено В ПАМЯТИ.
//
// Ради него обход и разнесён на две функции: инъекция обязана прогнать НАСТОЯЩЕЕ
// дерево, а не свою копию разбора, — иначе доказанной окажется копия, а
// исполняться будет оригинал. Подмена живёт только внутри прогона: дерево на
// диске не трогается ни на байт.
func readCredentialTreeOverriding(t *testing.T, overrides map[string]string) (facts []goFileFacts, scanned int) {
	t.Helper()
	fset := token.NewFileSet()
	applied := map[string]bool{}
	walkOwnerRegisterGoFiles(t, repoRoot(t), credentialLaneRoots, func(rel string, body []byte) {
		scanned++
		if src, ok := overrides[filepath.ToSlash(rel)]; ok {
			body = []byte(src)
			applied[filepath.ToSlash(rel)] = true
		}
		file, err := parser.ParseFile(fset, rel, body, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		imports := map[string]bool{}
		for _, im := range file.Imports {
			p, uerr := strconv.Unquote(im.Path.Value)
			rel, own := treeRelOfImport(p)
			if uerr != nil || !own {
				continue
			}
			imports[rel] = true
		}
		facts = append(facts, goFileFacts{
			rel: rel, dir: path.Dir(filepath.ToSlash(rel)),
			pkgName: file.Name.Name, file: file, fset: fset, imports: imports,
		})
	})
	// Подмена, не нашедшая своего файла, — НЕ «нечего подменять»: инъекция
	// прогналась бы на нетронутом дереве и объявила бы гейт способным упасть,
	// ничего не доказав.
	for rel := range overrides {
		if !applied[rel] {
			t.Fatalf("подменяемый файл %s обходом НЕ ПРОЧИТАН — инъекция беспредметна", rel)
		}
	}
	return facts, scanned
}

// collectCredentialLanes — полосы предъявления, объявленные деревом.
func collectCredentialLanes(facts []goFileFacts) []credentialLane {
	var out []credentialLane
	for _, ff := range facts {
		for _, d := range ff.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != credentialLaneMethod {
				continue
			}
			if !takesPresentedString(fd.Type.Params) || !returnsNamedTypeAndError(fd.Type.Results) {
				continue
			}
			qual, typ := qualifiedTypeName(fd.Type.Results.List[0].Type)
			if typ == "" {
				continue
			}
			dir := ff.dir
			if qual != "" {
				dir = importDirForQualifier(ff.file, qual)
			}
			out = append(out, credentialLane{
				receiver:   receiverTypeName(fd.Recv.List[0].Type),
				carrier:    typ,
				carrierDir: dir,
				where: fmt.Sprintf("%s:%d", ff.rel,
					ff.fset.Position(fd.Pos()).Line),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].receiver != out[j].receiver {
			return out[i].receiver < out[j].receiver
		}
		return out[i].carrier < out[j].carrier
	})
	return out
}

// collectCredentialCarriers — объявления носителей, названных полосами.
//
// Тип, объявление которого в осмотренном дереве не находится (`string` в
// результате, тип чужого модуля), носителем не становится: судить его поля
// нечем, и молча засчитать его «без полей» значило бы разбавить перепись нулём.
func collectCredentialCarriers(facts []goFileFacts, lanes []credentialLane) []credentialCarrier {
	var out []credentialCarrier
	seen := map[string]bool{}
	for _, l := range lanes {
		key := l.carrierDir + "." + l.carrier
		if seen[key] {
			continue
		}
		for _, ff := range facts {
			if ff.dir != l.carrierDir {
				continue
			}
			st := structDeclIn(ff.file, l.carrier)
			if st == nil {
				continue
			}
			seen[key] = true
			out = append(out, credentialCarrier{
				typ: l.carrier, lane: l.receiver, dir: ff.dir, file: ff.rel,
				fields: credentialFieldsOf(st, ff),
			})
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].typ < out[j].typ })
	return out
}

// credentialFieldsOf — поля носителя с ответом на вопрос «назван ли источник».
//
// Групповая форма читается ЗДЕСЬ, а не в суждении: doc-комментарий, стоящий
// выше по структуре и НАЗЫВАЮЩИЙ поле по имени, покрывает его. Поле, дописанное
// под таким комментарием и в нём не названное, остаётся без источника — это и
// есть тот случай, ради которого гейт заведён.
func credentialFieldsOf(st *ast.StructType, ff goFileFacts) []credentialField {
	var out []credentialField
	var carried []string // тексты doc-комментариев, встреченных выше в этой структуре
	for _, fl := range st.Fields.List {
		own := ""
		if fl.Doc != nil {
			own = fl.Doc.Text()
			carried = append(carried, own)
		}
		if fl.Comment != nil {
			own += " " + fl.Comment.Text()
		}
		for _, n := range fl.Names {
			named := strings.TrimSpace(own) != ""
			if !named {
				for _, c := range carried {
					if mentionsIdentifier(c, n.Name) {
						named = true
						break
					}
				}
			}
			out = append(out, credentialField{
				name:        n.Name,
				where:       fmt.Sprintf("%s:%d", ff.rel, ff.fset.Position(n.Pos()).Line),
				sourceNamed: named,
			})
		}
	}
	return out
}

// markCredentialFieldReaders — кто читает поля носителей.
//
// Разбор идёт по УЗЛАМ (`ast.SelectorExpr`), а не по тексту: имена полей стоят в
// комментариях этого же дерева чаще, чем в коде, и текстовый предикат зеленел бы
// на собственном объяснении (`testing.md` §«Гейт на класс», п. 4).
func markCredentialFieldReaders(facts []goFileFacts, carriers []credentialCarrier) {
	for ci := range carriers {
		c := &carriers[ci]
		known := map[string]bool{}
		for _, f := range c.fields {
			known[f.name] = true
		}
		read := map[string]bool{}
		for _, ff := range facts {
			if ff.rel == c.file {
				continue
			}
			ast.Inspect(ff.file, func(n ast.Node) bool {
				se, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if known[se.Sel.Name] {
					read[se.Sel.Name] = true
				}
				return true
			})
		}
		for fi := range c.fields {
			c.fields[fi].read = read[c.fields[fi].name]
		}
	}
}

// takesPresentedString — вход полосы: контекст ПЕРВЫМ и ПРЕДЪЯВЛЕННАЯ СТРОКА
// ПОСЛЕДНЕЙ.
//
// # ПЕРВАЯ РЕДАКЦИЯ ТРЕБОВАЛА РОВНО ДВУХ ПАРАМЕТРОВ — И НЕ ВИДЕЛА ЖИВОЙ ПОЛОСЫ
//
// Проверяющий подписанное утверждение (RFC 7523) объявлен как
// `Verify(ctx context.Context, assertionType, raw string)`: список параметров
// несёт ДВА элемента, а имён в нём ТРИ. Счёт по элементам списка давал «два» и
// проходил, счёт по именам давал «три» и отвергал — полоса пропадала из
// переписи целиком.
//
// Заметить это чтением нельзя: перепись печатала «полос 4» вместо «полос 5», и
// четыре — совершенно правдоподобное число. Нашла инъекция, потребовавшая, чтобы
// в переписи стояла ИМЕННО эта полоса.
//
// Поэтому предикат судит КРАЯ входа, а не его длину: контекст первым,
// предъявленная строка последней. Промежуточные параметры (вид предъявления,
// адресат) полосу полосой быть не отменяют.
func takesPresentedString(params *ast.FieldList) bool {
	if params == nil || len(params.List) < 2 || countFields(params) < 2 {
		return false
	}
	sel, ok := params.List[0].Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != "context" || sel.Sel.Name != "Context" {
		return false
	}
	s, ok := params.List[len(params.List)-1].Type.(*ast.Ident)
	return ok && s.Name == "string"
}

// returnsNamedTypeAndError — результат полосы: проверенное удостоверение либо отказ.
func returnsNamedTypeAndError(results *ast.FieldList) bool {
	if countFields(results) != 2 || len(results.List) != 2 {
		return false
	}
	e, ok := results.List[1].Type.(*ast.Ident)
	return ok && e.Name == "error"
}

// qualifiedTypeName — квалификатор пакета и имя типа.
//
// Формы, которые обязаны читаться как ОДИН тип: `T`, `*T`, `pkg.T`, `*pkg.T`,
// `T[K]`, `T[K, V]`. Обобщённые вписаны не для полноты: соседний гейт этого
// пакета их не читал, и слепой зоной оказался ровно тот носитель, ради которого
// его трогали, — перепись при этом печатала число, неотличимое от исправного.
func qualifiedTypeName(e ast.Expr) (qual, typ string) {
	switch t := e.(type) {
	case *ast.Ident:
		return "", t.Name
	case *ast.StarExpr:
		return qualifiedTypeName(t.X)
	case *ast.IndexExpr:
		return qualifiedTypeName(t.X)
	case *ast.IndexListExpr:
		return qualifiedTypeName(t.X)
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name, t.Sel.Name
		}
	}
	return "", ""
}

// importDirForQualifier — каталог модуля, стоящий за квалификатором пакета.
func importDirForQualifier(f *ast.File, qual string) string {
	for _, im := range f.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			continue
		}
		name := p[strings.LastIndex(p, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		if name != qual {
			continue
		}
		rel, _ := treeRelOfImport(p)
		return rel
	}
	return qual
}

// structDeclIn — объявление структурного типа с именем name в файле.
func structDeclIn(f *ast.File, name string) *ast.StructType {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			ts, ok := s.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				return st
			}
		}
	}
	return nil
}

// mentionsIdentifier — называет ли текст комментария ЭТО поле по имени.
//
// Именно по имени, с границами слова: подстрока засчитала бы `ExpiresAt` за
// упоминание `Expires`, и групповой комментарий начал бы покрывать поля, о
// которых он не говорит, — то есть ведомость исключений завелась бы сама собой.
func mentionsIdentifier(text, name string) bool {
	for i := 0; i+len(name) <= len(text); i++ {
		if text[i:i+len(name)] != name {
			continue
		}
		if i > 0 && isCredentialIdentByte(text[i-1]) {
			continue
		}
		if j := i + len(name); j < len(text) && isCredentialIdentByte(text[j]) {
			continue
		}
		return true
	}
	return false
}

func isCredentialIdentByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
