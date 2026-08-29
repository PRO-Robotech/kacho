// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт тона отказа СПОСОБЕН упасть — и что падает он
// на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву (`auditRefusalTone`):
// проба, повторяющая логику своей копией, доказывала бы свойство копии.
//
// У КАЖДОГО ОТРИЦАНИЯ ЗДЕСЬ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. «Находок ноль»
// само по себе неотличимо от «распознаватель ослеп», поэтому каждая молчащая
// проба дополнительно утверждает, что шаг гейтом УВИДЕН (перепись
// `foldedAssertions`) и что он промолчал по существу.
//
// ФИКСТУРА ПРИВЯЗАНА К ДЕРЕВУ, А НЕ К ПАМЯТИ АВТОРА: последняя проба берёт
// НАСТОЯЩИЙ заголовок из закоммиченных коллекций и НАСТОЯЩИЙ текст из
// производителей дерева, возвращает им снятый дефект и требует красного.
// Сменится форма записи заголовков — проба скажет об этом сама, вместо того
// чтобы синтетика продолжала доказывать свойство вчерашнего дерева.
package artifactgates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rtCorpusOf — производители для синтетической пробы. Перепись производителей по
// настоящему дереву проверяется гейтом отдельно; здесь предмет — разбор шага.
func rtCorpusOf(templates ...string) rtCorpus {
	blob := strings.Join(templates, "\x00")
	return rtCorpus{blob: blob, low: strings.ToLower(blob), n: len(templates)}
}

func rtSynthCorpus() rtCorpus {
	return rtCorpusOf(
		"Volume %s not found",
		"Illegal argument addressId",
		"volume with name %s already exists in project",
		"permission denied",
	)
}

func rtAudit(t *testing.T, corpus rtCorpus, folders ...nmItem) ([]rtFinding, rtCensus) {
	t.Helper()
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)
	f, cen, err := auditRefusalTone(dir, []string{rel}, func(string) rtCorpus { return corpus })
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

const rtURL = "{{baseUrl}}/storage/v1/volumes/{{garbageStorageId}}"

// ─── ось 1: расхождение регистра с производителем ────────────────────────────

func TestRT_FoldedLiteralDivergingFromProducerIsAFinding(t *testing.T) {
	step := nmStep("del-nx", "DELETE", rtURL,
		"pm.test('text', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('illegal argument addressid'));",
	)
	f, cen := rtAudit(t, rtSynthCorpus(), nmFolder("VOL-DEL-NEG — обычный заголовок без кавычек", step))
	if cen.foldedAssertions != 1 {
		t.Fatalf("гейт не увидел утверждения с приведением регистра: перепись %d", cen.foldedAssertions)
	}
	if len(f) == 0 {
		t.Fatal("расхождение регистра с производителем не дало находки — гейт не способен упасть на своём предмете")
	}
	var named bool
	for _, x := range f {
		if x.step != "del-nx" {
			t.Fatalf("находка не называет координату: %q", x.step)
		}
		// Находка обязана нести ДОСЛОВНОЕ написание производителя: без него
		// читатель не знает, чем чинить, и тратит на догадку целый прогон.
		if strings.Contains(x.why, `"Illegal argument addressId"`) {
			named = true
		}
	}
	if !named {
		t.Fatalf("находка не назвала написание производителя — чинить по ней нельзя: %v", f)
	}
}

// Законный близнец №1: написание производителя утверждается как есть.
func TestRT_ProducerSpellingWithoutFoldingIsLawfulAndSilent(t *testing.T) {
	step := nmStep("del-nx", "DELETE", rtURL,
		"pm.test('text', () => pm.expect(pm.response.json().message||'').to.eql('Illegal argument addressId'));",
	)
	f, cen := rtAudit(t, rtSynthCorpus(), nmFolder("VOL-DEL-NEG — обычный заголовок без кавычек", step))
	if len(f) != 0 {
		t.Fatalf("гейт покраснел на ЗАКОННОМ утверждении — он ловит форму, а не существо: %v", f)
	}
	if cen.steps != 1 {
		t.Fatalf("шаг гейтом не прочитан — молчание было бы слепотой: перепись шагов %d", cen.steps)
	}
}

// Законный близнец №2: ОТРИЦАНИЕ. Приведение регистра там РАСШИРЯЕТ проверку.
func TestRT_NegationWithFoldingIsLawfulAndSilent(t *testing.T) {
	step := nmStep("del-nx", "DELETE", rtURL,
		"pm.test('no leak', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.not.include('illegal argument addressid'));",
	)
	f, cen := rtAudit(t, rtSynthCorpus(), nmFolder("VOL-DEL-NEG — обычный заголовок без кавычек", step))
	if len(f) != 0 {
		t.Fatalf("гейт покраснел на отрицании, где приведение расширяет проверку: %v", f)
	}
	if cen.foldedAssertions != 0 {
		t.Fatalf("отрицание засчитано в осматриваемую совокупность — предмет гейта размыт: %d", cen.foldedAssertions)
	}
}

// Законный близнец №3 — ГРАНИЦА гейта, названная им самим: производитель пишет
// текст строчными, заголовок заглавных не объявлял, доказательства расхождения
// нет. Молчание здесь обязано быть ВИДНЫМ в переписи, иначе оно неотличимо от
// проверки этих мест.
func TestRT_FoldingOverAnAllLowercaseProducerIsOutOfScopeAndCounted(t *testing.T) {
	step := nmStep("get-nx", "GET", rtURL,
		"pm.test('text', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('permission denied'));",
	)
	f, cen := rtAudit(t, rtSynthCorpus(), nmFolder("AZD-GET-NEG — обычный заголовок без кавычек", step))
	if len(f) != 0 {
		t.Fatalf("гейт вышел за свою объявленную границу: %v", f)
	}
	if cen.foldedAssertions != 1 || cen.foldedWithoutProof != 1 {
		t.Fatalf("граница не попала в перепись (осмотрено %d, без доказательства %d) — "+
			"«ноль находок» стало бы неотличимо от «ноль прочитанного»",
			cen.foldedAssertions, cen.foldedWithoutProof)
	}
}

// ─── ось 2: заглавные, объявленные заголовком ────────────────────────────────

func TestRT_TitleDeclaredUppercaseUnderFoldingIsAFinding(t *testing.T) {
	step := nmStep("del-nx", "DELETE", rtURL,
		"pm.test('text', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('not found'));",
	)
	title := "VOL-DEL-NEG-NOTFOUND — Delete well-formed-но-нет volumeId → Operation error NOT_FOUND 'Volume <id> not found'"
	f, cen := rtAudit(t, rtSynthCorpus(), nmFolder(title, step))
	if cen.stepsWithDeclaredText != 1 {
		t.Fatalf("гейт не признал заголовок объявлением текста: перепись %d", cen.stepsWithDeclaredText)
	}
	if len(f) == 0 {
		t.Fatal("объявленные заголовком заглавные под приведением регистра не дали находки")
	}
	var named bool
	for _, x := range f {
		if strings.Contains(x.why, `"Volume"`) {
			named = true
		}
	}
	if !named {
		t.Fatalf("находка не назвала объявленную заглавными часть: %v", f)
	}
}

// Законный близнец: тот же заголовок, утверждение регистр НЕ приводит.
func TestRT_TitleDeclaredUppercaseAssertedVerbatimIsSilent(t *testing.T) {
	step := nmStep("del-nx", "DELETE", rtURL,
		"pm.test('text', () => pm.expect(pm.response.json().message||'').to.include('Volume '));",
	)
	title := "VOL-DEL-NEG-NOTFOUND — Delete well-formed-но-нет volumeId → Operation error NOT_FOUND 'Volume <id> not found'"
	f, cen := rtAudit(t, rtSynthCorpus(), nmFolder(title, step))
	if cen.stepsWithDeclaredText != 1 {
		t.Fatalf("шаг гейтом не прочитан — молчание было бы слепотой: перепись %d", cen.stepsWithDeclaredText)
	}
	if len(f) != 0 {
		t.Fatalf("гейт покраснел на утверждении БЕЗ приведения регистра: %v", f)
	}
}

// Законный близнец: в кавычках заголовка стоит ПРОЗА, а не текст отказа. Судить
// по ней значило бы краснеть на собственном объяснении.
func TestRT_QuotedProseInTitleIsNotADeclaredTextAndIsSilent(t *testing.T) {
	step := nmStep("del-nx", "DELETE", rtURL,
		"pm.test('text', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('permission denied'));",
	)
	title := "VOL-DEL-NEG — Delete тома 'без права на удаление' → отказ; см. 'разбор полосы → выше'"
	f, cen := rtAudit(t, rtSynthCorpus(), nmFolder(title, step))
	if cen.stepsWithDeclaredText != 0 {
		t.Fatalf("проза в кавычках принята за объявление текста отказа: перепись %d", cen.stepsWithDeclaredText)
	}
	if len(f) != 0 {
		t.Fatalf("гейт покраснел на прозе заголовка: %v", f)
	}
	if cen.foldedAssertions != 1 {
		t.Fatalf("шаг гейтом не прочитан — молчание было бы слепотой: перепись %d", cen.foldedAssertions)
	}
}

// ─── фикстура, привязанная к ДЕРЕВУ ──────────────────────────────────────────

// Синтетика доказывает свойство разбора; эта проба доказывает, что разбор
// применим к ТОМУ ЖЕ дереву, которое судит гейт: заголовок берётся из
// закоммиченных коллекций, текст — из производителей дерева.
func TestRT_RestoredDefectOnARealTreeTitleIsAFinding(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	byOwner, err := rtProducers(root, optGoFiles(tt))
	if err != nil {
		t.Fatal(err)
	}
	cols := optCollections(tt)
	if len(cols) == 0 {
		t.Fatal("в индексе git нет коллекций newman — фикстуре не на чем стоять")
	}

	// Ищем НАСТОЯЩИЙ заголовок, объявляющий текст с заглавными, который дерево
	// действительно производит. Ноль таких — сам по себе находка: значит форма
	// записи заголовков сменилась и вид 2 больше не проверяется ни на чём.
	title, part, owner := rtFindDeclaringTitle(t, root, cols, byOwner)
	step := nmStep("assert-op-error", "GET", "{{baseUrl}}/operations/{{opId}}",
		"pm.test('text', () => pm.expect((j.error && j.error.message || '').toLowerCase()).to.include('not found'));",
	)
	f, cen := rtAudit(t, rtCorpusFor(byOwner, owner), nmFolder(title, step))
	if cen.stepsWithDeclaredText != 1 {
		t.Fatalf("настоящий заголовок %q гейт не признал объявлением текста", title)
	}
	if len(f) == 0 {
		t.Fatalf("возвращённый дефект на НАСТОЯЩЕМ заголовке %q (объявляет %q) не дал находки", title, part)
	}
}

// rtFindDeclaringTitle — первый заголовок дерева, объявляющий текст с заглавными,
// который производится этим же деревом.
func rtFindDeclaringTitle(t *testing.T, root string, cols []string, byOwner map[string]map[string]bool) (string, string, string) {
	t.Helper()
	for _, rel := range cols {
		owner := rtOwner(rel)
		corpus := rtCorpusFor(byOwner, owner)
		for _, title := range rtCollectionTitles(t, root, rel) {
			for _, d := range rtDeclaredTexts(title) {
				for _, p := range rtConstParts(d) {
					if rtHasUpper(p) && strings.Contains(corpus.blob, p) {
						return title, p, owner
					}
				}
			}
		}
	}
	t.Fatal("в закоммиченных коллекциях НЕТ ни одного заголовка, объявляющего текст отказа с " +
		"заглавными, который дерево производит — вид 2 гейта не проверяется ни на чём, и это " +
		"находка, а не повод выйти успехом")
	return "", "", ""
}

// rtCollectionTitles — заголовки кейсов коллекции, ровно в том виде, в каком их
// читает гейт: внешняя папка несёт заголовок кейса.
func rtCollectionTitles(t *testing.T, root, rel string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
	if err != nil {
		t.Fatal(err)
	}
	var col nmCollection
	if err := json.Unmarshal(b, &col); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range col.Item {
		if it.isFolder() && it.Name != "" {
			out = append(out, it.Name)
		}
	}
	return out
}

// ─── ось 3: утверждение УЖЕ объявленного заголовком текста ───────────────────
//
// Ось заведена потому, что оси 1 и 2 её предмета не видят: расхождения регистра
// нет (утверждается настоящая часть текста), заглавных в объявлении нет тоже —
// а шаг всё равно проверяет меньше, чем объявил. Ровно этой формой записан
// `NLB-UPD-STATE-IMMUTABLE-TYPE`: объявлено «type is immutable», утверждается
// «immutable», и сообщение о неизменяемости ЛЮБОГО поля проходит.

func rtImmutableCorpus() rtCorpus {
	return rtCorpusOf(
		"type is immutable after NetworkLoadBalancer.Create",
		"region_id is immutable after NetworkLoadBalancer.Create",
	)
}

const rtImmutableTitle = "NLB-UPD-STATE-IMMUTABLE-TYPE — Update with mask=type " +
	"InvalidArgument 'type is immutable' (Verifies REQ-NLB-IMMUTABLE-TYPE)"

func TestRT_AssertingLessThanTheTitleDeclaredIsAFinding(t *testing.T) {
	step := nmStep("upd-type", "PATCH", rtURL,
		"pm.test('text', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('immutable'));",
	)
	f, cen := rtAudit(t, rtImmutableCorpus(), nmFolder(rtImmutableTitle, step))
	if cen.stepsWithDeclaredText != 1 {
		t.Fatalf("гейт не признал заголовок объявлением текста: перепись %d", cen.stepsWithDeclaredText)
	}
	if len(f) == 0 {
		t.Fatal("утверждение СТРОГОЙ части объявленного текста не дало находки — " +
			"гейт не способен упасть на предмете оси 3")
	}
	// Находка обязана прийти ИМЕННО от оси 3: если бы её дали оси 1 или 2,
	// молчание оси 3 осталось бы недоказанным, а прогон — зелёным.
	var byAxis3 bool
	for _, x := range f {
		if strings.Contains(x.why, "строгая её часть") {
			byAxis3 = true
		}
	}
	if !byAxis3 {
		t.Fatalf("красное пришло от другой оси, ось 3 осталась недоказанной: %v", f)
	}
}

// Законный близнец: тот же заголовок и то же приведение регистра, но
// утверждается ВЕСЬ объявленный текст. Проверять больше объявленного шаг не
// обязан, поэтому гейт молчит.
func TestRT_AssertingExactlyWhatTheTitleDeclaredIsSilent(t *testing.T) {
	step := nmStep("upd-type", "PATCH", rtURL,
		"pm.test('text', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('type is immutable'));",
	)
	f, cen := rtAudit(t, rtImmutableCorpus(), nmFolder(rtImmutableTitle, step))
	if cen.foldedAssertions != 1 {
		t.Fatalf("шаг гейтом не прочитан — молчание было бы слепотой: перепись %d", cen.foldedAssertions)
	}
	if len(f) != 0 {
		t.Fatalf("гейт покраснел на утверждении ВСЕГО объявленного текста: %v", f)
	}
}

// Объявление вправе НАЧИНАТЬСЯ с подстановки. Прежняя редакция требовала первой
// буквы и такие объявления отвергала целиком — вид, записанный этой формой, был
// не находкой, а невидимостью.
func TestRT_DeclarationOpeningWithASubstitutionIsStillRead(t *testing.T) {
	corpus := rtCorpusOf(
		"bootSource name/resolvedDigest/materializedVolume are output-only and must not be set on input",
	)
	title := "INST-RD-CR-VAL-BOOTSOURCE-OUTPUT-FIELDS — Create с output-only полем в теле " +
		"sync 400 '... output-only and must not be set on input' (server-derived)"
	step := nmStep("cr-boot-out", "POST", rtURL,
		"pm.test('text', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('output-only'));",
	)
	f, cen := rtAudit(t, corpus, nmFolder(title, step))
	if cen.stepsWithDeclaredText != 1 {
		t.Fatalf("объявление, начинающееся с подстановки, гейтом не прочитано: перепись %d",
			cen.stepsWithDeclaredText)
	}
	if len(f) == 0 {
		t.Fatal("объявление прочитано, но находки нет — ось 3 на этой форме слепа")
	}
}

// ─── распознаватель ПРОИЗВОДИТЕЛЕЙ ───────────────────────────────────────────
//
// Текст, которого распознаватель не прочитал, для гейта не существует: у
// утверждения о нём «производителя нет», и все три оси молчат. Поэтому каждая
// форма записи отказа в этом дереве проверяется отдельно, а рядом стоит
// контроль — проза в комментарии производителем НЕ становится, иначе гейт
// доказывал бы предпосылку её собственным пересказом.
func TestRT_ProducerRecognizerReadsEveryFormInThisTree(t *testing.T) {
	dir := t.TempDir()
	rel := filepath.Join("services", "nlb", "internal", "producer.go")
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package p

// Комментарий: "prose only in a comment" производителем не является.
var immutableUpdateFields = map[string]string{
	"type": "type is immutable after NetworkLoadBalancer.Create",
}

func f(fieldName string) error {
	if a {
		return status.Error(codes.InvalidArgument, "plain status text")
	}
	if b {
		return fmt.Errorf("wrapped errorf text")
	}
	if c {
		return serviceerr.InvalidArg("boot_source", "single line invalidarg text")
	}
	if d {
		return serviceerr.InvalidArg("boot_source",
			"text carried onto the next line")
	}
	if e {
		return serviceerr.InvalidArg(fieldName, "field name is a variable here")
	}
	return corevalidate.ResourceID("Image", "img", x)
}
`
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	byOwner, err := rtProducers(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	got := byOwner["nlb"]
	for _, want := range []string{
		"type is immutable after NetworkLoadBalancer.Create", // таблица
		"plain status text",               // status.Error
		"wrapped errorf text",             // fmt.Errorf
		"single line invalidarg text",     // InvalidArg, имя поля литералом
		"text carried onto the next line", // тот же вызов, перенос строки
		"field name is a variable here",   // InvalidArg, имя поля переменной
		"invalid Image id '%s'",           // проверка формата чужого id
	} {
		if !got[want] {
			t.Errorf("распознаватель производителей не прочитал форму %q — "+
				"всё записанное ею осталось бы вне наблюдения, а не находкой", want)
		}
	}
	// Контроль: без него проба зеленела бы на распознавателе, который считает
	// производителем каждую строку файла.
	for bad := range got {
		if strings.Contains(bad, "prose only in a comment") {
			t.Errorf("проза комментария принята за производителя (%q) — гейт краснел бы "+
				"на собственном объяснении", bad)
		}
	}
	if len(got) == 0 {
		t.Fatal("производителей ноль — распознаватель ослеп, и «расхождения нет» стало бы " +
			"верно для КАЖДОГО утверждения дерева")
	}
}
