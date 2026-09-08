// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_kaname_exclusion_form_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`clienttruth_kaname_exclusion_form_test.go`) о способности падать не
// говорит ничего — зелёный получает и та проверка, что не смотрит никуда.
//
// Инъекции вносятся ПО ОДНОЙ, и каждая меняет РОВНО ОДИН факт против контроля:
// иначе неизвестно, какой из двух дал красное, и «краснеет» ничего не
// доказывает. К каждой приложен законный близнец той же формы, обязанный
// молчать.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// СТЕНД
//
// Законное состояние: край ЗОВЁТ снятие, читатель отказывает по семи поводам и
// НИ ОДНОГО о сочетании форм, страница объясняет взаимоисключение построением.
// На нём анализатор обязан молчать.

// exclusionGuideLegal — страница в законном состоянии.
//
// Второй абзац законно говорит об ОТКАЗЕ и предметом НЕ является: он про
// единообразие текста отказа. Он и есть близнец поабзацного вердикта — файл,
// прочитанный целиком, зачёл бы этот отказ обещанием предмета.
const exclusionGuideLegal = `# Установка

**Приём предъявленного и наш край — взаимоисключающие способы назваться, и
держится это ПОСТРОЕНИЕМ.** Край снимает арендаторское удостоверение перед
пересылкой за себя, установив личность сам.

Отказ выглядит одинаково при любой причине: служба отвергает предъявленное
одним и тем же текстом.
`

// exclusionReaderLegal — читатель, отказывающий по поводам, НИ ОДИН из которых
// не о сочетании форм.
//
// НАДГРОБИЕ снятой ветви стоит здесь намеренно: связный абзац по-русски о том
// самом сочетании, и он ловит проверку по подстроке — та объявила бы живым
// ровно то, что снято.
const exclusionReaderLegal = `package presentedcred

// ЗДЕСЬ СТОЯЛ ОТКАЗ «обе формы личности разом» — и он снят вместе с предметом:
// сочетание производил наш же край, поэтому ветвь отвергала бы каждый
// проксированный им запрос. Слова "presented" и "forwarded" в этом комментарии
// вызовом отказа не являются.
func (r *Reader) decide(raw string) error {
	if raw == "" {
		return r.refuse("verified token names no principal")
	}
	return r.refuse("token type is not the one this surface accepts")
}
`

// exclusionStripDeclLegal — файл, ОБЪЯВЛЯЮЩИЙ снятие.
//
// Своё имя он называет трижды — в шапке и в двух сигнатурах, — а вызовом это не
// является: объявление без вызывающего механизма не даёт. Файл изымается по
// имени, и изъятие проверяется отдельным прогоном ниже.
const exclusionStripDeclLegal = `package principalmeta

// StripPresentedCredential и StripCredentialBeforeForwarding — снятие
// удостоверения перед пересылкой за себя.
func StripPresentedCredential(md map[string]string) map[string]string { return md }

func StripCredentialBeforeForwarding(next int) int { return next }
`

// exclusionEdgeWiringLegal — провязка: край ЗОВЁТ снятие.
const exclusionEdgeWiringLegal = `package main

import "example/principalmeta"

func main() {
	_ = principalmeta.StripCredentialBeforeForwarding(1)
	_ = principalmeta.StripPresentedCredential(nil)
}
`

type exclusionStand struct{ root string }

func newExclusionStand(t *testing.T) *exclusionStand {
	t.Helper()
	s := &exclusionStand{root: t.TempDir()}
	s.write(t, "guide/INSTALL.md", exclusionGuideLegal)
	s.write(t, "edge/internal/principalmeta/credential_strip.go", exclusionStripDeclLegal)
	s.write(t, "edge/cmd/api-gateway/main.go", exclusionEdgeWiringLegal)
	s.write(t, "svc/presentedcred/reader.go", exclusionReaderLegal)
	// Проба соседнего пакета: она называет снятие и отказ на сочетании ОБА, и
	// засчитайся она — стенд перестал бы быть законным. Файлы `_test.go` из
	// обхода изымаются по суффиксу имени, а не перечнем.
	s.write(t, "edge/cmd/api-gateway/main_test.go", `package main

func TestStrip() {
	_ = StripCredentialBeforeForwarding(1)
	_ = refuse("both a presented credential and a forwarded identity in one request")
}
`)
	return s
}

func (s *exclusionStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (s *exclusionStand) run(t *testing.T) (
	[]ClientTruthKanameExclusionFormFinding, ClientTruthKanameExclusionFormCensus,
) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditClientTruthKanameExclusionForm(ClientTruthKanameExclusionFormOptions{
		Tree:            clientTruthSyntheticTree(t, s.root),
		GuidePath:       "guide/INSTALL.md",
		EdgeDir:         "edge",
		StripFuncs:      []string{"StripPresentedCredential", "StripCredentialBeforeForwarding"},
		StripDeclFile:   "edge/internal/principalmeta/credential_strip.go",
		ReaderDir:       "svc/presentedcred",
		RefuseFunc:      "refuse",
		CoPresenceTerms: []string{"presented", "forwarded"},
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// exclusionKinds — виды находок в порядке появления.
func exclusionKinds(findings []ClientTruthKanameExclusionFormFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Kind)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// КОНТРОЛЬ

// TestExclusionFormGate_SilentOnTheLegalStand — всё цело, гейт молчит.
//
// Без этого прогона любое «краснеет» ниже доказывало бы лишь то, что он
// краснеет всегда.
func TestExclusionFormGate_SilentOnTheLegalStand(t *testing.T) {
	t.Parallel()
	f, c := newExclusionStand(t).run(t)
	if len(f) != 0 {
		t.Fatalf("гейт краснеет на законном стенде: %v", exclusionKinds(f))
	}
	if c.StripCalls != 2 {
		t.Fatalf("вызовов снятия насчитано %d, а провязка зовёт два — "+
			"молчание пришло бы от слепоты, а не от целости стенда", c.StripCalls)
	}
	if c.SubjectParagraphs != 1 || c.ExplainByBuild != 1 || c.ExplainByRefusal != 0 {
		t.Fatalf("перепись страницы не та, на которой стенд объявлен законным: %+v", c)
	}
	if c.RefuseCalls != 2 {
		t.Fatalf("отказов читателя насчитано %d, а их два — распознаватель отказов слеп", c.RefuseCalls)
	}
	if c.CoPresenceRefusals != 0 {
		t.Fatalf("надгробие снятой ветви зачтено производителем отказа (%d) — "+
			"анализатор читает комментарий вместо вызова", c.CoPresenceRefusals)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// A. ОБЕЩАНИЕ ОТКАЗА БЕЗ ПРОИЗВОДИТЕЛЯ

// exclusionGuidePromisesRefusal — тот же законный абзац, к которому ДОБАВЛЕНО
// обещание отказа. Один добавленный факт: построение он по-прежнему называет.
const exclusionGuidePromisesRefusal = `# Установка

**Приём предъявленного и наш край — взаимоисключающие способы назваться, и
держится это ПОСТРОЕНИЕМ.** Край снимает арендаторское удостоверение перед
пересылкой за себя, установив личность сам. Такой запрос несёт ОБЕ формы сразу,
и служба отвергает его.

Отказ выглядит одинаково при любой причине: служба отвергает предъявленное
одним и тем же текстом.
`

func TestExclusionFormGate_PromisedRefusalWithoutAProducerIsFound(t *testing.T) {
	t.Parallel()
	s := newExclusionStand(t)
	s.write(t, "guide/INSTALL.md", exclusionGuidePromisesRefusal)

	f, c := s.run(t)
	if got := exclusionKinds(f); len(got) != 1 || got[0] != "refusal-without-producer" {
		t.Fatalf("ожидалась ровно одна находка вида refusal-without-producer, получено %v", got)
	}
	if f[0].Line != 3 {
		t.Errorf("находка не называет строку абзаца предмета: %d", f[0].Line)
	}
	if s := f[0].String(); !strings.Contains(s, "guide/INSTALL.md:3") {
		t.Errorf("находка не называет координату: %s", s)
	}
	if c.ExplainByBuild != 1 {
		t.Errorf("построение перестало быть названным — инъекция сменила ДВА факта, "+
			"и красное могло прийти от второго: %+v", c)
	}
}

// TestExclusionFormGate_PromisedRefusalWithAProducerIsSilent — законный близнец
// A и ПРОВЕРКА САМОИСТЕЧЕНИЯ.
//
// Изменён ровно один факт против инъекции A: у обещанного отказа появился
// производитель. Это и есть мир прежней редакции — форма взаимоисключения
// остаётся решением владельца, и гейт его не запрещает. Вернётся ветвь —
// утверждение A умолкнет само.
func TestExclusionFormGate_PromisedRefusalWithAProducerIsSilent(t *testing.T) {
	t.Parallel()
	s := newExclusionStand(t)
	s.write(t, "guide/INSTALL.md", exclusionGuidePromisesRefusal)
	s.write(t, "svc/presentedcred/reader.go", `package presentedcred

func (r *Reader) decide(raw string) error {
	if raw == "" {
		return r.refuse("both a presented credential and a forwarded identity in one request")
	}
	return r.refuse("token type is not the one this surface accepts")
}
`)
	f, c := s.run(t)
	if len(f) != 0 {
		t.Fatalf("гейт краснеет при живом производителе отказа — послабление не истекает: %v",
			exclusionKinds(f))
	}
	if c.CoPresenceRefusals != 1 {
		t.Fatalf("производитель отказа на сочетании не распознан (%d) — молчание "+
			"пришло от слепоты, а не от наличия производителя", c.CoPresenceRefusals)
	}
}

// TestExclusionFormGate_RefusalNamingOneFormIsNotAProducer — вторая половина
// того же: отказ, назвавший ОДНУ форму, производителем сочетания не является.
//
// Требуются ВСЕ слова: иначе производителем сочтётся любой отказ, говорящий
// «presented», а таких у читателя большинство.
func TestExclusionFormGate_RefusalNamingOneFormIsNotAProducer(t *testing.T) {
	t.Parallel()
	s := newExclusionStand(t)
	s.write(t, "guide/INSTALL.md", exclusionGuidePromisesRefusal)
	s.write(t, "svc/presentedcred/reader.go", `package presentedcred

func (r *Reader) decide(raw string) error {
	return r.refuse("more than one credential presented in one request")
}
`)
	f, c := s.run(t)
	if got := exclusionKinds(f); len(got) != 1 || got[0] != "refusal-without-producer" {
		t.Fatalf("отказ об ОДНОЙ форме зачтён производителем сочетания: %v", got)
	}
	if c.CoPresenceRefusals != 0 {
		t.Fatalf("отказ об одной форме признан отказом о сочетании: %+v", c)
	}
}

// TestExclusionFormGate_UppercaseClaimIsJudged — регистр снимается.
//
// Страница пишет `ОБЕ` прописными; предикат, читающий написанное, промолчал бы
// на живом производителе. Та же слепота, что четырежды за жизнь приёмки.
func TestExclusionFormGate_UppercaseClaimIsJudged(t *testing.T) {
	t.Parallel()
	s := newExclusionStand(t)
	s.write(t, "guide/INSTALL.md", `# Установка

**ПРИЁМ ПРЕДЪЯВЛЕННОГО И НАШ КРАЙ — ВЗАИМОИСКЛЮЧАЮЩИЕ СПОСОБЫ НАЗВАТЬСЯ.** КРАЙ
СНИМАЕТ УДОСТОВЕРЕНИЕ ПЕРЕД ПЕРЕСЫЛКОЙ. СЛУЖБА ОТВЕРГАЕТ ЗАПРОС С ОБЕИМИ
ФОРМАМИ.
`)
	f, _ := s.run(t)
	if got := exclusionKinds(f); len(got) != 1 || got[0] != "refusal-without-producer" {
		t.Fatalf("прописная запись не рассужена — распознаватель читает написанное: %v", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// B. ПОСТРОЕНИЕ ЖИВО, А СТРАНИЦА ЕГО НЕ НАЗЫВАЕТ

func TestExclusionFormGate_UnnamedConstructionIsFound(t *testing.T) {
	t.Parallel()
	s := newExclusionStand(t)
	// Один изменённый факт: абзац предмета перестал называть построение.
	// Отказа он не обещает — иначе краснели бы оба утверждения разом.
	s.write(t, "guide/INSTALL.md", `# Установка

**Приём предъявленного и наш край — взаимоисключающие способы назваться.**
Выберите один из них и не включайте второй.
`)
	f, c := s.run(t)
	if got := exclusionKinds(f); len(got) != 1 || got[0] != "construction-unnamed" {
		t.Fatalf("ожидалась ровно одна находка вида construction-unnamed, получено %v", got)
	}
	if f[0].Line != 3 {
		t.Errorf("находка не называет строку абзаца предмета: %d", f[0].Line)
	}
	if c.SubjectParagraphs != 1 {
		t.Errorf("абзац предмета не распознан: %+v", c)
	}
}

// TestExclusionFormGate_VanishedSubjectIsFound — объяснение исчезло ЦЕЛИКОМ.
//
// Ради этого прогона утверждение B и сделано положительным: отрицание («страница
// не говорит такого-то слова») здесь ЗАМОЛЧАЛО БЫ — вход, на котором оно находит
// нарушение, перестал быть представимым.
func TestExclusionFormGate_VanishedSubjectIsFound(t *testing.T) {
	t.Parallel()
	s := newExclusionStand(t)
	s.write(t, "guide/INSTALL.md", "# Установка\n\nПоднимите службу и позовите её.\n")

	f, c := s.run(t)
	if got := exclusionKinds(f); len(got) != 1 || got[0] != "construction-unnamed" {
		t.Fatalf("исчезнувшее объяснение не найдено: %v", got)
	}
	if !strings.Contains(f[0].String(), "абзаца предмета на странице нет вовсе") {
		t.Errorf("находка не отличает «объяснено иначе» от «не объяснено вовсе»: %s", f[0])
	}
	if c.SubjectParagraphs != 0 {
		t.Errorf("абзац предмета найден там, где его нет: %+v", c)
	}
}

// TestExclusionFormGate_NoStripInTheTreeSilencesB — законный близнец B.
//
// Изменён ровно один факт: провязки снятия в дереве нет. Гейт судит СОГЛАСИЕ
// страницы с деревом, а не выбор формы, — поэтому молчит. Что молчание это не
// зелёный вердикт, а отсутствие предмета, обязан сказать ВЫЗЫВАЮЩИЙ переписью:
// прогон по дереву падает на `StripCalls == 0` премисой.
func TestExclusionFormGate_NoStripInTheTreeSilencesB(t *testing.T) {
	t.Parallel()
	s := newExclusionStand(t)
	s.write(t, "guide/INSTALL.md", `# Установка

**Приём предъявленного и наш край — взаимоисключающие способы назваться.**
Выберите один из них и не включайте второй.
`)
	if err := os.Remove(filepath.Join(s.root, "edge/cmd/api-gateway/main.go")); err != nil {
		t.Fatal(err)
	}
	f, c := s.run(t)
	if len(f) != 0 {
		t.Fatalf("гейт требует называть построение, которого в дереве нет: %v", exclusionKinds(f))
	}
	if c.StripCalls != 0 {
		t.Fatalf("вызовы снятия насчитаны после снятия провязки (%d) — считаются "+
			"объявления, а не вызовы", c.StripCalls)
	}
}

// TestExclusionFormGate_DeclarationAloneIsNotAWiring — разбор, а не подстрока.
//
// Объявляющий файл называет обе функции снятия трижды: в шапке и в двух
// сигнатурах. Проверка по подстроке объявила бы механизм живым при снятой
// провязке — то есть дала бы зелёное ровно там, где предмет уехал.
func TestExclusionFormGate_DeclarationAloneIsNotAWiring(t *testing.T) {
	t.Parallel()
	s := newExclusionStand(t)
	if err := os.Remove(filepath.Join(s.root, "edge/cmd/api-gateway/main.go")); err != nil {
		t.Fatal(err)
	}
	_, c := s.run(t)
	if c.EdgeGoFiles == 0 {
		t.Fatal("файлов края разобрано 0 — молчание беспредметно")
	}
	if c.StripCalls != 0 {
		t.Fatalf("объявление снятия зачтено провязкой: вызовов %d при нуле вызывающих",
			c.StripCalls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ПУСТОЙ ОБХОД

// TestExclusionFormGate_EmptyTraversalIsNotASilentSuccess — шов назван честно.
//
// Анализатор на пустом дереве находок не даёт — и не должен: у него нет способа
// отличить «смотреть не на что» от «всё чисто». Отличает это ВЫЗЫВАЮЩИЙ, и
// материал для отличия анализатор обязан ему дать — перепись, у которой на
// пустом обходе нули по КАЖДОЙ оси. Здесь утверждается ровно это.
func TestExclusionFormGate_EmptyTraversalIsNotASilentSuccess(t *testing.T) {
	t.Parallel()
	s := &exclusionStand{root: t.TempDir()}
	s.write(t, "guide/INSTALL.md", "# Установка\n")

	f, c := s.run(t)
	if len(f) != 0 {
		t.Fatalf("находки на пустом дереве: %v", exclusionKinds(f))
	}
	if c.EdgeGoFiles != 0 || c.ReaderGoFiles != 0 || c.StripCalls != 0 || c.RefuseCalls != 0 {
		t.Fatalf("перепись пустого обхода не пуста — вызывающий не отличит "+
			"«ноль находок» от «ноль прочитанного»: %+v", c)
	}
	if c.GuideParagraphs == 0 {
		t.Fatal("страница прочитана как пустая — премиса вызывающего сработает не на том")
	}
}

// TestExclusionFormGate_MissingGuideIsAnError — страницы нет вовсе.
//
// Молчаливый ноль здесь означал бы гейт, переживший переезд своего предмета.
func TestExclusionFormGate_MissingGuideIsAnError(t *testing.T) {
	t.Parallel()
	s := newExclusionStand(t)
	if err := os.Remove(filepath.Join(s.root, "guide/INSTALL.md")); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	_, _, err := AuditClientTruthKanameExclusionForm(ClientTruthKanameExclusionFormOptions{
		Tree:            clientTruthSyntheticTree(t, s.root),
		GuidePath:       "guide/INSTALL.md",
		EdgeDir:         "edge",
		StripFuncs:      []string{"StripPresentedCredential", "StripCredentialBeforeForwarding"},
		StripDeclFile:   "edge/internal/principalmeta/credential_strip.go",
		ReaderDir:       "svc/presentedcred",
		RefuseFunc:      "refuse",
		CoPresenceTerms: []string{"presented", "forwarded"},
	}, &log)
	if err == nil {
		t.Fatal("страницы нет, а анализатор вернул успех — пустой обход неотличим от чистого дерева")
	}
}
