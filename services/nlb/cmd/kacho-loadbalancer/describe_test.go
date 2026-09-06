// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// describe_test.go — дескриптор процесса собирается ЗДЕСЬ, и здесь же он обязан
// проходить конструктор.
//
// # Предмет, и он найден дорогой ценой
//
// Носитель сделал три поля дескриптора обязательными, а композиционный корень
// первого переведённого сервиса их не объявил. Сборка молчала (отказ
// рантаймовый), прогон носителя молчал (корень сервиса вне его области), прогон
// самого сервиса молчал тоже — `describe()` не звал НИ ОДИН тест. Итог: ветка
// ломала подъём единственного сервиса на носителе, и ни одна проверка не могла об
// этом сказать.
//
// Отсюда правило, которое эти пробы исполняют: у дескриптора обязан быть
// потребитель в прогоне — тот же конструктор, который решает его судьбу на
// старте, и на той же конфигурации, с какой поднимается процесс. Свойство «каждый
// композиционный корень проходит конструктор» держится не этим файлом, а гейтом
// по дереву (`internal/repohygiene`, TestEveryDescriptorHasAProbe): один файл
// закрывает один сервис, гейт — тех, кого ещё не написали.

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

const probeGatewaySAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"

// bootConfig — конфигурация, загруженная ТЕМ ЖЕ вызовом, что и на старте
// (`config.Load` из переменных окружения; путь к файлу пуст — ConfigMap'а в
// прогоне нет).
//
// Литерал `config.Config{…}` здесь был бы ДРУГОЙ величиной: он обошёл бы
// умолчания, а половина полей дескриптора приезжает именно из них — и проба
// утверждала бы про конфигурацию, которой не бывает ни на одной посадке. Ровно
// так пропала бы, например, величина бюджета отказов: она приезжает умолчанием.
func bootConfig(t *testing.T, env map[string]string) *config.Config {
	t.Helper()
	// Имя переменной круга отправителей — с ДЕФИСАМИ, и это не описка: viper
	// заменяет на `__` только точки, дефисы в имени ключа остаются как есть, а
	// явного BindEnv у этого ключа нет (в отличие от `trust-any-forwarder`).
	// Замерено загрузкой обоих написаний: подчёркивания НЕ биндятся. Форма с
	// подчёркиваниями, которую называют комментарии конфигурации, — отдельная
	// находка, и здесь она не воспроизводится, иначе проба зеленела бы на круге,
	// которого процесс не получил.
	base := map[string]string{
		"KACHO_NLB_MODE":                          "dev",
		"KACHO_NLB_REPOSITORY__POSTGRES__URL":     "postgres://u:p@pg-nlb:5432/kacho_nlb?sslmode=require",
		"KACHO_NLB_EXTAPI__IAM__INTERNAL_ADDR":    "kaname-internal:9091",
		"KACHO_NLB_EXTAPI__IAM__ADDR":             "kaname:9090",
		"KACHO_NLB_AUTHZ__TRUSTED-FORWARDER-SANS": probeGatewaySAN,
		// Имя ЭТОЙ переменной — с подчёркиваниями, и это не описка рядом с дефисами
		// выше: ключ домена привязан явным BindEnv (defaults.go), а круг выше
		// приезжает общей заменой приставки, которая дефисы не трогает. Померено
		// загрузкой обоих написаний.
		"KACHO_NLB_AUTHZ__TRUST_DOMAIN": "kacho.cloud",
	}
	for k, v := range env {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
	c, err := config.Load("")
	if err != nil {
		t.Fatalf("конфигурация не загрузилась: %v", err)
	}
	return c
}

// probeNarrower / probeGate — то, что композиционный корень приносит дескриптору
// помимо конфигурации. Оба строятся теми же конструкторами, что на старте.
func probeNarrower() *authzfilter.Narrower {
	return authzfilter.New(nil, authzfilter.Config{Breakglass: true})
}

func probeGate() *bootgate.Gate {
	return bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-nlb"})
}

// TestDescribeIsAcceptedByTheConstructor — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ, и он первым:
// пока он красный, процесс не поднимается вовсе, а всякое отрицание ниже зеленеет
// по чужой причине.
func TestDescribeIsAcceptedByTheConstructor(t *testing.T) {
	desc, err := describe(bootConfig(t, nil), quietLogger(), probeNarrower(), probeGate(), probeExistence{}, probeAuthzObserve, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор kacho-nlb отвергнут конструктором — процесс НЕ ПОДНИМЕТСЯ:\n%v", err)
	}
	if !desc.Accepted() {
		t.Fatal("дескриптор собран литералом в обход конструктора: носитель откажется по нему поднимать процесс")
	}

	s := desc.Spec()

	// Верхняя граница обработки вызова объявлена ВЕЛИЧИНОЙ.
	if s.HandlingBudget <= 0 {
		t.Fatalf("верхняя граница обработки не объявлена (%v)", s.HandlingBudget)
	}

	// Бюджет отказов — ТА ЖЕ величина, что стояла в композиционном корне до
	// перевода (`DenyRateLimitPerSec: 100`). Утверждается ЧИСЛО, а не «ось
	// объявлена»: рубеж пропал ровно тем способом, при котором «объявлено»
	// осталось бы верным — величина исчезла, а форма нет.
	budget, ok := s.DenyBudget.Get()
	if !ok {
		because, na := s.DenyBudget.NotApplicableBecause()
		t.Fatalf("бюджет отказов объявлен изъятием (%q, na=%v), а решение о доступе nlb принимает "+
			"вопросом к kaname — шторм отказов есть кому ронять", because, na)
	}
	if budget != 100 {
		t.Fatalf("темп отсечки шторма отказов %v/с, а до перевода на носитель он был 100/с: "+
			"ноль читается механизмом как «ограничения нет», любое другое число — молчаливая "+
			"смена рубежа", budget)
	}

	// Загрузочный гейт мутаций принесён ЗНАЧЕНИЕМ, а не изъятием: nlb эмитит
	// намерения регистрации и служит тенантские Create.
	gate, gok := s.BootGate.Get()
	if !gok || gate == nil {
		t.Fatal("ось загрузочного гейта не несёт значения: в окне, пока путь доставки намерений " +
			"не поднят, ресурс создавался бы без владельца")
	}

	// Перепись объявленного: «ось объявлена» и «в ней столько-то записей» —
	// разные утверждения, и второе обязано быть видно в логе.
	wired, _ := s.Narrowers.Get()
	emits, _ := s.Emits.Get()
	registers, _ := s.Registers.Get()
	t.Logf("дескриптор принят: граница обработки %v, бюджет отказов %v/с, гейт мутаций принесён, "+
		"проводок сужателя %d, эмитируемых отношений %d, регистрируемых типов %d",
		s.HandlingBudget, budget, len(wired), len(emits), len(registers))
	if len(wired) == 0 {
		t.Fatal("проводок сужателя ноль: у nlb три списочных метода объявлены сужаемыми в каталоге " +
			"прав, и за ними нет пообъектной проверки на крае — отсутствующий сужатель означает " +
			"не «строже», а «без рубежа»")
	}
}

// TestDescribeProbeCanFail — контроль того, что проба выше СПОСОБНА упасть.
//
// Без него «дескриптор принят» неотличимо от «конструктор ничего не проверяет»:
// положительное утверждение, у которого нет отрицательного близнеца, зеленеет
// одинаково на исправном и на выключенном.
func TestDescribeProbeCanFail(t *testing.T) {
	// Ребро решения о доступе не названо — отказ О6. Оба ключа пусты: адрес
	// резолвится firstNonEmpty(internal, public), и обнулить надо ОБА, иначе
	// проба зеленела бы на живом ребре.
	cfg := bootConfig(t, map[string]string{
		"KACHO_NLB_EXTAPI__IAM__INTERNAL_ADDR": "",
		"KACHO_NLB_EXTAPI__IAM__ADDR":          "",
	})
	_, err := describe(cfg, quietLogger(), probeNarrower(), probeGate(), probeExistence{}, probeAuthzObserve, prometheus.NewRegistry())
	if err == nil {
		t.Fatal("дескриптор без ребра решения о доступе принят — конструктор не судит ничего, " +
			"и положительная проба выше вакуумна")
	}
	if !strings.Contains(err.Error(), "CheckEdge") {
		t.Fatalf("отказ не называет предмета — чинить по нему нечего:\n%v", err)
	}
}

// TestDeclaredCircleIsTheOneTheProcessCarries — круг отправителей, объявленный
// конфигурацией, доезжает до дескриптора СВОИМ предикатом, а пустой круг без
// явного опт-ина роняет старт.
//
// Здесь стояла (в `cert_bound_identity_test.go`) проверка текстом исходника: что
// каждая из четырёх цепочек берёт пару извлечения личности у общего конструктора.
// Её предмет исчез вместе с цепочками: собрать свою пару композиционный корень
// больше НЕ МОЖЕТ — поля интерсепторного типа в дескрипторе нет, а цепочку ставит
// носитель (порядок и полнота пары заперты в `pkg/servicehost`,
// `TestForwardedIdentityIsHonouredOnlyFromTheCircle` и
// `TestUnnarrowedCircleWouldHonourAnyVerifiedPeer`). Осталось ровно то, что
// носителю знать неоткуда: КАКОЙ круг приносит этот сервис — и что пустой он
// принести не вправе.
func TestDeclaredCircleIsTheOneTheProcessCarries(t *testing.T) {
	desc, err := describe(bootConfig(t, nil), quietLogger(), probeNarrower(), probeGate(), probeExistence{}, probeAuthzObserve, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	circle, _ := desc.Spec().Forwarders.Get()
	if !circle.IsNarrowed() {
		t.Fatal("объявленный круг отправителей не сужен: пустой круг означает «принимаем " +
			"переданную личность от любого проверенного пира», а не «ни от кого»")
	}

	// Отрицание в паре с положительным: тот же путь на пустом круге и без явного
	// опт-ина обязан ронять старт, называя ручку. Отказ приходит РАНЬШЕ дескриптора
	// — от стражи конфигурации, — и это не обход утверждения: предмет здесь «пустой
	// круг не доживает до слушателей», а какая из двух страж его остановит, для
	// вызывающего безразлично. Обе читают ОДИН предикат (`IsNarrowed`).
	t.Setenv("KACHO_NLB_MODE", "dev")
	t.Setenv("KACHO_NLB_REPOSITORY__POSTGRES__URL", "postgres://u:p@pg-nlb:5432/kacho_nlb?sslmode=require")
	t.Setenv("KACHO_NLB_EXTAPI__IAM__INTERNAL_ADDR", "kaname-internal:9091")
	t.Setenv("KACHO_NLB_AUTHZ__TRUSTED-FORWARDER-SANS", "")
	if _, lerr := config.Load(""); lerr == nil {
		t.Fatal("пустой круг принят без явного опт-ина — сужения нет, а выглядит оно как есть")
	} else if !strings.Contains(lerr.Error(), "trusted-forwarder-sans") {
		t.Fatalf("отказ не называет ручку — оператор не поймёт, что крутить:\n%v", lerr)
	}
}

// TestCarrierRaisesTheService — БОЕВАЯ проба перевода: носитель обязан поднять
// kacho-nlb по его дескриптору и его же регистраторам.
//
// Она прогоняет ВСЕ отказы старта, которым нужен служимый набор RPC (О2, О3, О4,
// О5, О5б, О9, О11): носитель собирает оба сервера, зовёт наши регистраторы,
// снимает служимый набор у самих серверов, выводит домены и каталог и судит
// объявленное против служимого. Ни дескриптор сам по себе, ни прогон носителя на
// своих фикстурах этого не делают — только пара «наш дескриптор × наши
// регистраторы».
//
// Контекст отменён заранее: если носитель дойдёт до слушателей, он тут же их
// погасит и вернёт управление. Портов проба не занимает, к БД не ходит —
// обработчики строятся с нулевыми зависимостями, потому что предмет здесь —
// НАБОР служимых методов, а не их поведение.
func TestCarrierRaisesTheService(t *testing.T) {
	cfg := bootConfig(t, map[string]string{
		// Слушатели на эфемерных портах локального интерфейса: если старт всё же
		// дойдёт до них, проба не подерётся за 9090/9091 с чужим прогоном.
		"KACHO_NLB_API_SERVER__ENDPOINT":          "tcp://127.0.0.1:0",
		"KACHO_NLB_API_SERVER__INTERNAL_ENDPOINT": "tcp://127.0.0.1:0",
	})
	desc, err := describe(cfg, quietLogger(), probeNarrower(), probeGate(), probeExistence{}, probeAuthzObserve, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}

	// Обработчики строятся с ПУСТЫМИ зависимостями: предмет пробы — НАБОР
	// служимых методов и его сходимость с каталогом прав, а не поведение
	// обработчиков. Пустой bundle пиров нужен, потому что регистрация читает его
	// поля; ни один вызов по проводу здесь не делается.
	w := grpcWiring{
		cfg:     cfg,
		logger:  quietLogger(),
		peers:   &peerClients{},
		repo:    kachopg.New(nil, nil),
		opsRepo: operations.NewRepo(nil, "kacho_nlb"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if serr := servicehost.Serve(ctx, desc,
		func(reg grpc.ServiceRegistrar) { registerPublic(reg, w) },
		func(reg grpc.ServiceRegistrar) { registerInternal(reg, w) },
	); serr != nil {
		t.Fatalf("носитель не поднимает kacho-nlb по его собственному дескриптору и регистраторам "+
			"— то есть перевод не завершён, каким бы зелёным ни был всё остальное:\n%v", serr)
	}
}

// probeExistence — порт сверки существования для проб композиционного корня.
//
// Отвечает «объекта нет» на всё: предмет этих проб — отказы старта, которые
// носитель считает ДО первого соединения, и до вопроса к базе дело не доходит.
// Настоящая проба живёт на пуле (`internal/repo/kacho/pg`); подменять её здесь
// поведением значило бы подменить предмет — конструктор требует ПРИНЕСЁННЫЙ порт,
// а не работающий.
type probeExistence struct{}

func (probeExistence) ObjectExists(context.Context, string, string) (bool, error) {
	return false, nil
}

// ProbeableTypes — охват ДЕЛЕГИРУЕТСЯ настоящей пробе сервиса.
//
// Подделка не вправе быть снисходительнее продукта: объяви она свой перечень —
// и сверка охвата на старте (`servicehost`, О5в) судила бы фикстуру вместо
// пробы, то есть молчала бы ровно там, где таблица настоящей разошлась с картой
// прав сервиса (задача продукта #1931).
func (probeExistence) ProbeableTypes() []string { return (&kachopg.ExistenceProbe{}).ProbeableTypes() }

// probeAuthzObserve — приёмник величин кеша вердиктов для проб КОНСТРУКТОРА.
//
// Заглушка здесь законна: предмет этих проб — что судит конструктор дескриптора,
// а не куда уезжают величины. Настоящий приёмник, чей вызов носителем
// утверждается, стоит в пробе подъёма (`carrier_start_test.go`): там его пропажа
// красит пробу, здесь — не может по построению.
func probeAuthzObserve(func() authz.Metrics) {}
