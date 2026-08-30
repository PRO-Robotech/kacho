// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт дословности текста полосы операций СПОСОБЕН
// упасть — и что падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву
// (`auditOperationLaneText`): проба, повторяющая логику своей копией,
// доказывала бы свойство копии, а не гейта.
//
// У КАЖДОГО ОТРИЦАНИЯ ЗДЕСЬ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. «Находок ноль»
// само по себе неотличимо от «распознаватель ослеп», поэтому каждая молчащая
// проба дополнительно утверждает, что гейт шаг УВИДЕЛ (перепись `opsWithTopMessage`)
// и промолчал по существу.
//
// ФИКСТУРА ПРИВЯЗАНА К ДЕРЕВУ, А НЕ К ПАМЯТИ АВТОРА: отдельная проба берёт
// НАСТОЯЩИЙ шаг полосы из закоммиченных коллекций, возвращает ему снятый дефект
// и требует красного. Сменится форма записи утверждений в генераторах — эта
// проба скажет об этом сама, вместо того чтобы синтетика продолжала доказывать
// свойство вчерашнего дерева.
package artifactgates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// optLaneTemplates — шаблоны полосы для синтетических проб. Те же, что даёт
// перепись на этом дереве; перепись производителей проверяется ОТДЕЛЬНОЙ пробой
// на настоящем дереве, а здесь предмет — разбор шагов.
func optLaneTemplates() map[string]bool {
	return map[string]bool{
		`operation %s not found`:         true,
		`operation %s already completed`: true,
		`invalid operation id "%s"`:      true,
		`permission denied`:              true,
	}
}

func optAudit(t *testing.T, folders ...nmItem) ([]optFinding, optCensus) {
	t.Helper()
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)
	f, cen, err := auditOperationLaneText(dir, []string{rel}, optLaneTemplates())
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

const optOpsURLFixture = "{{baseUrl}}/operations/{{garbageOpId}}"

// ─── ось 1: приведение регистра ──────────────────────────────────────────────

func TestOPT_LowercasedMessageAssertionIsAFinding(t *testing.T) {
	step := nmStep("get-nx", "GET", optOpsURLFixture,
		"pm.test('text matches', () => pm.expect(pm.response.json().message.toLowerCase()).to.include('not found'));",
	)
	f, cen := optAudit(t, nmFolder("OP-GET-CONF-NF-TEXT", step))
	if cen.opsWithTopMessage != 1 {
		t.Fatalf("гейт не увидел утверждения о сообщении полосы: перепись %d", cen.opsWithTopMessage)
	}
	if len(f) == 0 {
		t.Fatal("приведение к нижнему регистру не дало находки — гейт не способен упасть на своём предмете")
	}
	var sawLower bool
	for _, x := range f {
		if strings.Contains(x.why, "приведено к нижнему регистру") {
			sawLower = true
		}
		if x.step != "get-nx" {
			t.Fatalf("находка не называет координату: %q", x.step)
		}
	}
	if !sawLower {
		t.Fatalf("находка не называет предмет (регистр): %v", f)
	}
}

// Законный близнец №1: равенство вычисленному тексту, регистр сохранён.
func TestOPT_VerbatimEqualityIsLawfulAndSilent(t *testing.T) {
	step := nmStep("get-nx", "GET", optOpsURLFixture,
		"pm.test('сообщение дословно равно тексту владельца', () => "+
			"pm.expect(pm.response.json().message).to.eql('operation ' + pm.environment.get('garbageOpId') + ' not found'));",
	)
	f, cen := optAudit(t, nmFolder("OP-GET-CONF-NF-TEXT", step))
	if cen.opsWithTopMessage != 1 {
		t.Fatalf("гейт не увидел законного утверждения — молчание было бы слепотой: перепись %d",
			cen.opsWithTopMessage)
	}
	if len(f) != 0 {
		t.Fatalf("гейт покраснел на ЗАКОННОМ утверждении — он ловит форму, а не существо: %v", f)
	}
}

// Законный близнец №2: ОТРИЦАНИЕ. Приведение регистра там РАСШИРЯЕТ проверку,
// поэтому запрет к нему не относится — и это сказано в шапке гейта.
func TestOPT_NegationWithLowercaseIsLawfulAndSilent(t *testing.T) {
	step := nmStep("get-nx", "GET", optOpsURLFixture,
		"pm.test('сообщение дословно равно тексту владельца', () => "+
			"pm.expect(pm.response.json().message).to.eql('operation ' + pm.environment.get('garbageOpId') + ' not found'));",
		"pm.test('нет утечки', () => pm.expect(pm.response.text().toLowerCase()).to.not.include('sqlstate'));",
	)
	f, _ := optAudit(t, nmFolder("OP-GET-CONF-NF-TEXT", step))
	if len(f) != 0 {
		t.Fatalf("гейт покраснел на отрицании, где приведение расширяет проверку: %v", f)
	}
}

// ─── ось 2: вхождение вместо равенства ───────────────────────────────────────

func TestOPT_SubstringWithoutEqualityIsAFinding(t *testing.T) {
	step := nmStep("cancel-done", "POST", "{{baseUrl}}/operations/{{opId}}:cancel",
		"pm.test('mentions already completed', () => pm.expect(pm.response.json().message).to.include('already completed'));",
	)
	f, cen := optAudit(t, nmFolder("OP-CANCEL-NEG-ALREADY-DONE", step))
	if cen.opsWithTopMessage != 1 {
		t.Fatalf("гейт не увидел шага: перепись %d", cen.opsWithTopMessage)
	}
	var sawSubstring bool
	for _, x := range f {
		if strings.Contains(x.why, "ВХОЖДЕНИЕ подстроки") {
			sawSubstring = true
		}
	}
	if !sawSubstring {
		t.Fatalf("вхождение без равенства не дало своей находки: %v", f)
	}
}

// ─── ось 3: текст без производителя ──────────────────────────────────────────

func TestOPT_TextWithNoProducerIsAFinding(t *testing.T) {
	step := nmStep("get-garbage", "GET", optOpsURLFixture,
		"pm.test('текст края', () => pm.expect(pm.response.json().message).to.eql('operation id has an unknown prefix'));",
	)
	f, _ := optAudit(t, nmFolder("OP-GET-NEG-UNKNOWN-PREFIX", step))
	var sawNoProducer bool
	for _, x := range f {
		if strings.Contains(x.why, "не производит НИ ОДИН шаблон полосы") {
			sawNoProducer = true
		}
	}
	if !sawNoProducer {
		t.Fatalf("текст без производителя не дал находки — гейт не закрывает класс #1400: %v", f)
	}
}

// Законный близнец №3: тот же вид записи, но текст ПРОИЗВОДИТСЯ — с уже
// подставленным значением, как пишется ожидание при литеральном id в пути.
func TestOPT_SubstitutedTextWithProducerIsLawfulAndSilent(t *testing.T) {
	step := nmStep("get-malformed-op", "GET", "{{baseUrl}}/operations/garbage-not-an-op-id",
		"pm.test('текст края', () => pm.expect(pm.response.json().message).to.eql('invalid operation id \"garbage-not-an-op-id\"'));",
	)
	f, cen := optAudit(t, nmFolder("GOP-GET-VAL-MALFORMED", step))
	if cen.opsWithTopMessage != 1 {
		t.Fatalf("гейт не увидел шага — молчание было бы слепотой: перепись %d", cen.opsWithTopMessage)
	}
	if len(f) != 0 {
		t.Fatalf("гейт покраснел на тексте, который производитель ПРОИЗВОДИТ: %v", f)
	}
}

// ─── ось 4: чужой предмет не судится ─────────────────────────────────────────

// Сообщение ВЛАДЕЛЬЦА ресурса внутри тела операции производит домен, а не
// полоса; вычислить его двумя каталогами нельзя, и гейт о нём не высказывается.
// Без этой пробы граница была бы объявлением, а не свойством.
func TestOPT_OwnerErrorMessageIsNotJudged(t *testing.T) {
	step := nmStep("poll-op", "GET", "{{baseUrl}}/operations/{{opId}}",
		"const j = pm.response.json();",
		"pm.test('op error text', () => pm.expect((j.error && j.error.message || '').toLowerCase()).to.include('not found'));",
	)
	f, cen := optAudit(t, nmFolder("NLB-CR-NEG-REGION-UNKNOWN", step))
	if len(f) != 0 {
		t.Fatalf("гейт высказался о сообщении ВЛАДЕЛЬЦА, текст которого он не вычисляет: %v", f)
	}
	if cen.opsWithOwnerErrorOnly != 1 {
		t.Fatalf("шаг с сообщением владельца не попал в свою величину переписи (%d) — "+
			"тогда «ноль находок» покрывало бы то, чего гейт не читал", cen.opsWithOwnerErrorOnly)
	}
}

// Шаг ВНЕ полосы операций не судится: тексты доменных ресурсов вычисляются не здесь.
func TestOPT_NonOperationStepIsNotJudged(t *testing.T) {
	step := nmStep("get-nx-net", "GET", "{{baseUrl}}/vpc/v1/networks/{{garbageNetworkId}}",
		"pm.test('mentions not found', () => pm.expect(pm.response.json().message.toLowerCase()).to.include('not found'));",
	)
	f, cen := optAudit(t, nmFolder("NET-GET-NEG-NOTFOUND", step))
	if len(f) != 0 {
		t.Fatalf("гейт вышел за объявленную границу — судит шаг вне полосы операций: %v", f)
	}
	if cen.opsSteps != 0 {
		t.Fatalf("шаг вне полосы засчитан в полосу: перепись %d", cen.opsSteps)
	}
}

// ─── ось 5: фикстура, привязанная к ДЕРЕВУ ───────────────────────────────────

// Берёт НАСТОЯЩИЙ шаг закоммиченной коллекции и возвращает ему снятый дефект.
// Синтетика доказывает разбор; эта проба доказывает, что разбор применим к той
// форме записи, которую производят генераторы СЕГОДНЯ.
func TestOPT_RealCollectionStepWithTheDefectBackIsAFinding(t *testing.T) {
	root := repoRoot(t)
	rel := filepath.Join("services", "vpc", "tests", "newman", "collections", "operation.postman_collection.json")
	raw, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь фиксирован в этой пробе
	if err != nil {
		t.Fatalf("чтение %s: %v", rel, err)
	}
	// Дефект возвращается ровно в той форме, в какой он стоял до починки.
	before := `pm.expect(pm.response.json().message).to.eql('operation ' + pm.environment.get('garbageVpcId') + ' not found')`
	after := `pm.expect(pm.response.json().message.toLowerCase()).to.include('not found')`
	if !strings.Contains(string(raw), before) {
		t.Fatalf("в %s больше нет починенного утверждения — фикстура пережила свой предмет: "+
			"либо кейс переписан, либо коллекция не перегенерирована", rel)
	}
	injected := strings.Replace(string(raw), before, after, 1)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "col.json"), []byte(injected), 0o600); err != nil {
		t.Fatal(err)
	}
	f, cen, err := auditOperationLaneText(dir, []string{"col.json"}, optLaneTemplates())
	if err != nil {
		t.Fatal(err)
	}
	if cen.opsWithTopMessage == 0 {
		t.Fatal("на настоящей коллекции гейт не увидел ни одного утверждения о сообщении полосы")
	}
	var sawLower bool
	for _, x := range f {
		if strings.Contains(x.why, "приведено к нижнему регистру") && x.step == "get-vpc-garbage" {
			sawLower = true
		}
	}
	if !sawLower {
		t.Fatalf("возвращённый в НАСТОЯЩУЮ коллекцию дефект не найден — гейт разбирает не ту "+
			"форму, которую производят генераторы: %v", f)
	}

	// Контроль в обратную сторону: та же коллекция БЕЗ инъекции обязана молчать.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "col.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	f2, _, err := auditOperationLaneText(dir2, []string{"col.json"}, optLaneTemplates())
	if err != nil {
		t.Fatal(err)
	}
	if len(f2) != 0 {
		t.Fatalf("настоящая коллекция без инъекции даёт находки — красное приходит не от инъекции: %v", f2)
	}
}

// ─── ось 6: предпосылка ──────────────────────────────────────────────────────

// Перепись производителей на настоящем дереве обязана быть непустой и покрывать
// оба каталога. Утверждение «текст вычислен» иначе становится памятью автора.
func TestOPT_ProducerCensusOnTheRealTreeIsNonEmpty(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	templates, covered, err := optTemplates(root, optGoFiles(tt))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range optProducerDirs {
		if !covered[d] {
			t.Fatalf("каталог производителей %s не покрыт переписью", d)
		}
	}
	if len(templates) == 0 {
		t.Fatal("перепись производителей пуста — гейт судил бы по своей памяти")
	}
	// Текст «нет такой операции» обязан быть среди вычисленных: он и есть предмет,
	// из-за которого гейт заведён.
	if !templates["operation %s not found"] {
		t.Fatalf("среди вычисленных шаблонов нет текста «нет такой операции»; вычислено: %v", templates)
	}
	// И глагол `%q` обязан быть раскрыт — иначе верное ожидание с кавычками
	// объявлялось бы находкой.
	if !templates[`invalid operation id "%s"`] {
		t.Fatalf("глагол %%q не раскрыт в кавычки; вычислено: %v", templates)
	}
}

// Пустая перепись шаблонов — отказ, а не «ноль находок»: иначе всякий текст
// кейса оказался бы «без производителя», и гейт покраснел бы на всём дереве.
func TestOPT_EmptyTemplateCensusMakesEveryTextAFinding(t *testing.T) {
	step := nmStep("get-nx", "GET", optOpsURLFixture,
		"pm.test('дословно', () => pm.expect(pm.response.json().message).to.eql('operation ' + pm.environment.get('garbageOpId') + ' not found'));",
	)
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", nmFolder("CASE", step))
	f, _, err := auditOperationLaneText(dir, []string{rel}, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) == 0 {
		t.Fatal("на пустой переписи шаблонов гейт промолчал — значит проверка производителя " +
			"не исполняется вовсе, и предпосылка гейта ничем не держится")
	}
	// Ради этого проба и стоит: раз на пустой переписи краснеет ВСЁ, гейт по дереву
	// обязан на ней отказываться судить — и он отказывается `t.Fatal`-ом, а не
	// выходит успехом с «ноль находок».
}
