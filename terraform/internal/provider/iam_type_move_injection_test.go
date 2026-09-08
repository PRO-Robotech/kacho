// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Инъекция: способна ли проба переезда УПАСТЬ — и падает ли она с именем виновного типа.
//
// # Зачем инъекция здесь обязательна
//
// Проба переезда зелена на исправном дереве, и это не говорит о ней ничего: зелёной она
// была бы и потеряв способность краснеть. Три формы поломки ниже прогоняются НАСТОЯЩИМ
// запросом переезда через сервер протокола — тот же путь, что у пробы, отличается только
// провайдер, которому запрос адресован.
//
// # Почему три формы, а не одна
//
// Они ломают РАЗНОЕ, и первая одна доказала бы меньше, чем кажется:
//
//	снят метод переезда     — форма, которую поймала бы и проверка «объявлен ли метод»;
//	метод есть, состояние
//	  отдаётся ПУСТЫМ       — форма, которую проверка наличия метода НЕ ловит вовсе:
//	                          объявление на месте, а запись арендатора после переезда
//	                          пуста. Ради неё предмет пробы назван ИСХОДОМ;
//	переезд выдан типу,
//	  имени не менявшему    — обратная сторона: приём чужого состояния там, где
//	                          переезжать неоткуда.
//
// # Один факт на инъекцию
//
// Каждая обёртка меняет РОВНО ОДНО против настоящего ресурса и повторяет всё остальное
// дословно — описание типа, схему, разборчивость источника (имя прежнего типа и адрес
// провайдера). Инъекция, поменявшая заодно разборчивость, роняла бы соседнее утверждение
// («переезд не всеяден»), и красное пришло бы не от проверяемого.

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// ---- провайдер с подменённым ресурсом ------------------------------------------------------

type moveProbeInjectedProvider struct {
	provider.Provider
	swap map[string]func(resource.Resource) resource.Resource
}

func (p moveProbeInjectedProvider) Resources(ctx context.Context) []func() resource.Resource {
	base := p.Provider.Resources(ctx)
	out := make([]func() resource.Resource, 0, len(base))
	for _, ctor := range base {
		wrap, ok := p.swap[typeNameOfResource(ctor())]
		if !ok {
			out = append(out, ctor)
			continue
		}
		out = append(out, func() resource.Resource { return wrap(ctor()) })
	}
	return out
}

func moveProbeWith(typeName string, wrap func(resource.Resource) resource.Resource) provider.Provider {
	return moveProbeInjectedProvider{
		Provider: New(),
		swap:     map[string]func(resource.Resource) resource.Resource{typeName: wrap},
	}
}

// ---- форма 1: метод переезда снят ----------------------------------------------------------

// moveProbeStripped — ресурс без объявления переезда.
//
// Встроен ИНТЕРФЕЙС ресурса, а не структура: набор методов интерфейса не несёт MoveState,
// поэтому обёртка не удовлетворяет resource.ResourceWithMoveState — ровно как ресурс,
// у которого метод не написан. Всё прочее поведение — настоящего ресурса.
type moveProbeStripped struct{ resource.Resource }

// ---- форма 2: метод есть, а состояние доезжает НЕПОЛНЫМ -------------------------------------

// moveProbeDropsAField — переезд объявлен, разборчив и успешен, но теряет одно поле.
//
// Это форма, которую проверка «объявлен ли метод переезда» не ловит ВОВСЕ: объявление на
// месте, запрос проходит, план строится — а поля в записи арендатора нет. Ради неё предмет
// пробы назван исходом, а не наличием.
//
// # Почему теряется ОДНО поле, а не всё состояние
//
// Замер на terraform-plugin-framework v1.19.0: состояние, отданное ЦЕЛИКОМ пустым,
// исполнитель отличить от «объявление не подошло» не может — он сверяет ответ с пустым
// значением схемы и на совпадении идёт к следующему объявлению, а исчерпав их, отвечает
// отказом. То есть полная потеря состояния и так громкая. Тихая — частичная, и проверять
// надо её.
//
// Разборчивость повторена дословно (прежнее имя типа + наш провайдер): менять её значило бы
// ломать ВТОРОЕ утверждение заодно, и красное пришло бы не от проверяемого.
type moveProbeDropsAField struct{ resource.Resource }

func (r moveProbeDropsAField) MoveState(ctx context.Context) []resource.StateMover {
	var sr resource.SchemaResponse
	r.Resource.Schema(ctx, resource.SchemaRequest{}, &sr)
	src := sr.Schema
	retired := retiredResourceTypeNames[typeNameOfResource(r.Resource)]

	return []resource.StateMover{{
		SourceSchema: &src,
		StateMover: func(_ context.Context, req resource.MoveStateRequest, resp *resource.MoveStateResponse) {
			if req.SourceTypeName != retired || !sourceProviderIsOurs(req.SourceProviderAddress) {
				return
			}
			if req.SourceState == nil {
				return
			}
			attrs := map[string]tftypes.Value{}
			if err := req.SourceState.Raw.As(&attrs); err != nil {
				return
			}
			names := make([]string, 0, len(attrs))
			for n := range attrs {
				names = append(names, n)
			}
			// Поле выбирается по порядку имён, а не наугад: инъекция обязана быть
			// воспроизводимой, иначе её красное нечем повторить.
			sort.Strings(names)
			lost := names[0]
			attrs[lost] = tftypes.NewValue(attrs[lost].Type(), nil)

			moved := *req.SourceState
			moved.Raw = tftypes.NewValue(req.SourceState.Raw.Type(), attrs)
			resp.TargetState = moved
		},
	}}
}

// ---- форма 3: переезд выдан типу, имени не менявшему ---------------------------------------

// moveProbeBlanket — переезд, принимающий ЛЮБОЙ источник от нашего провайдера.
//
// Это то, во что превращается объявление переезда, если спрашивать носителя, а не словарь
// снятых имён: плоские ресурсы описываются одной реализацией, и типы платформы получили бы
// чужой переезд даром.
type moveProbeBlanket struct{ resource.Resource }

func (r moveProbeBlanket) MoveState(ctx context.Context) []resource.StateMover {
	var sr resource.SchemaResponse
	r.Resource.Schema(ctx, resource.SchemaRequest{}, &sr)
	src := sr.Schema

	return []resource.StateMover{{
		SourceSchema: &src,
		StateMover: func(_ context.Context, req resource.MoveStateRequest, resp *resource.MoveStateResponse) {
			if req.SourceState == nil {
				return
			}
			resp.TargetState = *req.SourceState
		},
	}}
}

// ---- прогоны -------------------------------------------------------------------------------

// moveProbeNamed — имена типов, о которых вынесены находки.
func moveProbeNamed(findings []moveProbeFinding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range findings {
		if seen[f.TypeName] {
			continue
		}
		seen[f.TypeName] = true
		out = append(out, f.TypeName)
	}
	return out
}

func moveProbeSaidAbout(findings []moveProbeFinding, typeName string) string {
	var out []string
	for _, f := range findings {
		if f.TypeName == typeName {
			out = append(out, f.What)
		}
	}
	return strings.Join(out, "\n")
}

// Прогон 1 — КОНТРОЛЬ: дерево цело, находок нет, перепись непуста.
//
// Без него молчание при инъекции было бы неотличимо от молчания мёртвой проверки: она
// обязана уметь и то и другое, и оба прогона названы врозь.
func TestMoveProbeIsSilentOnTheIntactTree(t *testing.T) {
	findings, census := moveProbeAudit(New())
	if len(findings) != 0 {
		t.Errorf("на целом дереве проба нашла %d нарушений — контроль не прошёл, и красное "+
			"при инъекции ничего не докажет:\n%v", len(findings), findings)
	}
	if census.Registered == 0 || census.DeclaredRenames == 0 || census.GuardedMove == 0 {
		t.Fatalf("перепись пуста (в реестре %d, объявлено %d, сторожится %d) — молчание "+
			"означало бы «не прочитано ничего», а не «всё цело»",
			census.Registered, census.DeclaredRenames, census.GuardedMove)
	}
	t.Logf("контроль: в реестре %d · объявлено переездов %d · сторожится %d · отказ "+
		"сторожится у %d", census.Registered, census.DeclaredRenames, census.GuardedMove,
		census.GuardedRefusal)
}

// Прогон 2 — ИНЪЕКЦИЯ по каждой из трёх форм, по одному факту за раз.
//
// Утверждается не только «покраснело», но и ЧТО напечатано и ПРО КОГО: находка, называющая
// общий счёт вместо имени типа, послала бы читателя искать поломку у всех девяти.
func TestMoveProbeRedensWithTheNameOfTheBrokenType(t *testing.T) {
	for _, c := range []struct {
		what     string
		typeName string
		wrap     func(resource.Resource) resource.Resource
		says     string
	}{
		{
			what: "объявление переезда снято", typeName: typeNameIAMRole,
			wrap: func(r resource.Resource) resource.Resource { return moveProbeStripped{r} },
			says: "переезд ОТВЕРГНУТ",
		},
		{
			// Форма, которой проверка «объявлен ли метод» не видит вовсе.
			what: "переезд объявлен, но теряет поле", typeName: typeNameIAMUserToken,
			wrap: func(r resource.Resource) resource.Resource { return moveProbeDropsAField{r} },
			says: "состояние переезд НЕ ПЕРЕЖИЛО",
		},
		{
			what: "переезд выдан типу, имени не менявшему", typeName: typeNameVPCNetwork,
			wrap: func(r resource.Resource) resource.Resource { return moveProbeBlanket{r} },
			says: "тип ПРИНЯЛ переезд",
		},
	} {
		t.Run(c.what, func(t *testing.T) {
			findings, census := moveProbeAudit(moveProbeWith(c.typeName, c.wrap))
			if len(findings) == 0 {
				t.Fatalf("проба смолчала на внесённом дефекте (%s у %s) — она не способна "+
					"его найти, и её зелёный на целом дереве ничего не значит",
					c.what, c.typeName)
			}

			named := moveProbeNamed(findings)
			if len(named) != 1 || named[0] != c.typeName {
				t.Errorf("находки названы про %v, а дефект внесён ровно у %s.\n"+
					"Инъекция обязана ронять ТОЛЬКО проверяемое: красное про соседей "+
					"означает, что покраснел не тот, кого ломали.\n%v",
					named, c.typeName, findings)
			}

			said := moveProbeSaidAbout(findings, c.typeName)
			if !strings.Contains(said, c.says) {
				t.Errorf("находка про %s не называет предмет %q — читатель уйдёт искать "+
					"не туда:\n%s", c.typeName, c.says, said)
			}

			// Перепись обязана ПОКАЗАТЬ недостачу, а не только напечатать находку: она
			// и есть та вторая величина, ради которой их две.
			switch c.says {
			case "переезд ОТВЕРГНУТ", "состояние переезд НЕ ПЕРЕЖИЛО":
				if census.GuardedMove >= census.DeclaredRenames {
					t.Errorf("перепись не заметила недостачи: объявлено %d, сторожится %d — "+
						"а переезд одного типа сломан", census.DeclaredRenames, census.GuardedMove)
				}
			case "тип ПРИНЯЛ переезд":
				if census.SharedCarrier == 0 && census.WithoutRename == 0 {
					t.Errorf("перепись не осмотрела ни одного типа без переименования")
				}
			}
			t.Logf("инъекция «%s» у %s: находок %d, сторожится %d из %d объявленных",
				c.what, c.typeName, len(findings), census.GuardedMove, census.DeclaredRenames)
		})
	}
}

// Прогон 3 — ЗАКОННЫЙ БЛИЗНЕЦ: сосед по ОБЩЕЙ реализации молчит, пока ломают не его.
//
// Носитель плоских ресурсов обслуживает и тип службы доступа, сменивший имя, и типы
// платформы, не менявшие его. Инъекция бьёт по ОДНОМУ типу; сосед по реализации обязан
// остаться вне находок — иначе проба судит носителя, а не тип, и «сторожится девять»
// означало бы «сторожится семь».
func TestMoveProbeLeavesTheCarrierNeighbourAlone(t *testing.T) {
	// Сосед выводится из реестра, а не выписывается: выписанный отстал бы молча при
	// первом же новом плоском ресурсе.
	ctx := context.Background()
	p := New().(*kachoProvider)
	var brokenCarrier, neighbour string
	for _, ctor := range p.Resources(ctx) {
		r := ctor()
		if typeNameOfResource(r) == typeNameIAMAccount {
			brokenCarrier = typeOfResourceImpl(r)
		}
	}
	if brokenCarrier == "" {
		t.Fatalf("в реестре нет типа %s — близнеца не от кого отсчитывать", typeNameIAMAccount)
	}
	for _, ctor := range p.Resources(ctx) {
		r := ctor()
		name := typeNameOfResource(r)
		if retiredResourceTypeNames[name] == "" && typeOfResourceImpl(r) == brokenCarrier {
			neighbour = name
			break
		}
	}
	if neighbour == "" {
		t.Fatalf("у носителя %s нет ни одного типа без переименования — близнеца не "+
			"существует, и «проба судит тип, а не носителя» проверить нечем", brokenCarrier)
	}

	findings, _ := moveProbeAudit(moveProbeWith(typeNameIAMAccount,
		func(r resource.Resource) resource.Resource { return moveProbeStripped{r} }))

	named := moveProbeNamed(findings)
	if len(named) != 1 || named[0] != typeNameIAMAccount {
		t.Fatalf("сломан %s, а находки названы про %v", typeNameIAMAccount, named)
	}
	if said := moveProbeSaidAbout(findings, neighbour); said != "" {
		t.Errorf("сосед по носителю %s (%s) попал в находки, хотя ломали не его:\n%s",
			neighbour, brokenCarrier, said)
	}
	t.Logf("близнец: носитель %s, сломан %s, сосед %s — молчит",
		brokenCarrier, typeNameIAMAccount, neighbour)
}

// Прогон 4 — ПУСТОЙ ОБХОД виден в переписи, а не выглядит как «всё цело».
//
// Провайдер без ресурсов не даёт находок — и не может их дать: судить нечего. Молчание
// здесь обязано быть отличимо от молчания на целом дереве, иначе «переезд у всех типов
// цел» означало бы «типов не прочитано ни одного». Отличает их ПЕРЕПИСЬ, и потому проба
// падает на нулевой, а не радуется отсутствию находок.
func TestMoveProbeShowsAnEmptyWalkInTheCensus(t *testing.T) {
	findings, census := moveProbeAudit(moveProbeEmptyRegistry{New()})

	if len(findings) != 0 {
		t.Errorf("на пустом реестре найдено %d нарушений — находки взялись не из обхода:\n%v",
			len(findings), findings)
	}
	if census.Registered != 0 || census.GuardedMove != 0 || census.GuardedRefusal != 0 {
		t.Errorf("перепись пустого реестра непуста (в реестре %d, сторожится %d, отказ %d) — "+
			"она считает не то, что обходит", census.Registered, census.GuardedMove,
			census.GuardedRefusal)
	}
	// Словарь снятых имён от реестра не зависит: он объявление, а не обход. Именно
	// поэтому «объявлено 9, сторожится 0» — читаемая недостача, а не пустая строка.
	if census.DeclaredRenames == 0 {
		t.Fatal("словарь снятых имён пуст — недостачу не с чем сравнить")
	}
	t.Logf("пустой обход: в реестре %d · объявлено переездов %d · сторожится %d — "+
		"недостача видна", census.Registered, census.DeclaredRenames, census.GuardedMove)
}

// moveProbeEmptyRegistry — провайдер, не объявляющий ни одного ресурса.
type moveProbeEmptyRegistry struct{ provider.Provider }

func (moveProbeEmptyRegistry) Resources(context.Context) []func() resource.Resource { return nil }
