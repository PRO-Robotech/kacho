// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenclaimforeignbrand_test.go — клеймо выпущенного токена называет СВОЙ
// продукт, и написание у него ОДНО (задача #2127, семейство приёмки
// IAM-SEV-NAME-05).
//
// # Почему это гейт дерева, а не проба службы
//
// Клеймо ставит одна сторона, а читают его многие: край, служба реестра,
// посевные наборы, профиль развёртывания, собранные коллекции проб. Проба
// службы зелена при любом состоянии читателей — она их не видит. «Во всём
// дереве клеймо ровно одного словаря» есть свойство ДЕРЕВА, и держать его может
// только обход дерева.
//
// # Два утверждения, и второе несёт больше первого
//
// Ось А — ИДЕНТИЧНОСТЬ: имя клейма принадлежит словарю своего продукта. Оно
// читается оператором чужого облака БЕЗ нашего исходного кода — достаточно
// раскодировать токен, — и по норме разделения (`kacho#2076`) это имя, которым
// продукт себя называет, а не код, который он исполняет.
//
// Ось Б — ОДНО НАПИСАНИЕ: у имени из словаря нет двойника в другом словаре
// нигде в отслеживаемом дереве. Решение Р14 линии выноса отвергает окно, в
// котором принимаются оба написания: два имени одного клейма — два словаря об
// одном предмете, и расходятся они молча. Ось Б и есть то, что делает
// половинчатое переименование невозможным: клеймо, которое чеканка ставит под
// одним именем, а читатель ищет под другим, даёт отказ доступа, выглядящий
// дефектом прав.
//
// # Что здесь считается деревом
//
// Индекс git — то же множество, которое увидит свежий клон и CI.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// claimOwnNamespace — словарь, которым продукт называет свои клеймы.
	claimOwnNamespace = "kaname"
	// claimForeignNamespace — словарь платформы. Он остаётся законным у метрик,
	// схем и типов ресурсов модуля инфраструктуры — но не у клейма токена.
	claimForeignNamespace = "kacho"
	// claimGoCensusFloor — порог переписи файлов Go.
	claimGoCensusFloor = 1000
	// claimVocabularyFloor — сколько имён обязан вывести разбор. Ноль означает,
	// что он перестал видеть предмет, а не что дерево чисто.
	claimVocabularyFloor = 15
	// claimMintedFloor — сколько имён обязано стоять КЛЮЧОМ состава. Словарь
	// выводится семенем чеканки; пустое семя делает вывод беспредметным.
	claimMintedFloor = 10
)

// claimNamespaces — словари, чьи имена разбор считает именами клейм.
var claimNamespaces = map[string]bool{
	claimOwnNamespace:     true,
	claimForeignNamespace: true,
}

type claimBrandScan struct {
	Vocab  ClaimVocabulary
	Parsed int
	Census ClaimNameCensus
	// Prefixes — приставки словаря, отданные предикату, в файлах области.
	Prefixes []ClaimNameUse
	// Files — пофайловый разбор всего дерева Go, включая файлы ВНЕ области.
	// Нужен, чтобы имя чужого словаря нашлось и в файле, который ни одного
	// имени своего словаря не называет: связная компонента такой файл не
	// захватывает by construction, а клеймо в нём стоит.
	Files map[string]ClaimFileScan
}

func scanClaimBrandTree(t *testing.T) claimBrandScan {
	t.Helper()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if strings.HasSuffix(rel, ".go") && !skipPath(rel) {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)

	out := claimBrandScan{}
	out.Census.ByForm = map[ClaimNameForm]int{}
	files := make(map[string]ClaimFileScan, len(rels))
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		out.Parsed++
		uses, c, err := ScanTokenClaimNames(rel, src, claimNamespaces)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		out.Census.Literals += c.Literals
		out.Census.Shaped += c.Shaped
		out.Census.Positions += c.Positions
		for f, n := range c.ByForm {
			out.Census.ByForm[f] += n
		}
		fs := ClaimFileScan{Uses: uses}
		// Семя словаря берётся ТОЛЬКО из не-тестового дерева: синтетика проб
		// объявляет составы нарочно, и посеянный ею словарь описывал бы
		// фикстуры, а не продукт.
		if !strings.HasSuffix(rel, "_test.go") {
			mint, err := ScanClaimMint(rel, src, claimNamespaces, claimMinKeys)
			if err != nil {
				t.Fatalf("разбор составов %s: %v", rel, err)
			}
			fs.Assembled = mint
		}
		files[rel] = fs
	}
	out.Vocab = DeriveClaimVocabulary(files)
	out.Files = files
	for rel, fs := range files {
		if !out.Vocab.Files[rel] {
			continue
		}
		for _, u := range fs.Uses {
			if u.Form == ClaimFormPrefix {
				out.Prefixes = append(out.Prefixes, u)
			}
		}
	}
	sort.Slice(out.Prefixes, func(i, j int) bool {
		if out.Prefixes[i].File != out.Prefixes[j].File {
			return out.Prefixes[i].File < out.Prefixes[j].File
		}
		return out.Prefixes[i].Line < out.Prefixes[j].Line
	})
	return out
}

func claimFormsLine(c ClaimNameCensus) string {
	var parts []string
	for _, f := range []ClaimNameForm{
		ClaimFormKey, ClaimFormRead, ClaimFormCase, ClaimFormArg,
		ClaimFormConst, ClaimFormPrefix,
	} {
		parts = append(parts, fmt.Sprintf("%s %d", f, c.ByForm[f]))
	}
	return strings.Join(parts, " · ")
}

func (s claimBrandScan) log(t *testing.T, axis string) {
	t.Helper()
	t.Logf("перепись (%s): файлов Go разобрано %d, строковых литералов прочитано %d, "+
		"из них формы имени клейма %d, из них в позиции клейма %d (%s); "+
		"словарь выведен за %d круга: имён %d (из них чеканится %d), файлов области %d",
		axis, s.Parsed, s.Census.Literals, s.Census.Shaped, s.Census.Positions,
		claimFormsLine(s.Census), s.Vocab.Rounds, len(s.Vocab.Names),
		len(s.Vocab.Minted), len(s.Vocab.Files))
}

func (s claimBrandScan) assertCensusStands(t *testing.T) {
	t.Helper()
	if s.Parsed < claimGoCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d",
			s.Parsed, claimGoCensusFloor)
	}
	if len(s.Vocab.Minted) < claimMintedFloor {
		t.Fatalf("ключами состава стоит %d имён при пороге %d — семя словаря пусто, "+
			"и вывод беспредметен: разбор перестал видеть место чеканки",
			len(s.Vocab.Minted), claimMintedFloor)
	}
	if len(s.Vocab.Names) < claimVocabularyFloor {
		t.Fatalf("словарь клейм выведен из %d имён при пороге %d — разбор перестал "+
			"видеть предмет, и его молчание сказано ни о чём",
			len(s.Vocab.Names), claimVocabularyFloor)
	}
}

// TestTokenClaimsCarryTheProductsOwnName — ось А: идентичность.
func TestTokenClaimsCarryTheProductsOwnName(t *testing.T) {
	t.Parallel()
	scan := scanClaimBrandTree(t)
	scan.log(t, "ось А")
	scan.assertCensusStands(t)

	var found []string
	for name, u := range scan.Vocab.Names {
		if !strings.HasPrefix(name, claimForeignNamespace+"_") {
			continue
		}
		mint := "читается"
		if scan.Vocab.Minted[name] {
			mint = "чеканится"
		}
		found = append(found, fmt.Sprintf("%s — %s, впервые %s:%d", name, mint, u.File, u.Line))
	}
	for _, p := range scan.Prefixes {
		if p.Namespace == claimForeignNamespace {
			found = append(found, fmt.Sprintf("%s (приставка целого словаря) — %s:%d %s",
				p.Name, p.File, p.Line, p.Func))
		}
	}

	// Файл ВНЕ области: он не называет ни одного имени своего словаря, поэтому
	// связная компонента его не захватывает — а клеймо в нём стоять может.
	// Судится ДВОЙНИК: имя чужого словаря, у которого в своём словаре есть
	// тёзка. Однофамильцы приставки под это не подпадают — тёзки у них нет.
	//
	// НЕ-ТЕСТОВЫЕ, и это тот же разрез, что у семени словаря: синтетика проб
	// объявляет чужое имя НАРОЧНО — им доказывается, что гейт умеет упасть.
	// Судя её, гейт краснел бы на собственном доказательстве, а починка состояла
	// бы в том, чтобы доказательство обессмыслить.
	//
	// ЧТО ЭТО ОСТАВЛЯЕТ НЕПОКРЫТЫМ, названо, а не спрятано: пробный файл,
	// который называет ТОЛЬКО чужие имена и ни одного своего. Пробу продукта это
	// не задевает — она называет и своё, поэтому в область входит и судится
	// правилом выше; предмет исключения — ровно синтетика гейтов.
	for path, fs := range scan.Files {
		if scan.Vocab.Files[path] || strings.HasSuffix(path, "_test.go") {
			continue
		}
		for _, u := range fs.Uses {
			if u.Form == ClaimFormPrefix || u.Namespace != claimForeignNamespace {
				continue
			}
			twin := ForeignTwin(u.Name, claimForeignNamespace, claimOwnNamespace)
			if _, isClaim := scan.Vocab.Names[twin]; !isClaim {
				continue
			}
			found = append(found, fmt.Sprintf("%s — двойник клейма %s, %s:%d %s",
				u.Name, twin, u.File, u.Line, u.Func))
		}
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Fatalf("клейм чужого словаря %q найдено %d из %d выведенных:\n  %s\n\n"+
			"Имя клейма читается оператором чужого облака БЕЗ нашего исходного кода — "+
			"достаточно раскодировать токен. По норме разделения (`kacho#2076`) это имя, "+
			"которым продукт себя называет, а не код, который он исполняет.\n"+
			"Приставка целого словаря, отданная предикату, тяжелее одного имени: она "+
			"переносит словарь разом и при смене имён молча перестаёт совпадать — читатель "+
			"не отказывает, он просто ничего не находит.",
			claimForeignNamespace, len(found), len(scan.Vocab.Names),
			strings.Join(found, "\n  "))
	}
}

// TestClaimNameHasNoTwinInTheOtherNamespace — ось Б: одно написание.
//
// Правило выведено из оси А, а не выписано: снято клеймо — снято и правило о
// нём. Однофамильцы (метрика, схема, тип ресурса модуля инфраструктуры) под него
// не подпадают by construction — в словарь клейм они не входят, значит двойника
// не порождают.
func TestClaimNameHasNoTwinInTheOtherNamespace(t *testing.T) {
	t.Parallel()
	scan := scanClaimBrandTree(t)
	scan.log(t, "ось Б")
	scan.assertCensusStands(t)

	twins := map[string]string{}
	for name := range scan.Vocab.Names {
		other := claimForeignNamespace
		if strings.HasPrefix(name, claimForeignNamespace+"_") {
			other = claimOwnNamespace
		}
		for _, from := range []string{claimOwnNamespace, claimForeignNamespace} {
			if tw := ForeignTwin(name, from, other); tw != "" && tw != name {
				twins[tw] = name
			}
		}
	}

	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	// Go из оси Б ИСКЛЮЧЁН, и это не послабление, а разделение труда.
	//
	// Ось Б читает СЫРОЙ ТЕКСТ: позиции вне Go нет, судить по ней нечего. На Go
	// тот же приём находил бы имя в комментарии и в синтетике — то есть в
	// СОБСТВЕННОМ объяснении гейта и в фикстурах, которые нарочно несут чужое
	// имя, чтобы инъекция что-то доказывала. Гейт, краснеющий на своём
	// объяснении, снимают первым.
	//
	// Покрытие при этом не теряется: Go целиком судит ось А — по узлу разбора,
	// поэтому комментарий и синтетика ей не видны by construction, а файл вне
	// области она добирает правилом двойника выше.
	var rels []string
	for rel := range tt.files {
		if !skipPath(rel) && !strings.HasSuffix(rel, ".go") {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)

	var (
		scanned, readable int
		found             []string
	)
	for _, rel := range rels {
		scanned++
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		readable++
		for _, h := range FindClaimTwins(string(src), twins) {
			found = append(found, fmt.Sprintf("%s:%d  %s (двойник клейма %s)",
				rel, h.Line, h.Twin, h.Of))
		}
	}
	sort.Strings(found)
	t.Logf("перепись оси Б: отслеживаемых НЕ-Go файлов осмотрено %d (прочитано %d), "+
		"двойников выведено %d, находок %d; файлы Go судит ось А — по узлу разбора",
		scanned, readable, len(twins), len(found))

	if readable == 0 {
		t.Fatalf("прочитано ноль файлов при %d осмотренных — обход пуст, вердикт беспредметен",
			scanned)
	}
	if len(found) > 0 {
		t.Fatalf("двойников имени клейма найдено %d:\n  %s\n\n"+
			"Окна двух написаний нет (решение Р14): два имени одного клейма — два словаря "+
			"об одном предмете, и расходятся они молча. Разрез ОДИН: чеканка и ВСЕ "+
			"читатели — край, служба реестра, посевные наборы, профиль развёртывания, "+
			"собранные коллекции, страницы — переименовываются одним изменением.",
			len(found), strings.Join(found, "\n  "))
	}
}

// claimAuthorFloor — сколько имён обязано стоять в области оси В. Ноль означал
// бы, что не-тестовое дерево перестало называть клеймы вовсе, а не что у
// каждого имени нашёлся автор.
const claimAuthorFloor = 15

// claimReadWithoutAMinter — ведомость имён, которые дерево ЧИТАЕТ, ни разу не
// чеканя, и это РЕШЕНО, а не забыто.
//
// # Зачем ведомость вообще, если исходов три
//
// Исходов у имени без чеканщика три: чеканить · снять читателей вместе с тем,
// что они подпирали · записать решением, почему чеканщика нет. Третий исход
// законен и встречается: клеймо бывает косметическим, и тогда его отсутствие —
// ОБЪЯВЛЕННОЕ состояние с назначенным замещением, а не пропажа. Без ведомости
// третий исход выразить нечем, и гейт, у которого нет способа принять законный
// случай, снимает первый же, кто на него наткнётся.
//
// # Чем она отличается от маски
//
// Запись несёт КООРДИНАТУ решения, а не оправдание, и ИСТЕКАЕТ САМА: имя,
// которое дерево перестало читать либо начало чеканить, делает свою запись
// находкой. Пустая ведомость — цель, а не поломка: проверка на ней проходит.
//
// # Что здесь НЕ является законным случаем
//
// Читатель, у которого отсутствие клейма даёт ОТКАТ, а откат называет предмет
// иначе, чем его знает модель прав. Такое отсутствие не объявлено, а прикрыто:
// вызывающий получает не «признака нет», а неверный признак. Ровно за это сняты
// четыре имени задачи #2142 — их читатели брали откат ВСЕГДА.
var claimReadWithoutAMinter = map[string]string{
	// Отображаемое имя принципала. Косметическое ПО РЕШЕНИЮ: ни на одно решение
	// о доступе не влияет, а его отсутствие объявлено и замещается
	// идентификатором — обоими читателями, одинаково и явно.
	// Решение — services/iam/internal/domain/principal_claims.go; замещения —
	// там же (`principalFrom` соседнего пакета предъявленного удостоверения) и
	// gateway/internal/middleware/auth.go (`principalFromVerifiedToken`).
	// Запись истечёт сама, когда выпуск начнёт его ставить.
	"kaname_principal_display_name": "косметическое, отсутствие замещается " +
		"идентификатором — principal_claims.go",
}

// claimNameIsNamedOutsideTests — называет ли имя хоть один НЕ-ТЕСТОВЫЙ файл.
//
// Область оси В — словарь ПРОДУКТА, а не фикстур. Синтетика проб называет имена
// нарочно: край проносит незнакомое клеймо насквозь, и проба подаёт ему
// заведомо неизвестное имя, чтобы это доказать. Судя её, гейт требовал бы
// чеканщика для имени, которого не бывает, — то есть краснел бы на собственном
// доказательстве. Это тот же разрез, что у семени словаря и у правила двойника.
func claimNameIsNamedOutsideTests(scan claimBrandScan, name string) bool {
	for path, fs := range scan.Files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		for _, u := range fs.Uses {
			if u.Form != ClaimFormPrefix && u.Name == name {
				return true
			}
		}
	}
	return false
}

// TestEveryClaimNameHasAnAuthor — ось В: у имени клейма есть АВТОР.
//
// # Предмет
//
// Ось А судит, ЧЬИМ словарём названо клеймо; ось Б — что написание у него одно.
// Ни та, ни другая не спрашивает главного: ставит ли это имя хоть кто-нибудь.
// Имя, которое дерево только читает, объявляет возможность, неисполнимую ни при
// каком входе: читатель есть, производителя нет. Пока это так, «клеймо не
// пришло» и «клейма не бывает» с читающей стороны неразличимы, и решать, чего
// именно не хватает, приходится каждому, кто на них наткнётся.
//
// Это близнец запрета «принято-и-проигнорировано»: там поле принимают и не
// читают, здесь — читают то, чего никто не производит.
//
// # Что считается чеканкой
//
// Ключ состава — литералом ЛИБО идентификатором, объявленным в дереве
// константой. Различие между двумя записями лежит в тексте, а не в том, попадёт
// ли клеймо в токен, и распознаватель, знающий одну форму, объявил бы чеканимое
// имя читаемым. Сколько имён пришло каждой формой, печатает перепись:
// расширение распознавателя, не изменившее ни одного числа, холостое.
//
// # Суждение вынесено ЧИСТОЙ функцией
//
// `AuditClaimAuthors` живёт рядом с разбором, и инъекция подаёт ей синтетический
// словарь. Суждение, спрятанное внутрь тела пробы, доказывается только деревом —
// то есть ровно тем состоянием, ради которого проверка и заведена.
func TestEveryClaimNameHasAnAuthor(t *testing.T) {
	t.Parallel()
	scan := scanClaimBrandTree(t)
	scan.log(t, "ось В")
	scan.assertCensusStands(t)

	inArea := func(name string) bool { return claimNameIsNamedOutsideTests(scan, name) }
	audit := AuditClaimAuthors(scan.Vocab, inArea, claimReadWithoutAMinter)

	var names []string
	for name := range scan.Vocab.Names {
		names = append(names, name)
	}
	sort.Strings(names)

	var roles []string
	for _, name := range names {
		role := "читается"
		switch {
		case scan.Vocab.MintedByIdent[name]:
			role = "чеканится (ключ-константа)"
		case scan.Vocab.Minted[name]:
			role = "чеканится"
		}
		scope := "только пробы"
		if inArea(name) {
			scope = "продукт"
		}
		line := fmt.Sprintf("%s — %s, %s", name, role, scope)
		if why, recorded := claimReadWithoutAMinter[name]; recorded && !scan.Vocab.Minted[name] {
			line += " (решено: " + why + ")"
		}
		roles = append(roles, line)
	}

	t.Logf("перепись (ось В): имён %d, из них в не-тестовом дереве %d; "+
		"чеканится %d (из них ключом-константой %d); записей ведомости %d "+
		"(из них без предмета %d); идентификаторов, объявленных под двумя именами, %d\n"+
		"роли по именам:\n  %s",
		len(names), len(audit.Area), len(scan.Vocab.Minted), len(scan.Vocab.MintedByIdent),
		len(claimReadWithoutAMinter), len(audit.Stale), len(scan.Vocab.AmbiguousIdents),
		strings.Join(roles, "\n  "))

	if len(audit.Area) < claimAuthorFloor {
		t.Fatalf("в не-тестовом дереве названо %d имён при пороге %d — обход перестал "+
			"видеть предмет, и его молчание сказано ни о чём", len(audit.Area), claimAuthorFloor)
	}

	// Ведомость истекает САМА: запись, которой больше нечего исключать, —
	// находка. Иначе послабление переживает свой предмет и молча прощает
	// следующего, кто унаследует ровно эту слепую зону.
	if len(audit.Stale) > 0 {
		t.Fatalf("записей ведомости без предмета %d:\n  %s\n\n"+
			"Послабление живёт, пока у него есть предмет. Запись, которой больше "+
			"нечего исключать, унаследует следующая слепая зона — и простит её молча.",
			len(audit.Stale), strings.Join(audit.Stale, "\n  "))
	}

	var found []string
	for _, name := range audit.Missing {
		u := scan.Vocab.Names[name]
		found = append(found, fmt.Sprintf("%s — читается, чеканщика нет; впервые %s:%d %s",
			name, u.File, u.Line, u.Func))
	}
	if len(found) > 0 {
		t.Fatalf("имён клейм без чеканщика %d из %d названных продуктом:\n  %s\n\n"+
			"У словаря клейм два автора: одна сторона объявляет состав, другая "+
			"называет имена, которых в составе нет. Имя без автора объявляет "+
			"возможность, неисполнимую ни при каком входе, — и с читающей стороны "+
			"«клеймо не пришло» неотличимо от «клейма не бывает».\n"+
			"Исходов три, четвёртого нет: чеканить · снять читателей вместе с тем, "+
			"что они подпирали · записать решением в `claimReadWithoutAMinter`, "+
			"назвав координату решения.",
			len(found), len(audit.Area), strings.Join(found, "\n  "))
	}
}
