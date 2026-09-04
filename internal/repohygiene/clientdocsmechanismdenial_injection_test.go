// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// clientdocsmechanismdenial_injection_test.go — доказательство того, что гейт
// СПОСОБЕН упасть и способен смолчать.
//
// Инъекция подаёт НАСТОЯЩИЙ вход из дерева: четыре отрицания, дословно снятые с
// `95ea80b68^` — состояния до того, как их переписали. Синтетика здесь была бы
// слабее: она доказывает, что гейт ловит то, что автор гейта сумел вообразить.
//
// Прогонов три, как того требует канон:
//
//	контроль          — законные близнецы из ЖИВОГО дерева, гейт молчит;
//	инъекция нового   — историческое отрицание, краснеет только оно;
//	инъекция соседа   — предпосылка и распознаватель, каждый краснеет отдельно.

// historicalDenials — что стояло в клиентской документации до #1389.
var historicalDenials = []struct {
	name string
	rel  string
	body string
}{
	{
		name: "край, быстрый старт",
		rel:  "gateway/docs/content/intro.mdx",
		body: "  клиент поллит `OperationService.Get(id)` до `done=true`. Watch-RPC нет.\n",
	},
	{
		name: "край, обзор API",
		rel:  "gateway/docs/content/api/overview.mdx",
		body: "`OperationService.Get(id)` до `done=true` — Watch-RPC нет. Гейтвей направляет поллинг во владеющий\n",
	},
	{
		name: "compute, архитектура",
		rel:  "services/compute/docs/content/architecture/overview.mdx",
		body: "восстановление наблюдаемого состояния. Подписки на журнал снаружи нет — серверный стрим\nснят задачей #813 за отсутствием потребителя.\n",
	},
	{
		name: "storage, обзор API",
		// Отрицание РАЗОРВАНО переводом строки ровно так, как в исходнике: «Метода»
		// на одной строке, «подписки … не существует» на следующей. Построчный
		// разбор этого не увидел бы — склейка абзаца проверяется тем же входом.
		rel:  "services/storage/docs/content/api/overview.mdx",
		body: "возвращают `Operation` — клиент поллит `OperationService.Get(id)` до `done`. Метода\nподписки на изменения не существует.\n",
	},
}

// TestClientDocsDenialGateFallsOnEachHistoricalDenial — инъекция нового.
func TestClientDocsDenialGateFallsOnEachHistoricalDenial(t *testing.T) {
	domains := []string{"compute", "geo", "iam", "nlb", "registry", "storage", "vpc"}
	for _, c := range historicalDenials {
		findings, census := clientDocsDenialFindings(
			[]denialPage{{rel: c.rel, body: c.body}}, domains)
		if len(findings) != 1 {
			t.Errorf("%s: находок %d, ожидалась 1 — гейт не увидел отрицания, которое "+
				"действительно стояло в дереве (перепись: предложений %d, кандидатов %d)",
				c.name, len(findings), census.sentences, census.candidates)
			continue
		}
		if !strings.Contains(findings[0], c.rel) {
			t.Errorf("%s: находка не называет координату %q — по такой находке идут "+
				"искать не туда:\n%s", c.name, c.rel, findings[0])
		}
	}
}

// TestClientDocsDenialGateIsSilentOnLegitimateTwins — контроль на ЖИВОМ дереве.
//
// Близнецы не выдуманы: это все 34 отрицания, которые сегодня стоят в клиентской
// документации и все до одного истинны. Гейт, краснеющий хоть на одном, был бы снят
// первым же, кто его прочитает.
func TestClientDocsDenialGateIsSilentOnLegitimateTwins(t *testing.T) {
	root := repoRoot(t)
	pages, domains, err := clientDocPages(root, subscriptionDocsLister(treecorpus.UnderWithSuffix))
	if err != nil {
		t.Fatalf("состав клиентской документации: %v", err)
	}
	findings, census := clientDocsDenialFindings(pages, domains)
	t.Logf("контроль: страниц %d · отрицаний рядом с именем механизма %d · суженных %d",
		census.pages, census.candidates, census.candidates-census.findings)
	if census.candidates == 0 {
		t.Fatal("кандидатов ноль — контроль вакуумен: молчание гейта здесь не утверждает ничего")
	}
	for _, f := range findings {
		t.Errorf("гейт краснеет на ИСТИННОМ отрицании — ложная находка, из-за которой его снимут:\n%s", f)
	}
}

// TestClientDocsDenialDiscriminatorIsOneWordWide — самая острая пара, какая бывает.
//
// Два предложения, различающиеся ОДНИМ словом: «снаружи» против «у vpc». Первое
// отрицает платформенный механизм и ложно; второе сужено до домена, не служащего
// глагол, и истинно. Без этой пары нельзя утверждать, что гейт судит ПРЕДМЕТ
// отрицания, а не его форму, — он мог бы краснеть на любом «нет» рядом с «подписк».
func TestClientDocsDenialDiscriminatorIsOneWordWide(t *testing.T) {
	domains := []string{"compute", "vpc"}
	false_, _ := clientDocsDenialFindings(
		[]denialPage{{rel: "a.mdx", body: "Подписки на журнал снаружи нет.\n"}}, domains)
	true_, _ := clientDocsDenialFindings(
		[]denialPage{{rel: "b.mdx", body: "Подписки на журнал у vpc нет.\n"}}, domains)
	if len(false_) != 1 {
		t.Errorf("отрицание платформенного механизма прошло молча (находок %d) — "+
			"дискриминатор не различает предмет", len(false_))
	}
	if len(true_) != 0 {
		t.Errorf("отрицание, суженное до домена, объявлено находкой (%d) — гейт краснеет "+
			"на верном тексте:\n%v", len(true_), true_)
	}
}

// TestClientDocsDenialPremiseIsMeasuredNotAssumed — инъекция соседа: предпосылка.
//
// Гейт запрещает отрицать механизм ПОТОМУ, что механизм в дереве есть. Пропадёт
// объявление — запрет станет ложью, и гейт обязан не молчать, а падать
// предпосылкой. Здесь проверяется, что предпосылка ИЗМЕРЯЕТСЯ по дереву, а не
// подразумевается: подложный состав контрактов даёт ноль объявлений.
func TestClientDocsDenialPremiseIsMeasuredNotAssumed(t *testing.T) {
	root := repoRoot(t)
	declared, protoRead, err := denialMechanismDeclared(root, subscriptionDocsLister(treecorpus.UnderWithSuffix))
	if err != nil {
		t.Fatalf("состав контрактов: %v", err)
	}
	if protoRead == 0 || declared == 0 {
		t.Fatalf("предпосылка не измеряется: контрактов %d, объявлений стрима %d", protoRead, declared)
	}
	empty := subscriptionDocsLister(func(string, ...string) ([]string, error) { return nil, nil })
	gone, read, err := denialMechanismDeclared(root, empty)
	if err != nil {
		t.Fatalf("подложный состав: %v", err)
	}
	if gone != 0 || read != 0 {
		t.Errorf("на пустом составе контрактов предпосылка насчитала %d объявлений в %d файлах — "+
			"значит она берётся не из дерева, и её «истечение» не наступит никогда", gone, read)
	}
}

// TestClientDocsDenialRecognizerReportsAnEmptyWalk — инъекция соседа: слепота.
//
// «Ноль находок» обязано быть отличимо от «ноль прочитанного». Пустой корпус даёт
// ноль кандидатов — то самое состояние, на котором гейт падает отдельным
// утверждением, а не докладывает чистоту.
func TestClientDocsDenialRecognizerReportsAnEmptyWalk(t *testing.T) {
	findings, census := clientDocsDenialFindings(nil, []string{"vpc"})
	if len(findings) != 0 || census.pages != 0 || census.sentences != 0 || census.candidates != 0 {
		t.Errorf("пустой корпус дал страниц %d, предложений %d, кандидатов %d, находок %d — "+
			"перепись обязана показывать пустоту пустотой",
			census.pages, census.sentences, census.candidates, len(findings))
	}
}
