// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// describe_test.go — дескриптор процесса собирается ЗДЕСЬ, и здесь же он обязан
// проходить конструктор.
//
// # Предмет, и он найден дорогой ценой
//
// Носитель сделал три поля дескриптора обязательными, а этот композиционный
// корень их не объявил. Сборка молчала (отказ рантаймовый), прогон носителя
// молчал (корень сервиса не входил в его область), прогон самого geo молчал
// тоже — `describe()` не звал НИ ОДИН тест. Итог: ветка ломала подъём
// единственного сервиса, уже стоящего на носителе, и ни одна проверка не могла
// об этом сказать.
//
// Отсюда правило, которое эти пробы исполняют: у дескриптора обязан быть
// потребитель в прогоне — тот же конструктор, который решает его судьбу на
// старте, и на том же конфиге, с каким процесс поднимается. Свойство «каждый
// композиционный корень проходит конструктор» держится не этим файлом, а
// гейтом по дереву (`internal/repohygiene`, TestEveryDescriptorHasAProbe): один
// файл закрывает один сервис, гейт — тех, кого ещё не написали.

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
)

// bootConfig — конфигурация, загруженная ТЕМ ЖЕ вызовом, что и на старте
// (`config.Load` из переменных окружения).
//
// Литерал `config.Config{…}` здесь был бы ДРУГОЙ величиной: он обошёл бы
// умолчания, а половина полей дескриптора приезжает именно из них — и проба
// утверждала бы про конфигурацию, которой не бывает ни на одной посадке.
func bootConfig(t *testing.T, env map[string]string) config.Config {
	t.Helper()
	base := map[string]string{
		"KACHO_GEO_DB_PASSWORD":                  "secret",
		"KACHO_GEO_AUTHZ_IAM_GRPC_ADDR":          "kaname-internal:9091",
		"KACHO_GEO_AUTHZ_TRUSTED_FORWARDER_SANS": "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway",
		"KACHO_GEO_AUTH_MODE":                    "dev",
	}
	for k, v := range env {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatalf("конфигурация не загрузилась: %v", err)
	}
	return c
}

// TestDescribeIsAcceptedByTheConstructor — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ, и он первым:
// пока он красный, процесс не поднимается вовсе, а всякое отрицание ниже
// зеленеет по чужой причине.
func TestDescribeIsAcceptedByTheConstructor(t *testing.T) {
	desc, err := describe(bootConfig(t, nil), slog.New(slog.NewTextHandler(&strings.Builder{}, nil)), probeAuthzObserve, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор geo отвергнут конструктором — процесс НЕ ПОДНИМЕТСЯ:\n%v", err)
	}
	if !desc.Accepted() {
		t.Fatal("дескриптор собран литералом в обход конструктора: носитель откажется по нему поднимать процесс")
	}
	// Перепись: три оси, ставшие обязательными последними, объявлены ЯВНО, а не
	// проскочили нулевым значением. Утверждается не «принят», а «принят по
	// названным причинам» — иначе проба зеленела бы и на дескрипторе, у которого
	// эти оси отвалились вместе с их отказами.
	s := desc.Spec()
	if s.HandlingBudget <= 0 {
		t.Fatalf("верхняя граница обработки не объявлена (%v)", s.HandlingBudget)
	}
	if budget, ok := s.DenyBudget.Get(); !ok || budget <= 0 {
		t.Fatalf("бюджет отказов объявлен изъятием либо нулём (%v, ok=%v), а решение о доступе geo "+
			"принимает вопросом к соседу — шторм отказов есть кому ронять", budget, ok)
	}
	if !s.BootGate.Declared() {
		t.Fatal("ось загрузочного гейта не объявлена вовсе")
	}
	t.Logf("дескриптор принят: граница обработки %v, бюджет отказов объявлен, гейт мутаций объявлен", s.HandlingBudget)
}

// TestDescribeProbeCanFail — контроль того, что проба выше СПОСОБНА упасть.
//
// Без него «дескриптор принят» неотличимо от «конструктор ничего не проверяет»:
// положительное утверждение, у которого нет отрицательного близнеца, зеленеет
// одинаково на исправном и на выключенном.
func TestDescribeProbeCanFail(t *testing.T) {
	// Ребро решения о доступе не названо — отказ О6.
	cfg := bootConfig(t, map[string]string{"KACHO_GEO_AUTHZ_IAM_GRPC_ADDR": ""})
	_, err := describe(cfg, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)), probeAuthzObserve, prometheus.NewRegistry())
	if err == nil {
		t.Fatal("дескриптор без ребра решения о доступе принят — конструктор не судит ничего, " +
			"и положительная проба выше вакуумна")
	}
	if !strings.Contains(err.Error(), "CheckEdge") {
		t.Fatalf("отказ не называет предмета — чинить по нему нечего:\n%v", err)
	}
}

// TestGeoServesNoGatedMutation — САМОИСТЕЧЕНИЕ изъятия по загрузочному гейту.
//
// Дескриптор объявляет гейт мутаций неприменимым, и вторая половина причины —
// «отвергать нечего»: все мутации geo живут на `Internal*`-службах, которые под
// гейт не подпадают. Утверждение проверяемое, и проверяется оно ТЕМ ЖЕ
// предикатом, которым гейт исполняется (`servicehost.IsGatedMutation`), а не его
// копией: копия разошлась бы с оригиналом молча и ровно там, где расхождение
// незаметно.
//
// Появится у geo тенантское `Create` — проба покраснеет и назовёт метод. Это и
// есть предикат снятия изъятия, и он внешний: состояние дерева, а не память
// автора.
func TestGeoServesNoGatedMutation(t *testing.T) {
	regionUC := region.New(nil, nil, nil, nil)
	zoneUC := zone.New(nil, nil, nil, nil)
	opHandler := operationspb.NewHandler(operations.NewRepo(nil, "kacho_geo"))

	var served []string
	for _, reg := range []func(grpc.ServiceRegistrar){
		func(r grpc.ServiceRegistrar) { registerPublic(r, regionUC, zoneUC, opHandler) },
		func(r grpc.ServiceRegistrar) { registerInternal(r, regionUC, zoneUC, opHandler) },
	} {
		srv := grpc.NewServer()
		reg(srv)
		for name, info := range srv.GetServiceInfo() {
			for _, m := range info.Methods {
				served = append(served, "/"+name+"/"+m.Name)
			}
		}
	}
	if len(served) == 0 {
		t.Fatal("ни один метод не зарегистрирован — «гейтируемых мутаций нет» было бы верно " +
			"и на пустом наборе, то есть проба не отличала бы исправное от сломанного")
	}
	var gated []string
	for _, m := range served {
		if servicehost.IsGatedMutation(m) {
			gated = append(gated, m)
		}
	}
	if len(gated) != 0 {
		t.Fatalf("geo служит гейтируемую мутацию: %v.\nИзъятие BootGate в describe() пережило свой "+
			"предмет: оно обосновано тем, что отвергать нечего. Принесите гейт либо перепишите причину",
			gated)
	}
	t.Logf("осмотрено служимых методов: %d, гейтируемых мутаций среди них: 0", len(served))
}

// TestGeoServesNoServerStream — САМОИСТЕЧЕНИЕ изъятия по сроку жизни подписки.
//
// Дескриптор объявляет ось `StreamBudget` неприменимой с причиной «серверных
// стримов geo не служит». Утверждение проверяемое, и проверяется оно СОСТАВОМ
// ЗАРЕГИСТРИРОВАННОГО, а не памятью автора: появится у geo первая подписка —
// проба покраснеет и назовёт метод.
//
// Источник признака назван честно: здесь читается самоописание сервера
// (`grpc.ServiceInfo`), носитель на старте читает дескриптор метода. Оба
// порождены одним `.proto`, поэтому для утверждения «стримов нет» годится любой;
// расхождение между ними было бы дефектом генерации, а не этого изъятия.
//
// Носитель отказал бы и сам (О11), но на СТАРТЕ ПРОЦЕССА — то есть при
// развёртывании. Проба переносит тот же отказ в прогон.
func TestGeoServesNoServerStream(t *testing.T) {
	regionUC := region.New(nil, nil, nil, nil)
	zoneUC := zone.New(nil, nil, nil, nil)
	opHandler := operationspb.NewHandler(operations.NewRepo(nil, "kacho_geo"))

	methods, streams := 0, []string{}
	for _, reg := range []func(grpc.ServiceRegistrar){
		func(r grpc.ServiceRegistrar) { registerPublic(r, regionUC, zoneUC, opHandler) },
		func(r grpc.ServiceRegistrar) { registerInternal(r, regionUC, zoneUC, opHandler) },
	} {
		srv := grpc.NewServer()
		reg(srv)
		for name, info := range srv.GetServiceInfo() {
			for _, m := range info.Methods {
				methods++
				if m.IsServerStream {
					streams = append(streams, "/"+name+"/"+m.Name)
				}
			}
		}
	}
	if methods == 0 {
		t.Fatal("ни один метод не зарегистрирован — «серверных стримов нет» было бы верно и на " +
			"пустом наборе, то есть проба не отличала бы исправное от сломанного")
	}
	if len(streams) != 0 {
		t.Fatalf("geo служит серверный стрим: %v.\nИзъятие StreamBudget в describe() пережило свой "+
			"предмет: объявите срок жизни подписки величиной (больше HandlingBudget) либо "+
			"перепишите причину", streams)
	}
	// Вторая сторона той же оси, и на СВОЁМ дескрипторе: величина, объявленная
	// процессом без единой подписки, — проводка без предмета. Носитель откажет в
	// этом и сам (О11), но на старте процесса; проба переносит отказ в прогон.
	desc, err := describe(bootConfig(t, nil), slog.New(slog.NewTextHandler(&strings.Builder{}, nil)), probeAuthzObserve, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	if budget, ok := desc.Spec().StreamBudget.Get(); ok && len(streams) == 0 {
		t.Fatalf("дескриптор объявляет срок жизни подписки (%v), а служимых серверных стримов "+
			"нет ни одного: величина утверждает решение про подписки там, где подписок нет", budget)
	}
	t.Logf("осмотрено служимых методов: %d, серверных стримов среди них: 0", methods)
}

// TestBootPostureStillReportsWhatTheDescriptorCarries — самоотчёт о посадке
// печатается ПОСЛЕ приёма дескриптора, поэтому обе половины обязаны сходиться на
// одном конфиге. Проба тонкая намеренно: посадку судит `bootposture_test.go`,
// здесь утверждается лишь то, что обе читают одно и то же и не расходятся.
func TestBootPostureStillReportsWhatTheDescriptorCarries(t *testing.T) {
	cfg := bootConfig(t, nil)
	desc, err := describe(cfg, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)), probeAuthzObserve, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	posture := bootPosture(cfg)
	if got, want := posture.AuthMode, desc.Spec().Mode.String(); got != want {
		t.Fatalf("самоотчёт называет режим %q, дескриптор — %q: два места об одном предмете, "+
			"и гейт посадки читал бы не то, по чему процесс поднялся", got, want)
	}
	var _ = observability.LogBootPosture
}

// probeAuthzObserve — приёмник величин кеша вердиктов для проб КОНСТРУКТОРА.
//
// Заглушка здесь законна: предмет этих проб — что судит конструктор дескриптора,
// а не куда уезжают величины. Настоящий приёмник, чей вызов носителем
// утверждается, стоит в пробе подъёма (`carrier_start_test.go`): там его пропажа
// красит пробу, здесь — не может по построению.
func probeAuthzObserve(func() authz.Metrics) {}
