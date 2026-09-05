// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import "testing"

// Инъекция гейта Г6 — В ОБЕ СТОРОНЫ.
//
// Гейт судится ПРОГОНОМ, а не чтением описания. Без обратной стороны он ловил бы
// форму, а не существо, и первый же ложный срабат его отключил бы: слово
// «openfga» в дереве осталось законно (журнал `fga_outbox`, файл модели прав,
// учётка посредника), и гейт, краснеющий на прозе, снимут как непонятный.

// TestR7_3_26_InjectionRedOnAReturnedEngineCall — вернули обращение к снятому
// хранилищу → гейт КРАСНЕЕТ и НАЗЫВАЕТ КООРДИНАТУ.
func TestR7_3_26_InjectionRedOnAReturnedEngineCall(t *testing.T) {
	sources := map[string]string{
		"services/iam/internal/apps/kacho/api/thing/decide.go": `package thing

import "context"

type store interface {
	CheckWithContextualTuples(ctx context.Context, subject, relation, object string) (bool, error)
}

func decide(ctx context.Context, s store, subject, relation, object string) (bool, error) {
	// Возвращённое обращение: вопрос уходит наружу, к чужому хранилищу.
	return s.CheckWithContextualTuples(ctx, subject, relation, object)
}
`,
	}

	findings, census, err := FindRetiredEngineSurface(sources, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if census.Files != 1 {
		t.Fatalf("прочитано файлов %d, ожидался 1 — инъекция не доехала до разбора", census.Files)
	}
	if len(findings) == 0 {
		t.Fatal("гейт МОЛЧИТ на возвращённом обращении к снятому хранилищу — " +
			"он не способен покраснеть, и его зелёный на дереве ничего не значит")
	}
	got := findings[0]
	if got.File != "services/iam/internal/apps/kacho/api/thing/decide.go" || got.Line == 0 {
		t.Fatalf("находка без координаты: %+v — гейт, который не называет место, "+
			"невозможно исполнить", got)
	}
	if got.Symbol != "CheckWithContextualTuples" {
		t.Fatalf("находка называет %q, а обращение было к CheckWithContextualTuples", got.Symbol)
	}
}

// TestR7_3_26_InjectionRedOnAReturnedEngineType — вернули сам тип клиента → гейт
// краснеет.
//
// Отдельно от предыдущей: возвращение ТИПА и возвращение ВЫЗОВА — разные исходы,
// и находка обязана называть, что именно она видит.
func TestR7_3_26_InjectionRedOnAReturnedEngineType(t *testing.T) {
	sources := map[string]string{
		"services/iam/internal/clients/back.go": `package clients

type OpenFGAHTTPClient struct{ Endpoint string }
`,
	}
	findings, _, err := FindRetiredEngineSurface(sources, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("гейт молчит на возвращённом типе клиента снятого движка")
	}
	if findings[0].Symbol != RetiredEngineAnchor {
		t.Fatalf("находка называет %q, а объявлен был %q", findings[0].Symbol, RetiredEngineAnchor)
	}
}

// TestR7_3_26_InjectionSilentOnTheLegitimateTwin — ЗАКОННЫЙ БЛИЗНЕЦ той же формы:
// движок назван в ПРОЗЕ (комментарий, строковый литерал, имя журнала), а
// исполняемого обращения нет → гейт МОЛЧИТ.
//
// Без этой пробы гейт ловил бы слово. Слово в дереве остаётся законно и во
// множестве мест: журнал намерений называется `kaname.fga_outbox`, модель прав
// лежит в `fga_model.fga` и стала источником истины формы, учётка посредника
// зовётся `iam_fgaproxy`. Гейт, краснеющий на них, краснеет на исправном дереве.
func TestR7_3_26_InjectionSilentOnTheLegitimateTwin(t *testing.T) {
	sources := map[string]string{
		"services/iam/internal/repo/kacho/pg/journal.go": `package pg

import "context"

// Журнал намерений исторически называется fga_outbox — по снятому движку OpenFGA.
// Переименование это предмет отдельной задачи: применённые миграции не правятся.
const journalTable = "kaname.fga_outbox"

// notifyChannel — канал, на который шлёт триггер журнала.
const notifyChannel = "kacho_iam_fga_outbox"

func journalName(ctx context.Context) string {
	_ = ctx
	return journalTable + " / " + notifyChannel
}
`,
	}

	findings, census, err := FindRetiredEngineSurface(sources, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт КРАСНЕЕТ на законной прозе (%+v) — он ловит слово, а не "+
			"существо, и первый же ложный срабат его отключит", findings)
	}
	if census.ProseMentions != 1 {
		t.Fatalf("файлов, называющих движок только в прозе, насчитано %d, ожидался 1 — "+
			"перепись не отличает прозу от находки, и «ноль находок» перестаёт быть "+
			"отличимым от «слова в дереве нет»", census.ProseMentions)
	}
}

// TestR7_3_26_InjectionSilentOnANeighbouringPortWithTheSameVerb — второй законный
// близнец: соседний порт с ОДНОИМЁННЫМ, но своим методом.
//
// `Check` и `CheckWithContext` в словарь намеренно не входят — их отвечает форма,
// и запрет на них означал бы запрет на решение о доступе вообще.
func TestR7_3_26_InjectionSilentOnANeighbouringPortWithTheSameVerb(t *testing.T) {
	sources := map[string]string{
		"services/iam/internal/authzcascade/door.go": `package authzcascade

import "context"

type form interface {
	Allowed(ctx context.Context, subject, objectType, objectID, relation string) (bool, error)
}

type Client struct{ f form }

func (c *Client) Check(ctx context.Context, subject, relation, object string) (bool, error) {
	return c.f.Allowed(ctx, subject, "", object, relation)
}

func (c *Client) CheckWithContext(ctx context.Context, subject, relation, object string) (bool, error) {
	return c.Check(ctx, subject, relation, object)
}
`,
	}
	findings, _, err := FindRetiredEngineSurface(sources, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на двери решения поверх формы (%+v) — то есть на том, "+
			"ради чего движок и снимался", findings)
	}
}

// TestR7_3_26_ScopeIsAsserted — «ноль находок» отличимо от «ноль прочитанного».
func TestR7_3_26_ScopeIsAsserted(t *testing.T) {
	findings, census, err := FindRetiredEngineSurface(map[string]string{}, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("на пустом входе найдено %d — разбор выдумывает находки", len(findings))
	}
	if census.Files != 0 || census.Idents != 0 {
		t.Fatalf("на пустом входе перепись насчитала файлов %d, идентификаторов %d — "+
			"объём осмотренного не соответствует входу", census.Files, census.Idents)
	}
}
