// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// refusal_test.go — доказательство того, что отказы старта, живущие в
// дескрипторе, СПОСОБНЫ упасть, и что падают они на существе, а не на форме.
//
// Каждая проба — ПАРА: инъекция настоящим входом (дескриптор краснеет и
// НАЗЫВАЕТ ось) и законный близнец той же формы (дескриптор молчит). Отрицание
// в одиночку зеленеет сильнее всего тогда, когда сломано всё, поэтому пары
// обязательны.
//
// Вход у каждой пробы СИНТЕТИЧЕСКИЙ, а не «дескриптор переведённого сервиса»:
// у первого переведённого сервиса производителя половины этих входов нет вовсе
// (ноль сужаемых методов, ноль скрывающих типов), и прогон на нём подтвердил бы
// только то, что отказ бывает.
package servicecontract_test

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// tlsLike — транспорт, который САМ О СЕБЕ сообщает, что он проверенный.
//
// Подделкой это не является и делает пробу строже, а не слабее: предикат
// «проверен ли транспорт» читает `Info().SecurityProtocol` у самих креденшелов —
// то есть ответ транспорта, а не слово вызывающего. Настоящий `credentials.NewTLS`
// потребовал бы сертификатов, и проба меряла бы их загрузку вместо предиката.
type tlsLike struct {
	credentials.TransportCredentials
}

func (tlsLike) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "tls"}
}

// lawful — законный дескриптор той же формы, от которого отталкивается КАЖДАЯ
// инъекция. Он обязан приниматься: без этого положительного контроля любое
// отрицание ниже зеленело бы на полностью сломанном конструкторе.
func lawful() servicecontract.Spec {
	return servicecontract.Spec{
		Service:    "kacho-demo",
		Mode:       servicecontract.ModeProduction,
		Forwarders: grpcsrv.NewTrustedForwarders("spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "KACHO_DEMO_AUTHZ_TRUSTED_FORWARDER_SANS",
			TrustAny: "KACHO_DEMO_AUTHZ_TRUST_ANY_FORWARDER",
		},
		Authz:          servicecontract.AuthzViaIAM,
		CheckEdge:      servicecontract.NewPeerEdge("kacho-iam-internal:9091", tlsLike{}),
		CacheWindow:    5 * time.Second,
		ClientBudget:   5 * time.Second,
		HandlingBudget: 30 * time.Second,
		DBSSLMode:      "require",
		PublicAddr:     ":9090",
		InternalAddr:   ":9091",
		PublicCreds:    tlsLike{},
		InternalCreds:  tlsLike{},

		Emits:         servicecontract.NotApplicable[[]proxytuple.Relation]("демо ничего не регистрирует у владельца прав"),
		Registers:     servicecontract.NotApplicable[[]servicecontract.ObjectType]("демо не владеет ни одним типом объекта модели"),
		Narrowers:     servicecontract.NotApplicable[map[servicecontract.MethodFQN]servicecontract.ListNarrower]("ни один метод демо не сужается по правам"),
		HideExistence: servicecontract.NotApplicable[map[servicecontract.ObjectType]servicecontract.NotFoundFormat]("демо не скрывает существование ни одного типа"),
		Delivery:      servicecontract.NotApplicable[servicecontract.DeliveryProvenance]("демо ничего не регистрирует у владельца прав"),
		DenyBudget:    servicecontract.Value(100.0),
		BootGate:      servicecontract.NotApplicable[servicecontract.BootGate]("демо ничего не эмитит владельцу прав, поднимать нечего"),
	}
}

// TestLawfulSpecIsAccepted — положительный контроль. Стоит ПЕРВЫМ намеренно:
// пока он красный, каждая проба ниже утверждает «отказано» по причине, не
// имеющей отношения к своему предмету.
func TestLawfulSpecIsAccepted(t *testing.T) {
	if _, err := servicecontract.New(lawful()); err != nil {
		t.Fatalf("законный дескриптор отвергнут — все отрицания ниже вакуумны: %v", err)
	}
}

// refuses прогоняет инъекцию и требует, чтобы отказ НАЗЫВАЛ предмет: находка
// без координаты не является действием.
func refuses(t *testing.T, s servicecontract.Spec, mustName ...string) string {
	t.Helper()
	_, err := servicecontract.New(s)
	if err == nil {
		t.Fatalf("дескриптор принят — отказ не способен упасть")
	}
	for _, want := range mustName {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("отказ не называет %q — по нему нечего чинить:\n%v", want, err)
		}
	}
	return err.Error()
}

// ── О1: круг отправителей пуст по тому же предикату, что читает транспорт ────

func TestO1_UnnarrowedForwarderCircleRefusesStart(t *testing.T) {
	s := lawful()
	s.Forwarders = grpcsrv.TrustedForwarders{}
	msg := refuses(t, s, "KACHO_DEMO_AUTHZ_TRUSTED_FORWARDER_SANS")
	t.Logf("О1 (а) красный: %s", msg)
}

// TestO1_DegenerateValueIsEmptyForAllThree — третий обязательный контроль
// сценария A5: вырожденное значение обязано дать ОДИН И ТОТ ЖЕ ответ «пусто» у
// стражи и у транспорта. Величина здесь одна by construction (тип), и проба это
// ФИКСИРУЕТ, а не предполагает.
//
// # Что здесь вход, а что было бы подделкой
//
// Канонический вход инъекции — ручка со значением `","`. В круг она попадает НЕ
// одной строкой: разбор окружения режет её по запятой, и до типа доезжают ДВЕ
// ПУСТЫЕ записи. Первая редакция этой пробы передавала `","` одним элементом —
// то есть законный (хоть и бессмысленный) SAN, — и краснела на том, что кругом
// он и является. Ноль был бы получен по причине, не имеющей отношения к
// предмету; поэтому разбор здесь воспроизводится тем же `strings.Split`, каким
// его делает загрузка конфигурации, а не подставляется руками.
func TestO1_DegenerateValueIsEmptyForAllThree(t *testing.T) {
	const degenerateKnobValue = ","
	raw := strings.Split(degenerateKnobValue, ",")
	if len(raw) != 2 || raw[0] != "" || raw[1] != "" {
		t.Fatalf("вход пробы собран не так, как его собирает разбор окружения: %q", raw)
	}
	f := grpcsrv.NewTrustedForwarders(raw...)
	if f.IsNarrowed() {
		t.Fatalf("вырожденное значение ручки сочтено суженным кругом — страж меряет не то, что транспорт")
	}
	if len(f.SANs()) != 0 {
		t.Fatalf("транспорт получил %d записей из вырожденного значения", len(f.SANs()))
	}
	s := lawful()
	s.Forwarders = f
	refuses(t, s, "KACHO_DEMO_AUTHZ_TRUSTED_FORWARDER_SANS")
}

// TestO1_DevOptInIsNotHonouredInProduction — четвёртый контроль A5: тот же
// пустой круг в боевом режиме С ИСПРОШЕННЫМ опт-ином. Отказ обязан остаться —
// иначе сценарий подтверждает лишь то, что отказ бывает, и молчит о
// ЕДИНСТВЕННОМ способе его не получить.
func TestO1_DevOptInIsNotHonouredInProduction(t *testing.T) {
	s := lawful()
	s.Forwarders = grpcsrv.TrustedForwarders{}
	s.ForwarderKnobs.OptIn = true
	refuses(t, s, "KACHO_DEMO_AUTHZ_TRUSTED_FORWARDER_SANS")

	// Законный близнец: тот же опт-ин ВНЕ боевого режима принимается.
	s.Mode = servicecontract.ModeDev
	s.DBSSLMode = "disable"
	if _, err := servicecontract.New(s); err != nil {
		t.Fatalf("опт-ин вне боевого режима отвергнут — отказ ловит форму, а не существо: %v", err)
	}
}

// ── О6: ребро проверки прав объявлено, адрес/транспорт не задан явно ─────────

func TestO6_CheckEdgeWithoutAddressRefusesStart(t *testing.T) {
	s := lawful()
	s.CheckEdge = servicecontract.NewPeerEdge("", tlsLike{})
	refuses(t, s, "CheckEdge")
}

// TestO8_CheckEdgeOnInsecureTransportRefusesProductionStart — транспорт ребра
// решения о доступе судится ПОСАДКОЙ (О8), а не формой объявления (О6).
//
// Разделение не косметическое и проверяется обеими сторонами: «объявлено ли
// ребро явно» от режима не зависит и остаётся в О6; «проверен ли транспорт» —
// измерение посадки, и живёт там же, где sslmode и транспорт слушателей. Так у
// каждого предмета ровно одно правило.
//
// Предикат читает ответ САМИХ креденшелов, а не ручку конфигурации: сборщик на
// невзведённой ручке отдаёт незашифрованные креды БЕЗ ошибки, поэтому «ручка
// взведена, а креды выродились» им не проходит.
func TestO8_CheckEdgeOnInsecureTransportRefusesProductionStart(t *testing.T) {
	s := lawful()
	s.CheckEdge = servicecontract.NewPeerEdge("kacho-iam-internal:9091", insecure.NewCredentials())
	refuses(t, s, "CheckEdge")

	// Законный близнец: вне боевой посадки тот же транспорт принимается. Без
	// этой половины отказ был бы безусловным, и первая же фаза начальной
	// установки — та, что поднимает стенд с выключенным mTLS раньше, чем
	// выпущены сертификаты, — упёрлась бы в него навсегда.
	s.Mode = servicecontract.ModeDev
	s.DBSSLMode = "disable"
	if _, err := servicecontract.New(s); err != nil {
		t.Fatalf("небоевая посадка отвергнута боевым правилом: %v", err)
	}
}

// TestO6_SelfAuthzNeedsNoEdge — законный близнец: владелец модели ребра к себе
// не объявляет, и это НЕ находка. Без этой половины О6 отвергал бы законное.
func TestO6_SelfAuthzNeedsNoEdge(t *testing.T) {
	s := lawful()
	s.Authz = servicecontract.AuthzSelf
	s.CheckEdge = servicecontract.PeerEdge{}
	s.SelfCheck = stubCheck{}
	if _, err := servicecontract.New(s); err != nil {
		t.Fatalf("владелец модели отвергнут за отсутствие ребра к самому себе: %v", err)
	}
}

// TestSourceOfDecisionHasExactlyOneCarrier — пара «ребро / свой решатель»
// исчерпывающая и ВЗАИМОИСКЛЮЧАЮЩАЯ.
//
// Обе половины несущие. Без первой «сам себе владелец» оказался бы веткой,
// которую носитель не умеет поднять, и её пришлось бы дописывать заглушкой.
// Без второй в одном процессе жили бы ДВА источника решения о доступе, и какой
// из них действует, не было бы записано нигде.
func TestSourceOfDecisionHasExactlyOneCarrier(t *testing.T) {
	t.Run("владелец без решателя", func(t *testing.T) {
		s := lawful()
		s.Authz = servicecontract.AuthzSelf
		s.CheckEdge = servicecontract.PeerEdge{}
		refuses(t, s, "SelfCheck")
	})
	t.Run("потребитель с чужим решателем", func(t *testing.T) {
		s := lawful()
		s.SelfCheck = stubCheck{}
		refuses(t, s, "SelfCheck")
	})
}

// ── О7: окно кэша вердиктов не задано явно ──────────────────────────────────

func TestO7_UndeclaredCacheWindowRefusesStart(t *testing.T) {
	s := lawful()
	s.CacheWindow = 0
	refuses(t, s, "CacheWindow")
}

func TestO7_UndeclaredClientBudgetRefusesStart(t *testing.T) {
	s := lawful()
	s.ClientBudget = 0
	refuses(t, s, "ClientBudget")
}

// ── О8: боевая посадка ───────────────────────────────────────────────────────

func TestO8_ProductionRefusesInsecureListener(t *testing.T) {
	s := lawful()
	s.InternalCreds = insecure.NewCredentials()
	refuses(t, s, "InternalCreds")
}

func TestO8_ProductionRefusesWeakSSLMode(t *testing.T) {
	for _, mode := range []string{"", "disable", "allow", "prefer"} {
		s := lawful()
		s.DBSSLMode = mode
		refuses(t, s, "DBSSLMode")
	}
}

// TestO8_NonProductionAcceptsWhatProductionRefuses — законный близнец: та же
// посадка вне боевого режима принимается. Без него О8 был бы неотличим от
// безусловного запрета, и первый же локальный прогон его отключил бы.
func TestO8_NonProductionAcceptsWhatProductionRefuses(t *testing.T) {
	s := lawful()
	s.Mode = servicecontract.ModeDev
	s.DBSSLMode = "disable"
	s.InternalCreds = insecure.NewCredentials()
	s.PublicCreds = insecure.NewCredentials()
	if _, err := servicecontract.New(s); err != nil {
		t.Fatalf("небоевая посадка отвергнута боевым правилом: %v", err)
	}
}

// TestO8_ProductionAcceptsStrongSSLModes — вторая половина: то, что боевой
// режим ОБЯЗАН пропускать.
func TestO8_ProductionAcceptsStrongSSLModes(t *testing.T) {
	for _, mode := range []string{"require", "verify-ca", "verify-full"} {
		s := lawful()
		s.DBSSLMode = mode
		if _, err := servicecontract.New(s); err != nil {
			t.Fatalf("боевой режим отверг законный sslmode %q: %v", mode, err)
		}
	}
}

// ── О10: ось пуста и не объявлена «не применимо, потому что …» ───────────────

func TestO10_UndeclaredAxisRefusesStartNamingIt(t *testing.T) {
	// Каждая из четырёх осей — своя инъекция: О10, отказывающий по первой
	// попавшейся, оставил бы остальные три без производителя входа.
	t.Run("Emits", func(t *testing.T) {
		s := lawful()
		s.Emits = servicecontract.Axis[[]proxytuple.Relation]{}
		refuses(t, s, "Emits")
	})
	t.Run("Registers", func(t *testing.T) {
		s := lawful()
		s.Registers = servicecontract.Axis[[]servicecontract.ObjectType]{}
		refuses(t, s, "Registers")
	})
	t.Run("Narrowers", func(t *testing.T) {
		s := lawful()
		s.Narrowers = servicecontract.Axis[map[servicecontract.MethodFQN]servicecontract.ListNarrower]{}
		refuses(t, s, "Narrowers")
	})
	t.Run("HideExistence", func(t *testing.T) {
		s := lawful()
		s.HideExistence = servicecontract.Axis[map[servicecontract.ObjectType]servicecontract.NotFoundFormat]{}
		refuses(t, s, "HideExistence")
	})
}

// TestO10_EmptyReasonIsNotADeclaration — «не применимо» без причины объявлением
// не является. Иначе ось закрывалась бы пустой строкой, то есть тем же
// умолчанием, от которого Axis и заводился.
func TestO10_EmptyReasonIsNotADeclaration(t *testing.T) {
	s := lawful()
	s.Emits = servicecontract.NotApplicable[[]proxytuple.Relation]("")
	refuses(t, s, "Emits")
}

// TestO10_NamesEveryUndeclaredAxisAtOnce — отказ обязан перечислить ВСЕ
// незаполненные оси, а не первую. Иначе автор чинит по одной и узнаёт о
// следующей только на следующем старте — четыре перезапуска вместо одного.
func TestO10_NamesEveryUndeclaredAxisAtOnce(t *testing.T) {
	s := lawful()
	s.Emits = servicecontract.Axis[[]proxytuple.Relation]{}
	s.Registers = servicecontract.Axis[[]servicecontract.ObjectType]{}
	s.CacheWindow = 0
	msg := refuses(t, s, "Emits", "Registers", "CacheWindow")
	t.Logf("отказ назвал все три оси разом:\n%s", msg)
}

// ── #130: признак первой доставки — поле дескриптора, судимое соседкой ───────

// TestDeliveryProvenanceIsRequiredWhenTheServiceEmits — поле имеет ЧИТАТЕЛЯ:
// сервис, который эмитит намерения, обязан сказать, откуда они происходят.
// Объявленное-и-никем-не-читаемое поле — запрещённый мёртвый страж (core #16).
func TestDeliveryProvenanceIsRequiredWhenTheServiceEmits(t *testing.T) {
	s := lawful()
	s.Emits = servicecontract.Value([]proxytuple.Relation{proxytuple.RelationProject})
	s.Delivery = servicecontract.Axis[servicecontract.DeliveryProvenance]{}
	refuses(t, s, "Delivery")

	// Законный близнец: тот же эмиттер, объявивший происхождение, принимается.
	s.Delivery = servicecontract.Value(servicecontract.DeliveryWriterTransaction)
	if _, err := servicecontract.New(s); err != nil {
		t.Fatalf("объявленное происхождение отвергнуто: %v", err)
	}
}

// TestDeliveryProvenanceCannotBeClaimedByANonEmitter — зеркальная половина, и
// она несущая: происхождение доставки, объявленное сервисом, который ничего не
// доставляет, — утверждение без предмета. Ось судится соседкой, поэтому
// исключение истекает само.
func TestDeliveryProvenanceCannotBeClaimedByANonEmitter(t *testing.T) {
	s := lawful()
	s.Delivery = servicecontract.Value(servicecontract.DeliveryWriterTransaction)
	refuses(t, s, "Delivery")
}

// ── существование объекта: порт судится осью скрытия ────────────────────────

func TestExistenceProbeIsRequiredWhenTheServiceHidesExistence(t *testing.T) {
	s := lawful()
	s.HideExistence = servicecontract.Value(map[servicecontract.ObjectType]servicecontract.NotFoundFormat{
		"demo_widget": "Widget %s not found",
	})
	refuses(t, s, "Existence")

	s.Existence = stubProbe{}
	if _, err := servicecontract.New(s); err != nil {
		t.Fatalf("объявленное скрытие с провязанным портом отвергнуто: %v", err)
	}
}

// TestExistenceProbeWithoutHidingIsRefused — зеркало: провязанный порт у
// сервиса, который ничего не скрывает, — проводка, которую никто не спросит.
func TestExistenceProbeWithoutHidingIsRefused(t *testing.T) {
	s := lawful()
	s.Existence = stubProbe{}
	refuses(t, s, "Existence")
}

// ── имя сервиса и режим — обязательные поля, а не оси ───────────────────────

func TestServiceNameAndModeAreRequired(t *testing.T) {
	t.Run("Service", func(t *testing.T) {
		s := lawful()
		s.Service = ""
		refuses(t, s, "Service")
	})
	t.Run("Mode", func(t *testing.T) {
		s := lawful()
		s.Mode = 0
		refuses(t, s, "Mode")
	})
	t.Run("Logger", func(t *testing.T) {
		s := lawful()
		s.Logger = nil
		if _, err := servicecontract.New(s); err != nil {
			t.Fatalf("нулевой журнал обязан резолвиться в журнал по умолчанию, а не отказывать: %v", err)
		}
	})
}

// TestDescriptorCannotBeAssembledByLiteral — форма дескриптора: принятый Spec
// литералом не собирается, поэтому «собрал структуру мимо конструктора» —
// не обход правила, а невыразимость.
func TestDescriptorCannotBeAssembledByLiteral(t *testing.T) {
	var d servicecontract.Descriptor
	if d.Accepted() {
		t.Fatal("нулевой дескриптор объявил себя принятым — конструктор обходится литералом")
	}
	got, err := servicecontract.New(lawful())
	if err != nil {
		t.Fatalf("законный дескриптор отвергнут: %v", err)
	}
	if !got.Accepted() {
		t.Fatal("принятый дескриптор не объявляет себя принятым")
	}
}
