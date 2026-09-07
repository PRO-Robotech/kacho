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
			keys, err := ScanClaimMint(rel, src, claimNamespaces, claimMinKeys)
			if err != nil {
				t.Fatalf("разбор составов %s: %v", rel, err)
			}
			fs.Assembled = keys
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
