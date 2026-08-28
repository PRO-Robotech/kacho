// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

// subscribeVerb — полное имя ОБЩЕГО глагола подписки.
//
// Он записан здесь строкой, а не выведен из дескриптора служб: предмет пробы —
// что служится ИМЕННО он, и вывод имени из того же источника, что и регистрация,
// сделал бы утверждение тождественно истинным при любой ошибке.
const subscribeVerb = "/kacho.cloud.subscription.InternalSubscriptionService/Subscribe"

// TestNlbStreamBudgetIsDeclaredWithItsSubject — ось объявлена ВЕЛИЧИНОЙ, и
// величина переживает границу обработки одиночного вызова.
//
// Прежде ось была объявлена ИЗЪЯТИЕМ («серверных стримов у сервиса нет»), и это
// было верно ровно до провязки общего сервера подписки. Изъятие —
// заявление о дереве; появившийся стрим делает его ложью, а срок жизни потока
// при этом не назван никем.
func TestNlbStreamBudgetIsDeclaredWithItsSubject(t *testing.T) {
	desc, err := describe(bootConfig(t, nil), quietLogger(), probeNarrower(), probeGate(),
		probeExistence{}, probeAuthzObserve, prometheus.NewRegistry())
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

// TestNlbServesTheSubscriptionStreamOnTheInternalListenerOnly — поток подписки
// служится, служится РОВНО ОДИН, и только на внутреннем слушателе.
//
// Утверждается тройка, и каждая часть закрывает свой промах: стрим ЕСТЬ (иначе
// объявленная величина оси ни к чему не относится); он ОДИН и это общий глагол
// (второй означал бы второй язык подписки — ровно то, ради запрета чего заведён
// эпик); он НЕ на публичном слушателе (`Internal.*` наружу не выходит, запретом на публикацию внутренних служб наружу, и
// «internal = доверенный» здесь не допущение — слушатели обходятся порознь).
func TestNlbServesTheSubscriptionStreamOnTheInternalListenerOnly(t *testing.T) {
	w := grpcWiring{
		cfg:     bootConfig(t, nil),
		logger:  quietLogger(),
		peers:   &peerClients{},
		repo:    kachopg.New(nil, nil),
		opsRepo: operations.NewRepo(nil, "kacho_nlb"),
		// Сервер подписки — ЗАГЛУШКА: предмет пробы состав служимого набора, а
		// настоящий потребовал бы базы, сужателя и объявления посадки. Что
		// провязан настоящий, утверждает проба подъёма носителя.
		subscription: stubSubscriptionServer{},
	}

	byListener := map[string]func(grpc.ServiceRegistrar){
		"public":   func(reg grpc.ServiceRegistrar) { registerPublic(reg, w) },
		"internal": func(reg grpc.ServiceRegistrar) { registerInternal(reg, w) },
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
			"Internal-глагол, на внешнюю поверхность он не выходит (запретом на публикацию внутренних служб наружу)", got)
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

// stubSubscriptionServer — заглушка сервера потока для проб СОСТАВА.
//
// Она не отвечает ни на один вызов и не обязана: предмет проб — что глагол
// зарегистрирован и на каком слушателе, а не что он отдаёт. Поведение стережёт
// интеграционная проба журнала, где сервер настоящий и база настоящая.
type stubSubscriptionServer struct {
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer
}
