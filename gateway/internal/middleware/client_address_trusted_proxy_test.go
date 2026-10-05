// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// client_address_trusted_proxy_test.go — АДРЕС КЛИЕНТА ИЗ ЗАГОЛОВКА ПЕРЕСЫЛКИ
// ПРИНИМАЕТСЯ ТОЛЬКО ОТ ДОВЕРЕННОГО ЗВЕНА (kacho#3028).
//
// Предмет — оператор адреса клиента края: он кормит и условие `client_ip`
// модели прав, и `X-Forwarded-For` ретрансляции полосы входа, по которому
// служба доступа ведёт ограничение частоты «на источник». Заголовок пересылки
// пишет кто угодно; доверять ему можно только тогда, когда TCP-пир — звено,
// объявленное доверенным (раздача консоли). Иначе источник — сам TCP-пир.
//
// Каждое отрицательное утверждение — в паре с положительным близнецом,
// отличным в один факт (пир внутри круга против пира вне его).
package middleware_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// Документационные адреса (RFC 5737): клиенты снаружи и пиры внутри.
const (
	frontPod     = "10.244.1.17" // раздача консоли — доверенное звено
	otherPod     = "10.250.3.4"  // любой иной пир — НЕ доверенное звено
	podInCircle  = "10.244.3.4"  // под в сети круга, службой фронта НЕ выбранный
	clientA      = "198.51.100.23"
	clientB      = "203.0.113.41"
	forgedSource = "192.0.2.200"
)

func httpFrom(peer, xff string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/auth/login", nil)
	r.RemoteAddr = net.JoinHostPort(peer, "40000")
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

// Без объявленного круга доверенных звеньев заголовок пересылки не
// принимается ни от кого: источник — TCP-пир.
func TestClientAddress_NoDeclaredCircleTrustsNobody(t *testing.T) {
	e := middleware.NewContextExtractor(time.Now, true)
	if got := e.ClientIP(httpFrom(otherPod, forgedSource)); got != otherPod {
		t.Fatalf("пир %s без круга доверия сдвинул источник заголовком на %q; ожидался сам пир", otherPod, got)
	}
	addr := &net.TCPAddr{IP: net.ParseIP(otherPod), Port: 40000}
	ctx := e.BuildPeerAddr(nil, addr, forgedSource, middleware.ResolvedSubject{})
	if got := ctx["client_ip"]; got != otherPod {
		t.Fatalf("gRPC: пир %s без круга доверия сдвинул источник на %v", otherPod, got)
	}
}

// links — звенья фронта поимённо: адреса подов, выбранных службами фронта
// (gateway/internal/frontpeers). Здесь — неподвижный перечень.
type links []string

func (l links) Trusts(a netip.Addr) bool {
	for _, s := range l {
		if netip.MustParseAddr(s) == a.Unmap() {
			return true
		}
	}
	return false
}

// trustingTheFront — край за раздачей консоли: один доверенный прыжок, круг —
// сеть подов, где раздача живёт, звено — под раздачи поимённо.
func trustingTheFront() *middleware.ContextExtractor {
	return middleware.NewContextExtractor(time.Now, true,
		middleware.WithTrustedProxyHops(1),
		middleware.WithTrustedProxies(netip.MustParsePrefix("10.244.0.0/16")),
		middleware.WithTrustedPeers(links{frontPod}))
}

// УЗКИЙ КРУГ (kacho#3028, круг 3). Сеть круга — «под кластера», а не «звено
// фронта»: под в той же сети, службой фронта не выбранный, источник заголовком
// не сдвигает. Близнец — тот же заголовок от пода раздачи — принимается.
func TestClientAddress_PodInTheCircleNetworkIsNotALinkUnlessTheFrontSelectsIt(t *testing.T) {
	e := trustingTheFront()
	if got := e.ClientIP(httpFrom(podInCircle, forgedSource)); got != podInCircle {
		t.Errorf("под %s в сети круга, не звено фронта, сдвинул источник на %q", podInCircle, got)
	}
	addr := &net.TCPAddr{IP: net.ParseIP(podInCircle), Port: 40000}
	if got := e.BuildPeerAddr(nil, addr, forgedSource, middleware.ResolvedSubject{})["client_ip"]; got != podInCircle {
		t.Errorf("gRPC: под %s в сети круга, не звено фронта, сдвинул источник на %v", podInCircle, got)
	}
	if got := e.ClientIP(httpFrom(frontPod, clientA)); got != clientA {
		t.Errorf("близнец: заголовок звена фронта не принят: %q", got)
	}
}

// Сеть без звеньев поимённо — «никому»: доверие всей сети подов не выдаётся
// ни при каком круге. Близнец — звено в той же сети, названное поимённо,
// принимается (выше, trustingTheFront).
func TestClientAddress_CircleWithoutNamedLinksTrustsNobody(t *testing.T) {
	e := middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1),
		middleware.WithTrustedProxies(netip.MustParsePrefix("10.244.0.0/16")))
	if got := e.ClientIP(httpFrom(frontPod, forgedSource)); got != frontPod {
		t.Fatalf("круг без звеньев поимённо: заголовок пира %s сдвинул источник на %q", frontPod, got)
	}
	if !e.TrustsNobody() {
		t.Fatal("круг без звеньев поимённо: TrustsNobody() = false, а заголовок не принимается ни от кого")
	}
}

// Звено поимённо, но вне сети круга (адрес публичный либо чужой сети), — не
// звено: сеть круга остаётся внешней границей.
func TestClientAddress_NamedLinkOutsideTheCircleNetworkIsNotALink(t *testing.T) {
	e := middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1),
		middleware.WithTrustedProxies(netip.MustParsePrefix("10.244.0.0/16")),
		middleware.WithTrustedPeers(links{frontPod, otherPod}))
	if got := e.ClientIP(httpFrom(otherPod, forgedSource)); got != otherPod {
		t.Fatalf("звено %s вне сети круга сдвинуло источник на %q", otherPod, got)
	}
	if got := e.ClientIP(httpFrom(frontPod, clientA)); got != clientA {
		t.Fatalf("близнец: звено в сети круга не принято: %q", got)
	}
}

// Пир вне круга (под кластера, дошедший до края напрямую) не сдвигает
// источник; близнец — тот же заголовок от раздачи в круге — принимается.
func TestClientAddress_ForwardedHeaderIsHonouredOnlyFromTheDeclaredCircle(t *testing.T) {
	e := trustingTheFront()
	if got := e.ClientIP(httpFrom(otherPod, forgedSource)); got != otherPod {
		t.Errorf("пир вне круга сдвинул источник на %q; ожидался сам пир %s", got, otherPod)
	}
	if got := e.ClientIP(httpFrom(frontPod, clientA)); got != clientA {
		t.Errorf("заголовок раздачи из круга не принят: источник %q, ожидался %s", got, clientA)
	}
	outside := &net.TCPAddr{IP: net.ParseIP(otherPod), Port: 40000}
	if got := e.BuildPeerAddr(nil, outside, forgedSource, middleware.ResolvedSubject{})["client_ip"]; got != otherPod {
		t.Errorf("gRPC: пир вне круга сдвинул источник на %v", got)
	}
	inside := &net.TCPAddr{IP: net.ParseIP(frontPod), Port: 40000}
	if got := e.BuildPeerAddr(nil, inside, clientA, middleware.ResolvedSubject{})["client_ip"]; got != clientA {
		t.Errorf("gRPC: заголовок звена из круга не принят: %v", got)
	}
}

// Находка kacho#3028 в форме края: два клиента за раздачей — два источника, а
// подделка, приложенная клиентом, не меняет источника (раздача перезаписывает
// заголовок адресом пира, и край читает его справа).
func TestClientAddress_TwoClientsBehindTheFrontAreTwoSources(t *testing.T) {
	e := trustingTheFront()
	a := e.ClientIP(httpFrom(frontPod, clientA))
	b := e.ClientIP(httpFrom(frontPod, clientB))
	if a != clientA || b != clientB || a == b {
		t.Fatalf("за раздачей два клиента дали источники %q и %q; ожидались %s и %s", a, b, clientA, clientB)
	}
	// Звено, которое ДОПИСЫВАЕТ, а не перезаписывает: подделка левее записи
	// звена справа не выбирается.
	if got := e.ClientIP(httpFrom(frontPod, forgedSource+", "+clientA)); got != clientA {
		t.Fatalf("подделка левее записи звена выбрана: %q", got)
	}
}

// IPv4, пришедший как IPv4-в-IPv6, судится тем же кругом.
func TestClientAddress_MappedPeerIsJudgedByTheSameCircle(t *testing.T) {
	e := trustingTheFront()
	if got := e.ClientIP(httpFrom("::ffff:"+frontPod, clientA)); got != clientA {
		t.Fatalf("пир ::ffff:%s не узнан как звено круга: %q", frontPod, got)
	}
}

// TrustsNobody — то, что край печатает на старте: «заголовок не принимается ни
// от кого» истинно ровно тогда, когда так и ведёт себя ClientIP.
func TestClientAddress_TrustsNobodyAgreesWithBehaviour(t *testing.T) {
	front := netip.MustParsePrefix("10.244.0.0/16")
	for _, c := range []struct {
		name string
		e    *middleware.ContextExtractor
		want bool
	}{
		{"круг пуст", middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)), true},
		{"доверие выключено флагом", middleware.NewContextExtractor(time.Now, false, middleware.WithTrustedProxyHops(1),
			middleware.WithTrustedProxies(front)), true},
		{"ноль прыжков", middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(0),
			middleware.WithTrustedProxies(front)), true},
		{"круг без звеньев поимённо", middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1),
			middleware.WithTrustedProxies(front)), true},
		{"близнец: круг объявлен", trustingTheFront(), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.e.TrustsNobody(); got != c.want {
				t.Fatalf("TrustsNobody() = %v, ожидалось %v", got, c.want)
			}
			moved := c.e.ClientIP(httpFrom(frontPod, clientA)) != frontPod
			if moved == c.want {
				t.Fatalf("TrustsNobody() = %v, а заголовок от пира в круге %s источник сдвинул: %v", c.want, frontPod, moved)
			}
		})
	}
}
