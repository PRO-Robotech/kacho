// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// wiring_test.go — пробы, идущие ТЕМ ЖЕ ПУТЁМ, каким собирает носитель.
//
// # Зачем отдельный файл, если пробы звеньев уже есть
//
// Соседний `links_test.go` закрепляет ОТВЕТЫ звеньев: что делает решатель со
// сверкой существования, что отвечает обёртка, если её позвать. Этого мало, и
// разница выяснилась инъекцией: провязку сверки существования сняли в носителе —
// весь прогон остался зелёным. Пробы собирали звено РУКАМИ (`&existenceAwareCheck{…}`)
// либо звали обёртку НАПРЯМУЮ, поэтому утверждали про звено и молчали про то,
// НАДЕВАЕТ ли его носитель.
//
// Здесь вход — только то, что вызывает сам `Serve`: `decisionLink` и
// `serverPair`. Провязка, снятая в носителе, обязана красить пробу из этого
// файла; проба, которую можно оставить зелёной, сняв провязку, свойства не
// утверждает.
package servicehost

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	// Дескрипторы vpc линкуются РАДИ ГЛОБАЛЬНОГО РЕЕСТРА: набор пообъектных типов
	// выводится из карты прав резолвом запроса метода через реестр
	// (`catalogderive.ObjectScopedTypes`), поэтому синтетическое имя метода дало бы
	// ПУСТОЙ набор — и проба зеленела бы ровно на снятой провязке. Имя метода
	// обязано быть настоящим.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	// Дескрипторы compute — по той же причине и ради другой пробы: домен несёт
	// единственную подписку дерева, и признак «серверный стрим» снимается с её
	// настоящего дескриптора. Без линковки каталог домена пуст.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
)

// probedMethod — настоящий пообъектный МУТИРУЮЩИЙ метод.
//
// Мутация, а не чтение: пообъектное чтение звено скрывает и без всякого порта —
// по одной лишь форме вызова (`/Get` на глагольном `v_get`), поэтому проба на нём
// зеленела бы и со снятой провязкой (записано в `refusalSeenByCaller`).
const probedMethod = "/kacho.cloud.vpc.v1.NetworkService/Update"

// probedType — тип, у которого ЕСТЬ голос владельца. Форма не выписывается: её
// берут у того, кто ею отвечает.
const probedType = "vpc_network"

// probedID — идентификатор, который называет вызывающий.
const probedID = "netABCDEFGHJKMNPQR"

// carrierMap — карта прав в той форме, в какой её получает `decisionLink`.
func carrierMap() authz.RPCMap {
	return authz.RPCMap{
		probedMethod: {
			Relation: "v_update",
			Extract: authz.StaticExtractor(probedType, func(req any) (string, error) {
				id, _ := req.(string)
				return id, nil
			}),
		},
	}
}

// existenceSpec — дескриптор сервиса, который ПРИНЁС порт существования.
func existenceSpec(exists bool) servicecontract.Spec {
	s := chainSpec()
	s.Authz = servicecontract.AuthzSelf
	s.SelfCheck = denyingClient()
	s.DenyBudget = servicecontract.NotApplicable[float64]("проба меряет форму отказа, а не его темп")
	s.Existence = probeFunc(func(context.Context, string, string) (bool, error) { return exists, nil })
	return s
}

// refusalThroughTheCarrier собирает звено решения ТАК ЖЕ, как это делает `Serve`
// (через `decisionLink`), и возвращает то, что увидел вызывающий.
func refusalThroughTheCarrier(t *testing.T, spec servicecontract.Spec) error {
	t.Helper()
	intr, closeEdge, err := decisionLink(spec, carrierMap())
	if err != nil {
		t.Fatalf("звено решения о доступе не собралось: %v", err)
	}
	if closeEdge != nil {
		defer closeEdge()
	}
	_, cerr := intr.Unary()(principalCtx(), probedID,
		&grpc.UnaryServerInfo{FullMethod: probedMethod},
		func(context.Context, any) (any, error) { return "handled", nil })
	return cerr
}

// TestCarrierPutsTheExistenceProbeOnTheDecisionLink — ПРОВЯЗКА, а не ответ звена.
//
// Инъекция, ради которой проба написана: заменить `withExistenceHiding(spec, m, …)`
// на голого решателя в `decisionLink` — и вызывающий увидит `PermissionDenied`
// вместо промаха владельца. До этой пробы такая правка оставляла весь прогон
// зелёным.
func TestCarrierPutsTheExistenceProbeOnTheDecisionLink(t *testing.T) {
	form, ok := authz.OwnerNotFoundFormat(probedType)
	if !ok {
		t.Fatalf("у типа %q нет текста владельца — пробу не на чем строить", probedType)
	}
	err := refusalThroughTheCarrier(t, existenceSpec(true))

	if status.Code(err) != codes.NotFound {
		t.Fatalf("носитель собрал решателя БЕЗ сверки существования: отказ пришёл кодом %v, "+
			"и по одному лишь коду видно, что объект существует. Провязка снимается в decisionLink "+
			"(ветка источника решения) — именно её и утверждает эта проба", status.Code(err))
	}
	if want, got := fmt.Sprintf(form, probedID), status.Convert(err).Message(); got != want {
		t.Fatalf("текст отказа %q отличим от промаха владельца %q — оракул", got, want)
	}
}

// TestCarrierLetsAnAbsentObjectThroughToTheHandler — вторая половина существа на
// том же пути: объекта НЕТ → вызов доходит до обработчика, и промах отдаёт он.
//
// Без неё «всегда NotFound» было бы неотличимо от «различаем два случая».
func TestCarrierLetsAnAbsentObjectThroughToTheHandler(t *testing.T) {
	if err := refusalThroughTheCarrier(t, existenceSpec(false)); err != nil {
		t.Fatalf("вызов по отсутствующему объекту отвергнут звеном (%v) вместо пропуска к "+
			"обработчику: дословный промах владельца сказать было бы некому", err)
	}
}

// TestCarrierWithoutTheProbeAnswersDistinguishably — ИНЪЕКЦИЯ провязки: тот же
// путь, тот же дескриптор, порт не принесён. Отказ отличим.
func TestCarrierWithoutTheProbeAnswersDistinguishably(t *testing.T) {
	spec := existenceSpec(true)
	spec.Existence = nil
	if code := status.Code(refusalThroughTheCarrier(t, spec)); code != codes.PermissionDenied {
		t.Fatalf("без принесённого порта отказ пришёл кодом %v — значит форму ответа определяет "+
			"не порт, и проба выше ничего не доказывает", code)
	}
}

// TestScopedTypesAreDerivedFromTheRightsMap — набор пообъектных типов ВЫВОДИТСЯ
// из карты прав, а не берётся ниоткуда.
//
// Инъекция, ради которой проба написана: обнулить `scoped` в `withExistenceHiding`
// (пустая карта вместо вывода) — скрытие выключается для ВСЕХ типов сразу, и до
// этой пробы такая правка не краснела нигде.
//
// Утверждается ИСХОД: тип из карты попал в набор ⇒ отказ прозвучал голосом
// владельца. Пустой набор даёт `PermissionDenied` — то же, что снятая провязка,
// поэтому обе инъекции ловятся, и обе называют координату в тексте отказа.
func TestScopedTypesAreDerivedFromTheRightsMap(t *testing.T) {
	spec := existenceSpec(true)
	if err := refusalThroughTheCarrier(t, spec); status.Code(err) != codes.NotFound {
		t.Fatalf("тип %q объявлен в карте прав пообъектным, но скрытие по нему не сработало (%v): "+
			"набор типов в withExistenceHiding выведен не из карты либо пуст", probedType, status.Code(err))
	}
	// Контроль объёма: карта РЕАЛЬНО даёт непустой набор. Без него «сработало»
	// нельзя отличить от «сработало по другой причине».
	spec.Existence = probeFunc(func(_ context.Context, ot, id string) (bool, error) {
		if ot != probedType || id != probedID {
			t.Errorf("порт спрошен о (%q,%q), а карта называла (%q,%q)", ot, id, probedType, probedID)
		}
		return true, nil
	})
	_ = refusalThroughTheCarrier(t, spec)
}

// ── тот же вопрос ко ВТОРОЙ ветке источника решения ─────────────────────────

// denyingIAM — владелец модели, отвечающий отказом. Нужен затем, чтобы ветка
// `AuthzViaIAM` проверялась ТЕМ ЖЕ способом, что и `AuthzSelf`: провязка стоит в
// обеих, и снять её можно в любой.
type denyingIAM struct {
	iamv1.UnimplementedInternalIAMServiceServer
}

func (denyingIAM) Check(context.Context, *iamv1.CheckRequest) (*iamv1.CheckResponse, error) {
	return &iamv1.CheckResponse{Allowed: false}, nil
}

// startDenyingIAM поднимает владельца модели на эфемерном порту.
func startDenyingIAM(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель владельца модели: %v", err)
	}
	srv := grpc.NewServer()
	iamv1.RegisterInternalIAMServiceServer(srv, denyingIAM{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

// TestCarrierPutsTheExistenceProbeOnTheIAMEdgeToo — вторая ветка `decisionLink`.
//
// Провязка стоит в ОБЕИХ ветках источника решения, и снять её можно в любой —
// значит утверждать надо обе. Ветка ходит к настоящему владельцу модели
// (поднятому здесь же), потому что без ответа «не разрешено» до сверки
// существования дело не доходит вовсе.
func TestCarrierPutsTheExistenceProbeOnTheIAMEdgeToo(t *testing.T) {
	spec := existenceSpec(true)
	spec.Authz = servicecontract.AuthzViaIAM
	spec.SelfCheck = nil
	spec.CheckEdge = servicecontract.NewPeerEdge(startDenyingIAM(t), insecure.NewCredentials())
	spec.ClientBudget = 5 * time.Second

	form, _ := authz.OwnerNotFoundFormat(probedType)
	err := refusalThroughTheCarrier(t, spec)
	if status.Code(err) != codes.NotFound {
		t.Fatalf("на ребре к владельцу модели носитель собрал решателя БЕЗ сверки существования: "+
			"отказ пришёл кодом %v", status.Code(err))
	}
	if want, got := fmt.Sprintf(form, probedID), status.Convert(err).Message(); got != want {
		t.Fatalf("текст отказа %q отличим от промаха владельца %q — оракул", got, want)
	}
}

// ── стрим-звенья: СУЩЕСТВО, а не присутствие в слоте ────────────────────────

// runStreamChain сворачивает стрим-цепочку так же, как это делает grpc-go.
func runStreamChain(chain []grpc.StreamServerInterceptor, ss grpc.ServerStream, method string,
	h grpc.StreamHandler) error {
	info := &grpc.StreamServerInfo{FullMethod: method, IsServerStream: true}
	next := h
	for i := len(chain) - 1; i >= 0; i-- {
		link, downstream := chain[i], next
		next = func(srv any, s grpc.ServerStream) error { return link(srv, s, info, downstream) }
	}
	return next(nil, ss)
}

// fakeStream — стрим с подставным контекстом. Больше от него ничего не нужно:
// звенья читают `Context()` и передают стрим дальше.
type fakeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fakeStream) Context() context.Context { return s.ctx }

// TestAccessLogRecordsAStreamCallToo — журнал доступа пишет и стрим.
//
// Инъекция, ради которой проба написана: обнулить тело `accessLogStream` — до
// неё это не краснело нигде, при том что тот же дефект у unary ловился двумя
// пробами. Асимметрия была реальной: заявление «стрим тоже пишется» описывало
// код и не держалось ничем.
func TestAccessLogRecordsAStreamCallToo(t *testing.T) {
	log, buf := captureLogger()
	spec := chainSpec()
	spec.Logger = log
	var slot decisionSlot
	chain := streamChain(spec, &slot, probeLatency(t), nil, grpcsrv.ListenerPublic)
	chain = chain[:len(chain)-1] // без слота решения: предмет пробы — журнал

	err := runStreamChain(chain, &fakeStream{ctx: context.Background()}, panicMethod,
		func(any, grpc.ServerStream) error { return nil })
	if err != nil {
		t.Fatalf("стрим-вызов отвергнут: %v", err)
	}
	line := buf.String()
	if !strings.Contains(line, "grpc stream") || !strings.Contains(line, panicMethod) {
		t.Fatalf("стрим-вызов не оставил строки журнала — вид вызова остался ненаблюдаемым:\n%s", line)
	}
	if !strings.Contains(line, "code=OK") {
		t.Fatalf("строка журнала не называет исхода стрим-вызова:\n%s", line)
	}
}

// TestAccessLogRecordsThePanickingStreamCall — тот же исход, что у unary, и он
// тяжелейший: стрим-обработчик паникует ровно так же.
func TestAccessLogRecordsThePanickingStreamCall(t *testing.T) {
	log, buf := captureLogger()
	spec := chainSpec()
	spec.Logger = log
	var slot decisionSlot
	chain := streamChain(spec, &slot, probeLatency(t), nil, grpcsrv.ListenerPublic)
	chain = chain[:len(chain)-1]

	err := runStreamChain(chain, &fakeStream{ctx: context.Background()}, panicMethod,
		func(any, grpc.ServerStream) error { panic("разыменование nil на пути стрима") })
	if status.Code(err) != codes.Internal {
		t.Fatalf("паника в стриме не превратилась в codes.Internal (%v)", err)
	}
	if !strings.Contains(buf.String(), "grpc stream") {
		t.Fatalf("паниковавший стрим не оставил строки журнала:\n%s", buf.String())
	}
}

// TestStreamBudgetReachesTheStreamHandler — срок жизни подписки доезжает до
// СТРИМ-обработчика, и это ЕГО величина, а не граница обработки запроса.
//
// Числа выбраны так, чтобы проба различала два ответа: граница обработки — 5
// секунд, срок подписки — час. Возьми носитель унарную величину, и обработчик
// увидел бы срок в пределах пяти секунд; проба, где обе величины совпадают, эту
// подмену пропустила бы — и именно так выглядел бы возврат дефекта, ради
// которого ось заведена.
//
// Инъекция, ради которой проба написана: сделать `streamBudgetLink`
// пропускающим (`return h(srv, ss)`) — до неё это не краснело нигде.
func TestStreamBudgetReachesTheStreamHandler(t *testing.T) {
	spec := chainSpec()
	spec.HandlingBudget = 5 * time.Second
	spec.StreamBudget = servicecontract.Value(time.Hour)
	var slot decisionSlot
	chain := streamChain(spec, &slot, probeLatency(t), nil, grpcsrv.ListenerPublic)
	chain = chain[:len(chain)-1]

	var (
		dl  time.Time
		has bool
	)
	err := runStreamChain(chain, &fakeStream{ctx: context.Background()}, panicMethod,
		func(_ any, ss grpc.ServerStream) error {
			dl, has = ss.Context().Deadline()
			return nil
		})
	if err != nil {
		t.Fatalf("стрим-вызов отвергнут: %v", err)
	}
	if !has {
		t.Fatal("стрим-обработчик получил контекст БЕЗ срока — срок жизни подписки до него не доехал, " +
			"и подписка вправе держать соединение из ограниченного пула сколько угодно")
	}
	left := time.Until(dl)
	if left <= 5*time.Second {
		t.Fatalf("подписка получила срок %v — не больше границы обработки ОДИНОЧНОГО вызова (5s): "+
			"носитель взял унарную величину, и подписка рвалась бы по потолку запроса", left)
	}
	if left > time.Hour {
		t.Fatalf("срок %v превышает объявленный срок жизни подписки (1h)", left)
	}
}

// TestStreamChainWithoutTheBudgetLinkLeavesTheStreamUnbounded — ИНЪЕКЦИЯ: та же
// цепочка без звена границы. Стрим-обработчик получает контекст без срока.
func TestStreamChainWithoutTheBudgetLinkLeavesTheStreamUnbounded(t *testing.T) {
	log, _ := captureLogger()
	injected := []grpc.StreamServerInterceptor{accessLogStream(log)}
	var has bool
	err := runStreamChain(injected, &fakeStream{ctx: context.Background()}, panicMethod,
		func(_ any, ss grpc.ServerStream) error {
			_, has = ss.Context().Deadline()
			return nil
		})
	if err != nil {
		t.Fatalf("стрим-вызов отвергнут: %v", err)
	}
	if has {
		t.Fatal("срок появился без звена границы — значит его ставит кто-то другой, " +
			"и проба-близнец ничего не доказывает")
	}
}

// TestStreamBudgetNeverWidensTheCallersDeadline — вторая половина существа: более
// строгий срок вызывающего уважается и на стриме.
func TestStreamBudgetNeverWidensTheCallersDeadline(t *testing.T) {
	spec := chainSpec()
	spec.StreamBudget = servicecontract.Value(time.Hour)
	var slot decisionSlot
	chain := streamChain(spec, &slot, probeLatency(t), nil, grpcsrv.ListenerPublic)
	chain = chain[:len(chain)-1]

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var dl time.Time
	err := runStreamChain(chain, &fakeStream{ctx: ctx}, panicMethod,
		func(_ any, ss grpc.ServerStream) error {
			dl, _ = ss.Context().Deadline()
			return nil
		})
	if err != nil {
		t.Fatalf("стрим-вызов отвергнут: %v", err)
	}
	if time.Until(dl) > time.Second {
		t.Fatalf("окно расширено до нашего бюджета: осталось %v при просьбе клиента в 50мс", time.Until(dl))
	}
}

// ── оба слушателя: наблюдаемая половина ─────────────────────────────────────

// demoServiceDesc — синтетическая служба с одним unary-методом.
//
// `HandlerType` нулевой намеренно: регистратор сверяет реализацию с типом
// ТОЛЬКО когда реализация непуста, а нам нужен лишь обработчик, до которого
// цепочка либо доходит, либо нет.
var demoServiceDesc = grpc.ServiceDesc{
	ServiceName: "kacho.cloud.demo.v1.WidgetService",
	Methods: []grpc.MethodDesc{{
		MethodName: "Get",
		Handler: func(_ any, ctx context.Context, _ func(any) error, chain grpc.UnaryServerInterceptor) (any, error) {
			h := func(context.Context, any) (any, error) { return &iamv1.CheckResponse{Allowed: true}, nil }
			if chain == nil {
				return h(ctx, nil)
			}
			return chain(ctx, nil, &grpc.UnaryServerInfo{
				FullMethod: "/kacho.cloud.demo.v1.WidgetService/Get",
			}, h)
		},
	}},
	Metadata: "demo",
}

// callOverTheWire поднимает сервер на эфемерном порту, зовёт его метод и
// возвращает увиденный вызывающим код.
func callOverTheWire(t *testing.T, srv *grpc.Server) codes.Code {
	t.Helper()
	srv.RegisterService(&demoServiceDesc, nil)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("клиент: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out iamv1.CheckResponse
	cerr := conn.Invoke(ctx, "/kacho.cloud.demo.v1.WidgetService/Get", &iamv1.CheckRequest{}, &out)
	return status.Code(cerr)
}

// TestBothListenersRefuseIdenticallyOnTheWire — оба слушателя судятся ОДНОЙ
// цепочкой, и это утверждается НАБЛЮДАЕМО.
//
// Прежняя редакция пробы сравнивала `unaryChain(spec,&slot)` с ним же —
// доказывала детерминизм строителя и молчала о том, что подаёт слушателям
// `Serve`: правка, освобождающая внутренний слушатель от звена, оставляла её
// зелёной. Здесь поднимаются ОБА сервера, собранных `serverPair` (той же
// функцией, что зовёт `Serve`), и сверяется то, что видит вызывающий.
//
// Наблюдаемая примета выбрана та, которой цепочка обладает целиком: пустой слот
// решения о доступе отказывает `Unavailable`. Слушатель, оставшийся без звена,
// пропустил бы вызов к обработчику и ответил бы `OK` — то есть разошёлся бы с
// соседом ровно тем, что «internal = доверенный».
func TestBothListenersRefuseIdenticallyOnTheWire(t *testing.T) {
	spec := chainSpec()
	spec.PublicCreds = insecure.NewCredentials()
	spec.InternalCreds = insecure.NewCredentials()
	spec.Metrics = prometheus.NewRegistry()
	var slot decisionSlot
	public, internal, perr := serverPair(spec, &slot)
	if perr != nil {
		t.Fatalf("пара слушателей: %v", perr)
	}

	pub, intl := callOverTheWire(t, public), callOverTheWire(t, internal)
	if pub != intl {
		t.Fatalf("слушатели ответили по-разному: публичный %v, внутренний %v — «internal = доверенный» "+
			"есть запрещённое допущение, и цепочка у них обязана быть одна", pub, intl)
	}
	if pub != codes.Unavailable {
		t.Fatalf("оба слушателя ответили %v: пустой слот решения о доступе обязан отказывать, "+
			"а проба обязана мерить исход всей цепочки, а не одно звено", pub)
	}
}

// TestBothListenersReachTheHandlerOnceTheDecisionLinkIsInstalled — законный
// близнец: с установленным звеном оба слушателя доводят вызов до обработчика.
//
// Без него отрицание выше зеленело бы на всём сломанном: «оба отказали» верно и
// тогда, когда не работает ничего.
func TestBothListenersReachTheHandlerOnceTheDecisionLinkIsInstalled(t *testing.T) {
	spec := chainSpec()
	spec.PublicCreds = insecure.NewCredentials()
	spec.InternalCreds = insecure.NewCredentials()
	spec.Metrics = prometheus.NewRegistry()
	var slot decisionSlot
	slot.install(authz.NewInterceptor(authz.InterceptorOptions{
		ServiceName: "kacho-demo",
		Cache:       authz.NewCache(0),
		Logger:      spec.Logger,
		Client:      denyingClient(),
		Map: authz.RPCMap{
			"/kacho.cloud.demo.v1.WidgetService/Get": {Public: true},
		},
	}))
	public, internal, perr := serverPair(spec, &slot)
	if perr != nil {
		t.Fatalf("пара слушателей: %v", perr)
	}

	pub, intl := callOverTheWire(t, public), callOverTheWire(t, internal)
	if pub != codes.OK || intl != codes.OK {
		t.Fatalf("с установленным звеном вызов не дошёл до обработчика: публичный %v, внутренний %v — "+
			"значит отрицание выше зеленеет на всём сломанном", pub, intl)
	}
}

// закрепляют, что отказ верно читает уже готовый признак, и молчат о том, откуда
// он берётся. Инъекция, ради которой проба писалась: заменить
// `md.IsStreamingServer()` на `false` — весь прогон оставался зелёным, а отказ
// становился недостижимым на реальном пути.
//
// ПРЕДМЕТ ПРОБЫ ИЗМЕНИЛСЯ ВМЕСТЕ С ДЕРЕВОМ. Прежде здесь брался настоящий домен
// с настоящей подпиской — единственный серверный стрим дерева, поток журнала
// изменений compute, — и проверялись обе стороны: у подписки признак поднят, у
// соседнего одиночного метода того же домена нет.
//
// Стрим снят за отсутствием потребителя, и серверных стримов в дереве не осталось
// НИ ОДНОГО — ни в одном из восьми доменов. Это ровно то, что объявляют конвенции
// продукта («Watch RPC не существует»), и теперь дерево им соответствует целиком.
//
// Поэтому проба перевёрнута и утверждает СВОЙСТВО ДЕРЕВА: стримов нет. Она и есть
// страж возврата — появившийся стрим её уронит, и вместе с ним вернётся прежняя
// двусторонняя проверка признака на настоящем дескрипторе.
//
// Граница названа честно: о том, ЧИТАЕТСЯ ли признак с дескриптора, эта проба
// теперь не утверждает ничего — положительной стороны в дереве нет. Инъекция
// `IsStreamingServer() → false` сегодня не краснит ни одну пробу дерева, и это
// цена, а не недосмотр: восстановить наблюдение можно только вернув стрим.
func TestNoDomainServesServerStreams(t *testing.T) {
	// ДВЕ ГРАНИЦЫ, обе названы вслух, потому что обе делают пробу уже, чем её имя.
	//
	// Первая: обходчик аннотаций требует ЯВНЫХ имён доменов (на пустом перечне не
	// обходит ничего), а вывести их изнутри этого пакета неоткуда — каталог прав
	// живёт у края. Рукописный перечень умеет расходиться с деревом молча и уже
	// разошёлся: первая редакция назвала `kacho.cloud.nlb.v1`, а домен зовётся
	// `kacho.cloud.loadbalancer.v1`.
	//
	// Вторая: обход видит только дескрипторы, СЛИНКОВАННЫЕ в этот бинарь, а
	// тестовый бинарь пакета тянет не все стабы. Поэтому домен, чей каталог пуст,
	// считается НЕ ОСМОТРЕННЫМ, а не «без стримов»: перепись печатает оба списка
	// раздельно, и «ноль находок» остаётся отличимо от «ноль прочитанного».
	candidates := []string{
		"kaname.cloud.iam.v1", "kacho.cloud.vpc.v1", "kacho.cloud.compute.v1",
		"kacho.cloud.loadbalancer.v1", "kacho.cloud.geo.v1",
		"kacho.cloud.storage.v1", "kacho.cloud.registry.v1",
	}
	var seen, unlinked []string
	for _, d := range candidates {
		if len(catalogOf([]string{d}).rows) == 0 {
			unlinked = append(unlinked, d)
			continue
		}
		seen = append(seen, d)
	}
	if len(seen) == 0 {
		t.Fatal("ни один домен не слинкован в тестовый бинарь — проба ничего не " +
			"осмотрела, и её молчание неотличимо от исправности")
	}
	cat := catalogOf(seen)
	if len(cat.rows) == 0 {
		t.Fatal("каталог пуст — проба ничего не осмотрела, и её молчание " +
			"неотличимо от исправности")
	}
	total, streams := 0, []string{}
	for m, row := range cat.rows {
		total++
		if row.ServerStreaming {
			streams = append(streams, string(m))
		}
	}
	sort.Strings(streams)
	if len(streams) != 0 {
		t.Errorf("серверные стримы в дереве: %v.\nКонвенции продукта объявляют, что "+
			"Watch RPC не существует, и дерево этому соответствовало. Появившийся стрим "+
			"обязан принести с собой: срок жизни подписки величиной у носителя (сейчас "+
			"эта ось изъята), сужение по правам на каждую отдаваемую строку и "+
			"двустороннюю пробу признака на настоящем дескрипторе", streams)
	}
	t.Logf("перепись: осмотрено доменов %d %v, методов %d, серверных стримов среди них %d; "+
		"не слинковано в бинарь %d %v", len(seen), seen, total, len(streams), len(unlinked), unlinked)
}
