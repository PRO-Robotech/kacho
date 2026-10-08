// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// route_wiring_parity_test.go — «в списке» и «маршрутизируется» это два разных
// факта, и до этого файла проверялся только первый.
//
// ЧТО УЖЕ БЫЛО ЗАКРЫТО. Состав `allowlist.AllowedMethods` вычисляется из
// дескрипторов (allowlist/parity_test.go): забытый публичный RPC краснеет там.
//
// ЧЕГО ЭТО НЕ ДОКАЗЫВАЕТ. Резолвер (proxy/server.go) отвечает «маршрута нет» по
// ЧЕТЫРЁМ разным причинам, и членство в списке — только одна из них. Последняя
// причина — в карте открытых соединений нет ключа домена. Карту строит
// композиционный корень (`dialBackends` → `config.BackendAddrs`) из РУКОПИСНОГО
// набора ключей, и список методов с этим набором ничем не связан. Домен, чей
// ключ отсутствует, целиком выпадает из нативного gRPC — и вызывающий получает
// ровно тот отказ, которым укрыта административная поверхность. То есть тот же
// класс, ради которого писался вычисляемый список, живёт этажом ниже.
//
// ПЕРЕПИСЬ, КОТОРАЯ ЭТО УСТАНОВИЛА (ревизия b49f5794, предикат — весь
// `go test ./gateway/...`, единица — домен): из восьми маршрутизируемых доменов
// удаление ключа у ДВУХ не замечает ни один тест дерева — прогон остаётся
// rc=0. Ещё у четырёх падение случайное: краснеют пробы про ВЫБОР
// mTLS-удостоверений и про «ключ непуст», то есть про соседнее свойство; ни одна
// проба дерева не утверждала, что публичные методы домена маршрутизируются.
//
// ПОЧЕМУ ГЕЙТ ЗДЕСЬ, А НЕ В internal/proxy. Пакет `main` — единственное место,
// откуда вызывается НАСТОЯЩИЙ производитель карты (`dialBackends`). Гейт,
// собравший карту рядом своими руками, измерял бы собственную копию — ровно ту
// ошибку, которую ловит.
package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// Полы переписи. Они не «про размер контракта» — они про то, что гейт вообще
// что-то прочитал: пустой список или пустая карта дают ноль находок так же
// убедительно, как полное совпадение.
const (
	minAllowedMethods = 200
	minBackendKeys    = 10
)

// installation — какую установку объявляет проба.
//
// У notify адреса по умолчанию нет (NTF-4 Р20): пустое поле означает «notify в
// установке не объявлен», и карта соединений ключей notify тогда не несёт. Поэтому
// проба, которой нужна установка С notify, объявляет её явно — адресом и парой
// mTLS, ровно как профиль развёртывания, — а не получает её подстановкой
// умолчания. Без этой опции проба судила бы установку, которой не бывает.
type installation int

const (
	// installationWithoutNotify — адрес notify не объявлен.
	installationWithoutNotify installation = iota
	// installationWithNotify — адрес внутреннего слушателя notify-api и пара mTLS
	// края к нему объявлены.
	installationWithNotify
)

// notifyProbeAddr — адрес, которым проба объявляет notify. grpc.NewClient ленив,
// поэтому имя не разрешается и сети проба не касается.
const notifyProbeAddr = "notify-api.kacho.svc:9091"

// declareInstallation выставляет окружение установки до config.Load.
func declareInstallation(t *testing.T, inst installation) {
	t.Helper()
	switch inst {
	case installationWithNotify:
		cert, key, ca := writePEMTriple(t)
		fullCertEnv(t, cert, key, ca)
		t.Setenv("KACHO_API_GATEWAY_NOTIFY_INTERNAL_GRPC", notifyProbeAddr)
		t.Setenv("KACHO_API_GATEWAY_MTLS_NOTIFY_ENABLE", "true")
	case installationWithoutNotify:
		// Явно пусто: значение из окружения процесса, запустившего пробу, не
		// подменяет объявленную установку.
		t.Setenv("KACHO_API_GATEWAY_NOTIFY_INTERNAL_GRPC", "")
	default:
		t.Fatalf("неизвестная установка %d", inst)
	}
}

// realBackends открывает карту соединений тем же вызовом, что и composition root,
// для объявленной установки. grpc.NewClient ленив — сети здесь не касаемся.
func realBackends(t *testing.T, inst installation) (config.Config, proxy.Backends) {
	t.Helper()
	declareInstallation(t, inst)
	cfg, err := config.Load()
	require.NoError(t, err, "config.Load")
	backends, cleanup, err := dialBackends(cfg)
	require.NoError(t, err, "dialBackends — гейт не может судить о карте, которой нет")
	t.Cleanup(cleanup)
	require.GreaterOrEqual(t, len(backends), minBackendKeys,
		"в карте бэкендов %d ключей (< %d) — она пуста или собрана не тем вызовом; "+
			"пока это не починено, любой зелёный вердикт ниже беспредметен", len(backends), minBackendKeys)
	return cfg, backends
}

// TestRouteWiring_EveryAllowedMethodResolvesOnTheRealBackends — положительная
// сторона: каждый путь разрешённого списка РЕАЛЬНО резолвится на карте, которую
// строит composition root.
//
// Отсутствие ключа домена не выглядит как ошибка конфигурации ниоткуда: резолвер
// отвечает тем же самым отказом, что и за намеренно скрытую административную
// поверхность, поэтому целый домен может выпасть из нативного gRPC и не
// проявиться ни в одном наблюдаемом симптоме, кроме «RPC не работает».
func TestRouteWiring_EveryAllowedMethodResolvesOnTheRealBackends(t *testing.T) {
	_, backends := realBackends(t, installationWithNotify)
	resolve := proxy.Resolver(backends)

	require.GreaterOrEqual(t, len(allowlist.AllowedMethods), minAllowedMethods,
		"в списке %d записей (< %d) — читать нечего, вердикт беспредметен",
		len(allowlist.AllowedMethods), minAllowedMethods)

	perDomain := map[string]int{}
	unresolved := map[string][]string{}
	for m := range allowlist.AllowedMethods {
		domain, ok := proxy.RoutableDomain(m)
		require.True(t, ok,
			"путь %q не разбирается как маршрутизируемый — такой записи в списке быть не может", m)
		perDomain[domain]++
		target, conn, routed := resolve(m)
		if !routed || conn == nil || target != m {
			unresolved[domain] = append(unresolved[domain], m)
		}
	}

	domains := sortedKeys(perDomain)
	t.Logf("перепись: %d записей списка, %d маршрутизируемых доменов (%s), %d ключей в карте бэкендов",
		len(allowlist.AllowedMethods), len(domains), strings.Join(domains, " "), len(backends))

	if len(unresolved) == 0 {
		return
	}
	var report []string
	lost := 0
	for _, d := range sortedKeys(unresolved) {
		ms := unresolved[d]
		sort.Strings(ms)
		lost += len(ms)
		report = append(report, "  домен "+d+": "+itoa(len(ms))+" из "+itoa(perDomain[d])+
			" публичных RPC не маршрутизируются, например "+ms[0])
	}
	t.Errorf("%d публичных RPC в %d домен(ах) есть в разрешённом списке, но НЕ маршрутизируются: "+
		"composition root не открыл соединения для их домена. Внешнему вызывающему это "+
		"неотличимо от намеренно скрытого административного метода — оба получают один и тот же "+
		"отказ маршрутизации, поэтому недоделка живёт сколько угодно долго.\n%s",
		lost, len(unresolved), strings.Join(report, "\n"))
}

// TestRouteWiring_UnwiredDomainIsRefused — отрицание в паре с положительным.
//
// Без этой пробы зелёное выше означало бы «резолвер согласен на всё» ровно так
// же убедительно, как «всё провязано»: предикат обязан уметь ответить «нет» на
// том же самом входе, отличающемся ТОЛЬКО отсутствием ключа.
func TestRouteWiring_UnwiredDomainIsRefused(t *testing.T) {
	_, backends := realBackends(t, installationWithNotify)

	const probe = "/kacho.cloud.vpc.v1.NetworkService/Get"
	require.True(t, allowlist.IsAllowed(probe), "проба обязана быть настоящим разрешённым путём")
	domain, ok := proxy.RoutableDomain(probe)
	require.True(t, ok)

	// Законный близнец: на полной карте путь резолвится.
	if _, conn, routed := proxy.Resolver(backends)(probe); !routed || conn == nil {
		t.Fatalf("контроль сломан: %q не резолвится на полной карте — сравнивать не с чем", probe)
	}

	// Тот же путь, та же карта минус один ключ — отказ.
	stripped := proxy.Backends{}
	for k, v := range backends {
		if k != domain {
			stripped[k] = v
		}
	}
	if _, _, routed := proxy.Resolver(stripped)(probe); routed {
		t.Fatalf("без ключа %q путь %q всё равно резолвится — предикат гейта не различает "+
			"провязанный домен от непровязанного, значит его зелёное ничего не значит", domain, probe)
	}
}

// TestRouteWiring_InternalStaysUnroutedOnTheSameWiring — второй контроль: зелёное
// выше не должно достигаться тем, что резолвер маршрутизирует вообще всё.
// Административная поверхность на той же самой карте по-прежнему не резолвится.
func TestRouteWiring_InternalStaysUnroutedOnTheSameWiring(t *testing.T) {
	_, backends := realBackends(t, installationWithNotify)
	resolve := proxy.Resolver(backends)

	// Второй путь — notify: его соединение ведёт на ВНУТРЕННИЙ слушатель, где
	// InternalNoticeService зарегистрирован, поэтому отказ здесь держит только
	// резолвер, а не построение соединения (NTF-4 Р20).
	for _, adminProbe := range []string{
		"/kacho.cloud.vpc.v1.InternalAddressPoolService/List",
		"/kacho.cloud.notify.v1.InternalNoticeService/Create",
	} {
		require.True(t, proxy.IsInternalRoute(adminProbe), "проба обязана быть настоящим Internal*-путём")
		if _, _, routed := resolve(adminProbe); routed {
			t.Fatalf("Internal*-метод %q резолвится на внешней карте (запрет #6)", adminProbe)
		}
	}
}

// TestRouteWiring_NotifyKeysShareOneConnection — у notify слушатель один, и оба
// ключа карты (`notify` для резолвера по домену пакета, `notifyInternal` для
// внутреннего REST) ведут в ОДНО соединение: один адрес, одно ребро mTLS, одно
// закрытие (NTF-5 З18 п.1–п.3).
//
// Близнец — vpc: у него два слушателя, и его два ключа — два соединения. Без
// близнеца равенство указателей ниже могло бы означать, что карта вообще
// склеивает ключи домена, а не что у notify псевдоним.
func TestRouteWiring_NotifyKeysShareOneConnection(t *testing.T) {
	_, backends := realBackends(t, installationWithNotify)

	notify, ok := backends["notify"]
	require.True(t, ok, "установка с notify, а ключа \"notify\" в карте нет")
	notifyInternal, ok := backends["notifyInternal"]
	require.True(t, ok, "установка с notify, а ключа \"notifyInternal\" в карте нет")
	require.Same(t, notifyInternal, notify,
		"ключи notify и notifyInternal ведут в разные соединения — у notify слушатель один, "+
			"второе соединение было бы вторым адресом одного предмета")
	require.Equal(t, notifyProbeAddr, notify.Target(),
		"соединение notify ведёт не на объявленный адрес")

	vpc, vpcInternal := backends["vpc"], backends["vpcInternal"]
	require.NotNil(t, vpc)
	require.NotNil(t, vpcInternal)
	require.NotSame(t, vpc, vpcInternal,
		"близнец сломан: у vpc два слушателя, а ключи склеены в одно соединение")

	require.Equal(t, backendEdge("notify"), backendEdge("notifyInternal"),
		"у двух ключей notify разные рёбра mTLS")
	require.Equal(t, "notify", backendEdge("notify"))
}

// TestRouteWiring_NoNotifyAddressNoNotifyKeys — установка без notify: ни одного из
// двух ключей, и ни одна строка notify перечня не резолвится — вызывающий получает
// отказ маршрута, а не вызов по пустому адресу (NTF-5 З18 п.4, CX5-45 (г)).
//
// Положительный близнец — та же проба на установке с notify: те же строки
// резолвятся. Без него ноль резолвов ниже мог бы означать, что строк notify в
// перечне нет вовсе.
func TestRouteWiring_NoNotifyAddressNoNotifyKeys(t *testing.T) {
	var notifyRows []string
	for m := range allowlist.AllowedMethods {
		if d, ok := proxy.RoutableDomain(m); ok && d == "notify" {
			notifyRows = append(notifyRows, m)
		}
	}
	sort.Strings(notifyRows)
	require.NotEmpty(t, notifyRows, "в перечне нет ни одной строки notify — пробе нечего судить")

	t.Run("without", func(t *testing.T) {
		_, backends := realBackends(t, installationWithoutNotify)
		for _, key := range []string{"notify", "notifyInternal"} {
			_, ok := backends[key]
			require.False(t, ok, "адрес notify не объявлен, а ключ %q в карте есть", key)
		}
		resolve := proxy.Resolver(backends)
		for _, m := range notifyRows {
			if _, _, routed := resolve(m); routed {
				t.Errorf("установка без notify, а %q резолвится", m)
			}
		}
	})

	t.Run("with", func(t *testing.T) {
		_, backends := realBackends(t, installationWithNotify)
		resolve := proxy.Resolver(backends)
		for _, m := range notifyRows {
			if _, conn, routed := resolve(m); !routed || conn == nil {
				t.Errorf("близнец: установка с notify, а %q не резолвится", m)
			}
		}
		t.Logf("строк notify в перечне: %d, все резолвятся на установке с notify", len(notifyRows))
	})
}

// TestRouteWiring_EveryBackendKeyHasAConsumer — зеркальная сторона: за каждым
// открытым соединением стоит поверхность, которая его потребляет.
//
// Ключ — либо домен, названный разрешённым списком (его маршрутизирует
// резолвер), либо `<домен>Internal`-спутник такого домена (его потребляют
// internal-REST и opsproxy). Ключ, не являющийся ни тем, ни другим, — либо
// мёртвая провязка, либо домен, чью публичную поверхность забыли внести в
// список; и то и другое находка. Списка исключений у правила нет, поэтому
// истекать нечему.
func TestRouteWiring_EveryBackendKeyHasAConsumer(t *testing.T) {
	_, backends := realBackends(t, installationWithNotify)

	routed := map[string]struct{}{}
	for m := range allowlist.AllowedMethods {
		if d, ok := proxy.RoutableDomain(m); ok {
			routed[d] = struct{}{}
		}
	}
	require.NotEmpty(t, routed, "ни одного домена из списка — читать нечего")

	var orphans []string
	for key := range backends {
		if _, ok := routed[key]; ok {
			continue
		}
		if base, cut := strings.CutSuffix(key, "Internal"); cut {
			if _, ok := routed[base]; ok {
				continue
			}
		}
		orphans = append(orphans, key)
	}
	sort.Strings(orphans)
	t.Logf("перепись: %d ключей в карте, %d доменов маршрутизирует резолвер (%s)",
		len(backends), len(routed), strings.Join(sortedKeys(routed), " "))
	if len(orphans) > 0 {
		t.Errorf("%d ключ(ей) карты бэкендов не потребляет никто: %s — это либо мёртвая провязка, "+
			"либо домен, публичную поверхность которого забыли внести в разрешённый список "+
			"(во втором случае все его публичные RPC получают отказ маршрутизации)",
			len(orphans), strings.Join(orphans, ", "))
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
