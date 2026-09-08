// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт производителя текста отказа СПОСОБЕН упасть — и
// что падает он на своём предмете, а не на чужом и не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву
// (`auditRefusalProducer`): проба, повторяющая логику своей копией, доказывала
// бы свойство копии.
//
// # ТРИ ПРОГОНА, А НЕ ДВА
//
// Инъекция обязана ронять ТОЛЬКО проверяемое (`testing.md` §«Гейт на класс»,
// п. 2в). Гейт живёт рядом с гейтом ТОНА и читает те же шаги, поэтому
// доказательство идёт тремя прогонами на одной и той же фикстуре:
//
//	контроль              — всё цело: молчат ОБА;
//	инъекция нового       — текст без производителя: краснеет ТОЛЬКО новый;
//	инъекция существующего — расхождение регистра: краснеет ТОЛЬКО гейт тона.
//
// Без третьего прогона молчание гейта тона неотличимо от молчания мёртвого.
//
// # У КАЖДОГО ОТРИЦАНИЯ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ
//
// «Находок ноль» само по себе неотличимо от «распознаватель ослеп», поэтому
// каждая молчащая проба дополнительно утверждает, что шаг гейтом УВИДЕН
// (перепись объявленных утверждений) и что он промолчал по существу.
//
// # ФИКСТУРА ПОСЛЕДНЕЙ ПРОБЫ ПРИВЯЗАНА К ДЕРЕВУ, А НЕ К ПАМЯТИ АВТОРА
//
// Она берёт НАСТОЯЩИЕ производители дерева и снимает из них один текст — ровно
// то, что делает осознанная правка тона, — и требует красного. Сменится форма
// записи производителей — проба скажет об этом сама, вместо того чтобы синтетика
// продолжала доказывать свойство вчерашнего дерева.
package artifactgates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rpSynthCorpus — производители синтетической пробы: тексты владельца в тех же
// формах, в каких их пишет дерево.
func rpSynthCorpus() rtCorpus {
	return rpCorpusOf(
		"Network with name %s already exists",
		"Volume %s not found",
		"invalid membership id '%s'",
		"Illegal argument addressId",
		// Шаблон без постоянной части: он описывает ЛЮБОЕ сообщение и в счёт
		// идти не должен (см. rpMinAnchor).
		"%w: %s",
	)
}

func rpCorpusOf(templates ...string) rtCorpus {
	blob := strings.Join(templates, "\x00")
	return rtCorpus{blob: blob, low: strings.ToLower(blob), n: len(templates)}
}

func rpAudit(t *testing.T, corpus rtCorpus, folders ...nmItem) ([]rpFinding, rpCensus) {
	t.Helper()
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)
	f, cen, err := auditRefusalProducer(dir, []string{rel}, func(string) rtCorpus { return corpus })
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

const rpURL = "{{baseUrl}}/vpc/v1/networks"

// rpEql / rpHas — то, что порождает общий слой генератора: обе формы помощника.
func rpEql(text string) string {
	return "  pm.expect(j.message, JSON.stringify(j)).to.eql('" + text + "');"
}

func rpHas(text string) string {
	return "  pm.expect(j.message, JSON.stringify(j)).to.have.string('" + text + "');"
}

// rpHasEnv — та же форма вхождения, но с местом подстановки окружения посередине:
// именно так помощник собирает текст, в котором стоит `{{имя}}`.
func rpHasEnv(head, tail string) string {
	return "  pm.expect(j.message, JSON.stringify(j)).to.have.string('" + head +
		"' + pm.environment.get('runId') + '" + tail + "');"
}

// ─── ТРИ ПРОГОНА ОДНОЙ ФИКСТУРЫ ──────────────────────────────────────────────

// rpLaneFolder — шаг, у которого ОБА свойства целы: текст производится деревом и
// утверждается его написанием, без приведения регистра.
func rpLaneFolder(assertion string) nmItem {
	return nmFolder("NET-CR-NEG-DUP — Create с занятым именем → 'Network with name … already exists'",
		nmStep("create-dup", "POST", rpURL, assertion))
}

func TestRP_Run1_ControlBothGatesSilent(t *testing.T) {
	folder := rpLaneFolder(rpEql("Network with name net-dup- already exists"))

	f, cen := rpAudit(t, rpSynthCorpus(), folder)
	if cen.declared() != 1 {
		t.Fatalf("новый гейт не увидел объявленного утверждения: перепись равенством %d, вхождением %d",
			cen.eqlAsserts, cen.containsAsserts)
	}
	if len(f) != 0 {
		t.Fatalf("новый гейт покраснел на целой фикстуре: %v", f)
	}

	tf, tcen := rtAudit(t, rpSynthCorpus(), folder)
	if len(tf) != 0 {
		t.Fatalf("гейт тона покраснел на целой фикстуре: %v", tf)
	}
	_ = tcen
}

func TestRP_Run2_MissingProducerRedsOnlyTheNewGate(t *testing.T) {
	// Осознанная правка тона: продукт развёл слитый отказ, кейс пинит прежний.
	folder := rpLaneFolder(rpEql("referenced resource not found or still in use"))

	f, cen := rpAudit(t, rpSynthCorpus(), folder)
	if cen.declared() != 1 {
		t.Fatalf("гейт не увидел объявленного утверждения: перепись %d", cen.declared())
	}
	if len(f) == 0 {
		t.Fatal("текст без производителя не дал находки — гейт не способен упасть на своём предмете")
	}
	if len(cen.unproven) != 0 {
		t.Fatalf("исход «не установлено» на тексте, у которого ничего похожего нет: %v", cen.unproven)
	}
	var named bool
	for _, x := range f {
		if x.step != "create-dup" {
			t.Fatalf("находка не называет координату: %q", x.step)
		}
		// Находка обязана нести объявленный текст: без него читатель не знает,
		// что чинить, и тратит на догадку целый прогон.
		if strings.Contains(x.String(), "referenced resource not found or still in use") {
			named = true
		}
	}
	if !named {
		t.Fatalf("находка не назвала объявленного текста — чинить по ней нельзя: %v", f)
	}

	// А гейт тона обязан промолчать: приведения регистра здесь нет, его предмета
	// эта инъекция не касается.
	tf, _ := rtAudit(t, rpSynthCorpus(), folder)
	if len(tf) != 0 {
		t.Fatalf("инъекция уронила ЧУЖОЙ гейт — красное пришло бы от соседа, "+
			"и вакуумность нового осталась бы недоказанной: %v", tf)
	}
}

func TestRP_Run3_CaseDivergenceRedsOnlyTheToneGate(t *testing.T) {
	// Расхождение регистра — предмет СОСЕДА. Производитель у текста есть.
	folder := nmFolder("VOL-DEL-NEG — обычный заголовок без кавычек",
		nmStep("del-nx", "DELETE", rpURL,
			"pm.test('text', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('illegal argument addressid'));"))

	tf, _ := rtAudit(t, rpSynthCorpus(), folder)
	if len(tf) == 0 {
		t.Fatal("гейт тона промолчал на своём предмете — его молчание в прогоне 2 " +
			"неотличимо от молчания мёртвого, и доказательство рассыпается")
	}

	f, _ := rpAudit(t, rpSynthCorpus(), folder)
	if len(f) != 0 {
		t.Fatalf("новый гейт покраснел на ЧУЖОМ предмете — расхождение регистра судит сосед: %v", f)
	}
}

// ─── ось: обе формы помощника ────────────────────────────────────────────────

func TestRP_ContainsFormIsRecognisedAndLawfulWindowIsSilent(t *testing.T) {
	// Вхождением утверждается ОКНО в тексте владельца: сообщение доезжает с
	// хвостом, который статически не вычисляется.
	folder := rpLaneFolder(rpHas(" already exists"))
	f, cen := rpAudit(t, rpSynthCorpus(), folder)
	if cen.containsAsserts != 1 {
		t.Fatalf("форма вхождения не распознана: перепись равенством %d, вхождением %d — "+
			"форма, о которой распознаватель не знает, не находка, а невидимость",
			cen.eqlAsserts, cen.containsAsserts)
	}
	if len(f) != 0 {
		t.Fatalf("законное окно в тексте владельца объявлено находкой: %v", f)
	}
}

func TestRP_ContainsFormWithNoProducerIsAFinding(t *testing.T) {
	folder := rpLaneFolder(rpHas("still in use by another tenant"))
	f, cen := rpAudit(t, rpSynthCorpus(), folder)
	if cen.containsAsserts != 1 {
		t.Fatalf("форма вхождения не распознана: перепись %d", cen.containsAsserts)
	}
	if len(f) == 0 {
		t.Fatal("окно, которого владелец не производит, не дало находки — форма вхождения не проверяется ни на чём")
	}
}

// ─── ось: обе формы кавычек ──────────────────────────────────────────────────

func TestRP_DoubleQuotedDeclarationIsReadWhole(t *testing.T) {
	// Сериализатор общего слоя берёт двойные кавычки, как только текст несёт
	// апостроф. Распознаватель, знающий одни одинарные, вычитал бы отсюда
	// внутренний кусок `'not-an-id'` и объявил бы находку на исправном кейсе.
	folder := rpLaneFolder(`  pm.expect(j.message, JSON.stringify(j)).to.eql("invalid membership id \'not-an-id\'");`)
	f, cen := rpAudit(t, rpSynthCorpus(), folder)
	if cen.eqlAsserts != 1 {
		t.Fatalf("утверждение в двойных кавычках не распознано: перепись %d", cen.eqlAsserts)
	}
	if len(f) != 0 {
		t.Fatalf("исправный кейс в двойных кавычках объявлен находкой — распознаватель читает "+
			"внутренний апострофированный кусок вместо всего текста: %v", f)
	}
}

// ─── ось: значение, написанное ЛИТЕРАЛОМ, поглощается глаголом формата ───────

func TestRP_LiteralSubstitutionIsAbsorbedByTheVerb(t *testing.T) {
	// Кейс вправе написать подставляемое значение литералом, когда оно
	// постоянно. Мерка по ДОЛЕ покрытия объявляла здесь находку при полном
	// совпадении по существу — потому доля и заменена пересечением образцов.
	folder := rpLaneFolder(rpEql("Volume vol00000000000000000 not found"))
	f, cen := rpAudit(t, rpSynthCorpus(), folder)
	if cen.eqlAsserts != 1 {
		t.Fatalf("утверждение не распознано: перепись %d", cen.eqlAsserts)
	}
	if len(f) != 0 {
		t.Fatalf("литеральное значение на месте глагола формата объявлено находкой: %v", f)
	}
}

// ─── ось: шаблон без постоянной части в счёт не идёт ─────────────────────────

func TestRP_ProducerWithoutAConstantAnchorDoesNotCoverAnything(t *testing.T) {
	// В корпусе есть `%w: %s` — образец, описывающий любое сообщение. Если он
	// пойдёт в счёт, гейт станет вакуумным: покрыто будет всё.
	folder := rpLaneFolder(rpEql("subject: quota exceeded for tenant"))
	f, _ := rpAudit(t, rpCorpusOf("%w: %s"), folder)
	if len(f) == 0 {
		t.Fatal("шаблон без постоянной части покрыл произвольный текст — гейт вакуумен: " +
			"при таком корпусе он не может покраснеть НИ НА ЧЁМ")
	}
}

// ─── ось: близкий производитель даёт «не установлено», а не находку ──────────

func TestRP_NearProducerYieldsUnprovenNotAFinding(t *testing.T) {
	// Текст, собранный из аргументов: слово «region» приезжает значением, а не
	// шаблоном. Доказательства нет ни в одну сторону — и это отдельный исход.
	folder := rpLaneFolder(rpHasEnv("region ", " not found"))
	f, cen := rpAudit(t, rpCorpusOf("%w: %s %s not found", "Volume %s not found"), folder)
	if len(f) != 0 {
		t.Fatalf("текст, у которого есть близкий производитель, объявлен ДОКАЗАННОЙ находкой: %v", f)
	}
	if len(cen.unproven) != 1 {
		t.Fatalf("исход «не установлено» не назван числом: %d — тогда он неотличим от покрытого",
			len(cen.unproven))
	}
}

// ─── ось: фикстура привязана к ДЕРЕВУ ────────────────────────────────────────

func TestRP_RemovingARealProducerFromTheTreeRedsTheGate(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	byOwner, err := rtProducers(root, optGoFiles(tt))
	if err != nil {
		t.Fatal(err)
	}
	const owner = "vpc"
	const declared = "Subnet subnonexistent000001 not found"
	folder := rpLaneFolder(rpEql(declared))

	// С деревом как есть гейт молчит: текст производится.
	if f, _ := rpAudit(t, rtCorpusFor(byOwner, owner), folder); len(f) != 0 {
		t.Fatalf("гейт покраснел на настоящем тексте настоящего дерева: %v", f)
	}

	// Снимаем ВСЕХ производителей этого текста — ровно то, что делает осознанная
	// правка тона, разводящая слитый отказ на две полосы. Снимается по
	// ПЕРЕСЕЧЕНИЮ ОБРАЗЦОВ, а не по памяти автора: перечень производителей
	// дерева меняется, и фикстура, выписавшая один текст, доказывала бы свойство
	// вчерашнего дерева.
	removed := 0
	without := map[string]map[string]bool{}
	for o, texts := range byOwner {
		without[o] = map[string]bool{}
		for x := range texts {
			if (o == owner || o == rtSharedOwner) && rpIntersects(declared, x, false) {
				removed++
				continue
			}
			without[o][x] = true
		}
	}
	if removed == 0 {
		t.Fatalf("в дереве нет НИ ОДНОГО производителя текста %q — фикстура беспредметна: "+
			"снимать нечего, и красное доказывало бы не то", declared)
	}

	f, cen := rpAudit(t, rtCorpusFor(without, owner), folder)
	if len(f) == 0 {
		t.Fatalf("снятие %d настоящих производителей из дерева не дало находки — "+
			"гейт не поймал бы РОВНО того инцидента, ради которого заведён (не установлено: %d)",
			removed, len(cen.unproven))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ФОРМА, КОТОРОЙ РАСПОЗНАВАТЕЛЬ ДОЛГО НЕ ЗНАЛ: текст отказа объявлен
// ИМЕНОВАННОЙ КОНСТАНТОЙ и отдаётся по имени.
//
// Форма законна и распространена — единственный текст отказа выносят в
// константу именно затем, чтобы он был один. Пока распознаватель знал только
// вызовы конструкторов отказа, он на ней МОЛЧАЛ, и верное утверждение кейса о
// существующем тексте объявлялось утверждением без производителя.
//
// Сужение проверяется отдельной осью: всякая строковая константа корпусом НЕ
// становится — иначе он раздулся бы именами таблиц, ключей и путей, и гейт
// перестал бы находить настоящее.

func rpProducersOfSource(t *testing.T, src string) map[string]bool {
	t.Helper()
	root := t.TempDir()
	rel := filepath.Join("services", "iam", "internal", "probe", "refusal.go")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
		t.Fatalf("синтетическое дерево не создано: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), []byte(src), 0o600); err != nil {
		t.Fatalf("синтетика не записана: %v", err)
	}
	byOwner, err := rtProducers(root, []string{filepath.ToSlash(rel)})
	if err != nil {
		t.Fatalf("сбор производителей не состоялся: %v", err)
	}
	return byOwner["iam"]
}

func TestRP_NamedConstantIsRecognisedAsAProducer(t *testing.T) {
	t.Run("новая форма: текст объявлен константой сообщения", func(t *testing.T) {
		got := rpProducersOfSource(t, `package probe

const RefusalMessage = "credential is not accepted"
`)
		if !got["credential is not accepted"] {
			t.Fatalf("текст, объявленный константой сообщения, не признан производимым: %v", got)
		}
	})

	t.Run("прежняя форма по-прежнему читается", func(t *testing.T) {
		// Контроль сохранности: расширение распознавателя не имеет права
		// вытеснить то, что он читал раньше.
		got := rpProducersOfSource(t, `package probe

import "errors"

var errGone = errors.New("account is not empty")
`)
		if !got["account is not empty"] {
			t.Fatalf("прежняя форма перестала читаться: %v", got)
		}
	})

	t.Run("сужение: обычная строковая константа корпусом НЕ становится", func(t *testing.T) {
		// Без этой оси расширение было бы не расширением, а ослаблением: корпус
		// принял бы имена таблиц и путей, и гейт замолчал бы на настоящем.
		got := rpProducersOfSource(t, `package probe

const tableName = "kacho_iam.accounts"
const defaultPath = "/iam/v1/accounts"
`)
		if got["kacho_iam.accounts"] || got["/iam/v1/accounts"] {
			t.Fatalf("корпус принял строки, отказом не являющиеся: %v", got)
		}
	})
}
