// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"strings"
	"testing"

	"google.golang.org/grpc"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
	"github.com/PRO-Robotech/kacho/services/registry/internal/handler"
)

// subscribeVerb — полное имя ОБЩЕГО глагола подписки.
//
// Он записан здесь строкой, а не выведен из дескриптора служб: предмет пробы —
// что служится ИМЕННО он, и вывод имени из того же источника, что и регистрация,
// сделал бы утверждение тождественно истинным при любой ошибке.
const subscribeVerb = "/kacho.cloud.subscription.InternalSubscriptionService/Subscribe"

// TestRegistryStreamBudgetIsDeclaredWithItsSubject — ось объявлена ВЕЛИЧИНОЙ, и
// величина переживает границу обработки одиночного вызова.
//
// Прежде ось была объявлена ИЗЪЯТИЕМ («серверных стримов registry не служит»), и
// это было верно ровно до провязки общего сервера подписки. Изъятие — заявление
// о дереве; появившийся стрим делает его ложью, а срок жизни потока при этом не
// назван никем. Само изъятие обещало ровно это: «появится первая подписка —
// носитель уронит старт поимённо по её методу, а проба назовёт её раньше».
func TestRegistryStreamBudgetIsDeclaredWithItsSubject(t *testing.T) {
	cfg := bootConfig(t, nil)
	desc, err := describe(cfg, probeMode(t, cfg), probeLogger(), probePorts())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	spec := desc.Spec()
	if !spec.StreamBudget.Declared() {
		t.Fatal("ось срока жизни стрима НЕ объявлена: носитель роняет старт на необъявленной оси")
	}
	if why, waived := spec.StreamBudget.NotApplicableBecause(); waived {
		t.Fatalf("ось объявлена ИЗЪЯТИЕМ (%q), а сервис служит поток подписки: "+
			"изъятие стало ложью о дереве — величина не названа никем", why)
	}
	budget, ok := spec.StreamBudget.Get()
	if !ok {
		t.Fatal("ось объявлена, но величины не несёт")
	}
	if handling := spec.HandlingBudget; budget <= handling {
		t.Fatalf("срок жизни потока %v не превосходит границы обработки %v: поток "+
			"закрывался бы раньше первого события догона, и подписчик читал бы "+
			"штатный обрыв как «изменений нет»", budget, handling)
	}
	t.Logf("срок жизни потока %v, граница обработки одиночного вызова %v", budget, spec.HandlingBudget)
}

// TestRegistryServesTheSubscriptionStreamOnTheInternalListenerOnly — поток
// подписки служится, служится РОВНО ОДИН, и только на внутреннем слушателе.
//
// Утверждается тройка, и каждая часть закрывает свой промах: стрим ЕСТЬ (иначе
// объявленная величина оси ни к чему не относится); он ОДИН и это общий глагол
// (второй означал бы второй язык подписки); он НЕ на публичном слушателе
// (`Internal.*` на внешнюю поверхность не выходит, и «internal = доверенный»
// здесь не допущение — слушатели обходятся порознь).
func TestRegistryServesTheSubscriptionStreamOnTheInternalListenerOnly(t *testing.T) {
	registryHandler := handler.NewRegistryHandler(nil, nil, 0)
	internalHandler := handler.NewInternalRegistryHandler(nil)
	// Обработчик операций берётся из общего фундамента: посервисные копии сведены
	// в одно место (#1434), и своя у registry больше не заводится.
	opHandler := operationspb.NewHandler(operations.NewRepo(nil, "kacho_registry"))
	// Сервер подписки — ЗАГЛУШКА: предмет пробы состав служимого набора, а
	// настоящий потребовал бы базы, сужателя и объявления посадки. Что провязан
	// настоящий, утверждает интеграционная проба журнала.
	var sub subscriptionv1.InternalSubscriptionServiceServer = stubSubscriptionServer{}

	byListener := map[string]func(grpc.ServiceRegistrar){
		"public": func(reg grpc.ServiceRegistrar) {
			registerPublic(reg, registryHandler, handler.NewQuotaHandler(nil), opHandler)
		},
		"internal": func(reg grpc.ServiceRegistrar) {
			registerInternal(reg, internalHandler, opHandler, sub)
		},
	}

	seen := map[string][]string{}
	methods := 0
	for listener, reg := range byListener {
		srv := grpc.NewServer()
		reg(srv)
		for name, info := range srv.GetServiceInfo() {
			for _, m := range info.Methods {
				methods++
				if m.IsServerStream {
					seen[listener] = append(seen[listener], "/"+name+"/"+m.Name)
				}
			}
		}
	}
	if methods == 0 {
		t.Fatal("ни один метод не зарегистрирован — утверждение о составе стримов " +
			"было бы вакуумным на пустом наборе")
	}
	if got := seen["public"]; len(got) != 0 {
		t.Fatalf("на ПУБЛИЧНОМ слушателе служатся стримы %v: поток подписки — "+
			"Internal-глагол, на внешнюю поверхность он не выходит", got)
	}
	internal := seen["internal"]
	if len(internal) != 1 || internal[0] != subscribeVerb {
		t.Fatalf("на внутреннем слушателе служимые стримы %v, ожидался ровно один — %s.\n"+
			"Ноль означает, что общий сервер потока не провязан и объявленная величина "+
			"срока жизни ни к чему не относится; больше одного — второй язык подписки.",
			internal, subscribeVerb)
	}
	t.Logf("осмотрено служимых методов: %d, серверных стримов: публичный %d, внутренний %d",
		methods, len(seen["public"]), len(internal))
}

// TestSubscriptionIsNotServedWithoutANarrower — без сужателя поток НЕ поднимается
// вовсе, и это fail-closed, а не пропуск.
//
// За глаголом подписки нет пообъектной проверки на крае (он сужаемый), поэтому
// откатываться не на что: сервер без сужателя отдал бы ВЕСЬ журнал — все реестры
// всех арендаторов — под кодом, который выглядит фильтрующим. Аварийный режим
// (сужателя нет вовсе) обязан оставлять поток НЕ поднятым, а не поднятым и
// открытым.
func TestSubscriptionIsNotServedWithoutANarrower(t *testing.T) {
	cfg := bootConfig(t, nil)
	srv, err := buildSubscriptionServer(cfg, nil, probeLogger())
	if err != nil {
		t.Fatalf("отсутствие сужателя отдано ОТКАЗОМ подъёма (%v): аварийный режим не поднимает "+
			"поток и не обязан ронять процесс", err)
	}
	if srv != nil {
		t.Fatal("сервер потока собран БЕЗ сужателя: он отдал бы весь журнал целиком, " +
			"а код при этом выглядит фильтрующим")
	}
}

// TestSubscriptionRefusesAnUnnamedEndpointBase — величина посадки, которой никто
// не назвал, отвергается ПОДЪЁМОМ, а не обнаруживается первым событием.
//
// Основа адреса производит output-only поле `endpoint` состояния события. Общая
// форма разрешает подписчику читать непустую нагрузку как ПОЛНОЕ состояние
// предмета, поэтому пустая основа означала бы не «поле не заполнено», а «адрес у
// реестра такой» — утверждение, которого владелец не делал, и записал бы его
// подписчик как факт.
func TestSubscriptionRefusesAnUnnamedEndpointBase(t *testing.T) {
	cfg := bootConfig(t, map[string]string{"KACHO_REGISTRY_ENDPOINT_BASE": " "})
	cfg.EndpointBase = ""
	_, err := buildSubscriptionServer(cfg, probeNarrower(t), probeLogger())
	if err == nil {
		t.Fatal("подъём с ПУСТОЙ основой адреса прошёл: каждое событие несло бы адрес " +
			"вида «/reg-…», и подписчик записал бы его как факт")
	}
	if !strings.Contains(err.Error(), "endpoint") && !strings.Contains(err.Error(), "адрес") {
		t.Fatalf("отказ не называет предмет (%v): оператор не узнает, какую ручку задать", err)
	}
	t.Logf("отказ подъёма: %v", err)
}

// probeNarrower — сужатель, который СУЖАЕТ.
//
// Настоящий здесь не нужен и вреден: он потребовал бы соединения к владельцу
// модели прав, а предмет проб — величины посадки и состав служимого набора.
// Важно единственное свойство — что объект отвечает «я сужаю»: сервер потока
// отвергает подъём с подвешенным и не сужающим сужателем, и проба обязана
// проходить именно этот суд, а не обходить его.
func probeNarrower(t *testing.T) *listnarrow.Narrower {
	t.Helper()
	n := narrowtest.AllowingAll()
	if !n.Narrows() {
		t.Fatal("подставной сужатель не сужает — проба обошла бы тот самый суд, который проверяет")
	}
	return n
}

// stubSubscriptionServer — заглушка сервера потока для проб СОСТАВА.
//
// Она не отвечает ни на один вызов и не обязана: предмет проб — что глагол
// зарегистрирован и на каком слушателе, а не что он отдаёт. Поведение стережёт
// интеграционная проба журнала, где сервер настоящий и база настоящая.
type stubSubscriptionServer struct {
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer
}

// TestSubscriptionDoesNotDialWithAPoolOnlyDSN — поток берёт строку подключения
// ОДИНОЧНОГО соединения, а не пуловую.
//
// # Почему это отдельная проба, а не подробность
//
// `Config.DSN()` дописывает `pool_max_conns`, и её собственный комментарий
// говорит, чем это кончается вне пула: неизвестный PG-параметр и FATAL при
// подключении. Разбор строки такую ошибку НЕ ловит — ключ уезжает серверу
// runtime-параметром, — значит отказ наступает на ПОДКЛЮЧЕНИИ. У долгоживущего
// потока это самый тихий вид поломки: процесс поднялся, глагол выставлен, а
// КАЖДАЯ подписка отвечает «источник недоступен» и никогда ничем иным; ни один
// страж старта такого не назовёт.
//
// Положительный контроль внутри пробы обязателен: без него она зеленела бы и на
// посадке, где пуловый параметр не появляется вовсе, — то есть утверждала бы про
// строку, которой не бывает.
func TestSubscriptionDoesNotDialWithAPoolOnlyDSN(t *testing.T) {
	cfg := bootConfig(t, map[string]string{"KACHO_REGISTRY_DB_MAX_CONNS": "8"})

	// Положительный контроль: пуловая строка параметр НЕСЁТ — предмет пробы существует.
	if !strings.Contains(cfg.DSN(), "pool_max_conns") {
		t.Fatal("пуловая строка не несёт pool_max_conns на посадке с заданным пределом пула: " +
			"предмета у пробы нет, и её молчание ничего не значит")
	}
	if got := cfg.SingleConnDSN(); strings.Contains(got, "pool_") {
		t.Fatalf("строка одиночного соединения несёт параметр пула: %q", got)
	}
	// И то же самое проверяет СТРАЖ ПОДЪЁМА — на той функции, которая строит сервер.
	if _, err := buildSubscriptionServer(cfg, probeNarrower(t), probeLogger()); err != nil {
		t.Fatalf("подъём отвергнут на посадке с заданным пределом пула: %v", err)
	}
	// Путь поиска и режим шифрования при этом СОХРАНЕНЫ: одиночное соединение
	// обязано видеть схему сервиса так же, как её видит пул.
	if got := cfg.SingleConnDSN(); !strings.Contains(got, "search_path") {
		t.Fatalf("строка одиночного соединения потеряла путь поиска: %q — журнал лежит в схеме сервиса", got)
	}
	if got := cfg.SingleConnDSN(); !strings.Contains(got, "sslmode") {
		t.Fatalf("строка одиночного соединения потеряла режим шифрования: %q", got)
	}
}
