// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shadowverdict

// switch_test.go — ПЕРЕКЛЮЧЁННОЕ НАПРАВЛЕНИЕ: решает форма, движок спрашивается
// рядом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ — И ЧЕГО НАМЕРЕННО НЕ УТВЕРЖДАЕТСЯ
//
// Утверждается ИСХОД у вызывающего: чей ответ он получил. «Функция формы была
// вызвана» исходом не является — вызвать можно и выбросив ответ, и именно так
// выглядит молчаливый возврат к движку, который здесь запрещён.
//
// Не утверждается маршрутизация дверей решения (она в `authzcascade` и в
// `service`) — здесь предмет один: сам сравнитель.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/iam/internal/verdictsource"
)

// engineStub — движок, спрошенный РЯДОМ. Считает вызовы, чтобы «спрошен» было
// отличимо от «не спрошен», и отвечает заданным.
type engineStub struct {
	allowed  bool
	answered bool
	calls    int
}

func (e *engineStub) ask(context.Context) (bool, bool) {
	e.calls++
	return e.allowed, e.answered
}

func switched(form Asker, types ...string) *Comparator {
	return New(form, quiet()).WithSwitchboard(verdictsource.New(types...))
}

// Рубильник виден сравнителю: он отвечает на вопрос «кто решает», и отвечает
// одним предикатом — тем же, что читают страж старта и самоотчёт.
func TestComparatorReportsWhoDecides(t *testing.T) {
	c := switched(&stubAsker{}, "vpc_network")

	if !c.Decides("vpc_network") {
		t.Fatal("названный тип обязан решаться формой")
	}
	if c.Decides("vpc_subnet") {
		t.Fatal("не названный тип обязан остаться за движком")
	}
}

// ГЛАВНОЕ УТВЕРЖДЕНИЕ: вызывающий получает ответ ФОРМЫ, а не движка — и это
// проверяется на входе, где ответы ПРОТИВОПОЛОЖНЫ. Совпадающие ответы сделали
// бы утверждение тождественно истинным.
func TestSwitchedTypeReturnsTheFormsVerdictNotTheEngines(t *testing.T) {
	form := &stubAsker{allow: true}
	engine := &engineStub{allowed: false, answered: true}
	c := switched(form, "vpc_network")

	allowed, err := c.Verdict(context.Background(),
		"user:u1", "vpc_network", "n1", "v_get", nil, engine.ask)

	if err != nil {
		t.Fatalf("форма ответила — ошибки быть не должно: %v", err)
	}
	if !allowed {
		t.Fatal("вернулся ответ ДВИЖКА: источник вердикта не переключён")
	}

	settled(c)
	if engine.calls != 1 {
		t.Fatalf("движок обязан быть спрошен РЯДОМ ровно один раз, спрошен %d", engine.calls)
	}
}

// Зеркальная половина: форма отказывает — вызывающий получает отказ, даже когда
// движок разрешает. Без неё проба выше зеленела бы на «всегда разрешать».
func TestSwitchedTypeReturnsTheFormsDenialWhenEngineAllows(t *testing.T) {
	form := &stubAsker{allow: false}
	engine := &engineStub{allowed: true, answered: true}
	c := switched(form, "vpc_network")

	allowed, err := c.Verdict(context.Background(),
		"user:u1", "vpc_network", "n1", "v_get", nil, engine.ask)

	if err != nil || allowed {
		t.Fatalf("вернулся ответ движка: allowed=%v err=%v", allowed, err)
	}
}

// ОТКАЗ ФОРМЫ — ЭТО ОТКАЗ, А НЕ «СПРОСИ ДВИЖОК».
//
// Запасной путь на движок в момент переключения превращает измеренную
// независимость в фикцию: под нагрузкой он даёт ровно ту зависимость, ради
// снятия которой всё делается. Поэтому ошибка формы уезжает вызывающему
// ОТДЕЛЬНЫМ исходом — ни отказом в доступе, ни ответом движка.
func TestFormFailureIsAnOutageNotADenialAndNotTheEnginesAnswer(t *testing.T) {
	boom := errors.New("форма не ответила")
	form := &stubAsker{err: boom}
	engine := &engineStub{allowed: true, answered: true}
	c := switched(form, "vpc_network")

	allowed, err := c.Verdict(context.Background(),
		"user:u1", "vpc_network", "n1", "v_get", nil, engine.ask)

	if err == nil {
		t.Fatal("отказ формы обязан доехать до вызывающего ошибкой, а не превратиться в вердикт")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("причина отказа обязана сохраниться: %v", err)
	}
	if allowed {
		t.Fatal("ответ движка подставлен вместо неответившей формы — молчаливый возврат к движку")
	}
}

// Недоступность движка на ответ вызывающему не влияет НИКАК: тень не решает.
func TestEngineUnavailableDoesNotChangeTheAnswer(t *testing.T) {
	form := &stubAsker{allow: true}
	engine := &engineStub{answered: false}
	c := switched(form, "vpc_network")

	allowed, err := c.Verdict(context.Background(),
		"user:u1", "vpc_network", "n1", "v_get", nil, engine.ask)
	if err != nil || !allowed {
		t.Fatalf("тень повлияла на ответ: allowed=%v err=%v", allowed, err)
	}

	s := settled(c).Counters()
	if s.Compared != 0 {
		t.Fatalf("сравнивать не с чем — сравнений быть не должно, их %d", s.Compared)
	}
	if s.Unfinished != 1 {
		t.Fatalf("исход обязан лечь в «не выполнилось», их %d", s.Unfinished)
	}
}

// НАПРАВЛЕНИЕ РАСХОЖДЕНИЯ считается раздельно, и направления не равны.
//
// «Форма шире» — расширение доступа: уже случившееся событие безопасности.
// «Форма уже» — отказ в обслуживании. Один счётчик на оба сделал бы их
// неотличимыми ровно там, где различие и решает, откатывать ли тип.
func TestDivergenceIsCountedByDirection(t *testing.T) {
	t.Run("форма шире", func(t *testing.T) {
		c := switched(&stubAsker{allow: true}, "vpc_network")
		engine := &engineStub{allowed: false, answered: true}

		_, _ = c.Verdict(context.Background(), "user:u1", "vpc_network", "n1", "v_get", nil, engine.ask)

		s := settled(c).Counters()
		if s.DivergedFormWider != 1 || s.DivergedFormNarrower != 0 {
			t.Fatalf("направление названо неверно: шире=%d уже=%d", s.DivergedFormWider, s.DivergedFormNarrower)
		}
		if s.Diverged != 1 {
			t.Fatalf("направления обязаны оставаться подмножеством расхождений, их %d", s.Diverged)
		}
	})

	t.Run("форма уже", func(t *testing.T) {
		c := switched(&stubAsker{allow: false}, "vpc_network")
		engine := &engineStub{allowed: true, answered: true}

		_, _ = c.Verdict(context.Background(), "user:u1", "vpc_network", "n1", "v_get", nil, engine.ask)

		s := settled(c).Counters()
		if s.DivergedFormNarrower != 1 || s.DivergedFormWider != 0 {
			t.Fatalf("направление названо неверно: шире=%d уже=%d", s.DivergedFormWider, s.DivergedFormNarrower)
		}
	})

	t.Run("согласие направления не даёт", func(t *testing.T) {
		c := switched(&stubAsker{allow: true}, "vpc_network")
		engine := &engineStub{allowed: true, answered: true}

		_, _ = c.Verdict(context.Background(), "user:u1", "vpc_network", "n1", "v_get", nil, engine.ask)

		s := settled(c).Counters()
		if s.Diverged != 0 || s.DivergedFormWider != 0 || s.DivergedFormNarrower != 0 {
			t.Fatalf("согласие посчитано расхождением: %+v", s)
		}
		if s.Compared != 1 {
			t.Fatalf("сравнение состоялось, а не посчитано: %d", s.Compared)
		}
	})
}

// Направление считается и НА НЕПЕРЕКЛЮЧЁННОМ пути: оно свойство ПАРЫ ответов, а
// не того, кто спрошен первым. Иначе полярность пришлось бы «переворачивать»
// второй реализацией, и две меры одного предмета разошлись бы молча.
func TestDirectionIsCountedOnTheEngineDecidedPathToo(t *testing.T) {
	c := New(&stubAsker{allow: true}, quiet())

	settle := c.Ask(context.Background(), "user:u1", "vpc_network", "n1", "v_get", nil)
	settle(false, true) // движок отказал, форма разрешает ⇒ форма шире

	s := settled(c).Counters()
	if s.DivergedFormWider != 1 {
		t.Fatalf("направление на движковом пути не посчитано: %+v", s)
	}
}

// ИСТОЧНИК ВЕРДИКТА виден числом, и знаменатель сходится: каждое решение имеет
// ровно один источник. Без этого «переключено» и «объявлено переключённым»
// неразличимы — рубильник может стоять в позиции «форма», а решения продолжать
// идти движком.
func TestVerdictSourceIsCountedAndSumsToDecisions(t *testing.T) {
	c := switched(&stubAsker{allow: true}, "vpc_network")
	engine := &engineStub{allowed: true, answered: true}

	_, _ = c.Verdict(context.Background(), "user:u1", "vpc_network", "n1", "v_get", nil, engine.ask)
	c.Ask(context.Background(), "user:u1", "vpc_subnet", "s1", "v_get", nil)(true, true)
	c.Unaskable("объект вопроса не разобран", "", "v_get")

	s := settled(c).Counters()
	if s.VerdictsForm != 1 {
		t.Fatalf("вердиктов формы %d, ожидался 1", s.VerdictsForm)
	}
	if s.VerdictsEngine != 2 {
		t.Fatalf("вердиктов движка %d, ожидалось 2", s.VerdictsEngine)
	}
	if s.VerdictsForm+s.VerdictsEngine != s.Decisions {
		t.Fatalf("у решения обязан быть ровно один источник: %d+%d != %d",
			s.VerdictsForm, s.VerdictsEngine, s.Decisions)
	}
}

// Расхождение на ПЕРЕКЛЮЧЁННОМ типе в сторону расширения называется своим
// именем в журнале: это то, по чему оператор решает откатывать тип.
func TestWiderFormOnSwitchedTypeIsNamedAsAccessExpansion(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	c := New(&stubAsker{allow: true}, logger).WithSwitchboard(verdictsource.New("vpc_network"))
	engine := &engineStub{allowed: false, answered: true}

	_, _ = c.Verdict(context.Background(), "user:u1", "vpc_network", "n1", "v_get", nil, engine.ask)

	got := logOf(c, &buf)
	if !strings.Contains(got, "vpc_network") {
		t.Fatalf("запись не называет тип, который надо откатить: %s", got)
	}
	if !strings.Contains(got, "расширение доступа") {
		t.Fatalf("запись не называет направление как расширение доступа: %s", got)
	}
}

// Непровязанный рубильник не переключает ничего, и `Verdict` на нём — ошибка
// сборки, а не тихий «нет».
//
// Тихий отказ здесь был бы худшим из исходов: тип выглядел бы переключённым, а
// каждое решение по нему становилось бы отказом без единой записи.
func TestVerdictWithoutAFormIsAnErrorNotADenial(t *testing.T) {
	c := New(nil, quiet()).WithSwitchboard(verdictsource.New("vpc_network"))
	engine := &engineStub{allowed: true, answered: true}

	_, err := c.Verdict(context.Background(), "user:u1", "vpc_network", "n1", "v_get", nil, engine.ask)
	if err == nil {
		t.Fatal("вердикт без формы обязан быть ошибкой: тихий отказ неотличим от честного")
	}
	if c.Decides("vpc_network") {
		t.Fatal("без формы рубильник не вправе объявлять тип переключённым")
	}
}
