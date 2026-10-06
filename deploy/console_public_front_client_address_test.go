// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts && consolefront

// console_public_front_client_address_test.go — РАЗДАЧА КОНСОЛИ ДОНОСИТ ДО
// КРАЯ АДРЕС КЛИЕНТА, А НЕ ТО, ЧТО КЛИЕНТ О СЕБЕ НАПИСАЛ (kacho#3028).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ И ГРАНИЦА
//
// Край выводит адрес клиента из `X-Forwarded-For`, принятого от раздачи
// консоли (доверенное звено), и этим же адресом служба доступа ключует
// ограничение частоты входа «на источник». Значит, всё, что раздача кладёт в
// этот заголовок, становится источником. Поднимается НАСТОЯЩАЯ раздача —
// образ и карта настройки из рендера цепочки, несущей внешний вход, — а за ней
// дублёр края, который записывает, что раздача ему прислала.
//
// Два клиента с РАЗНЫХ адресов (хост через проброшенный порт и отдельный
// контейнер в той же сети) идут на TLS-вход В КАЖДУЮ полосу раздачи к краю, и
// каждый прикладывает один и тот же подделанный `X-Forwarded-For`. Полосы
// выводятся из карты настройки рендера (всё, что проксирует на адрес края), а
// не выписываются: полоса без своей строки заголовка отдаёт подделку краю как
// есть, и проба, ходящая в одну полосу, остальных не видит. Утверждается:
//
//	предмет  — `X-Forwarded-For`, дошедший до края, равен ровно адресу TCP-пира
//	           раздачи (его же раздача кладёт в `X-Real-IP`), и ни один из двух
//	           заголовков подделанного значения не несёт;
//	близнец  — два клиента дают два разных источника: подделка, одинаковая у
//	           обоих, не сводит их в один.
//
// Подмену адреса на Service (`externalTrafficPolicy`) эта проба не видит —
// kube-proxy в ней нет; ту часть цепочки держит рендер
// (console_public_front_render_test.go, п. 5).
//
// Запуск: `go test -tags 'helmcharts consolefront' -run TestConsolePublicFront ./deploy/`.
// Нет docker, helm или образа раздачи — отказ пробы («условие не создано»), а
// не пропуск.
package deploy_test

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const forgedForwardedFor = "192.0.2.200"

// seenByEdge — что раздача прислала дублёру края на один запрос; LinkSAN —
// имя звена в листе, который раздача предъявила (проверенная цепочка).
type seenByEdge struct{ ForwardedFor, RealIP, LinkSAN string }

// forwardRecorder — дублёр края: запоминает заголовки адреса по метке запроса.
type forwardRecorder struct {
	mu   sync.Mutex
	seen map[string]seenByEdge
}

func (f *forwardRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	s := seenByEdge{ForwardedFor: r.Header.Get("X-Forwarded-For"), RealIP: r.Header.Get("X-Real-IP")}
	if r.TLS != nil && len(r.TLS.VerifiedChains) > 0 && len(r.TLS.VerifiedChains[0]) > 0 {
		s.LinkSAN = strings.Join(r.TLS.VerifiedChains[0][0].DNSNames, ",")
	}
	f.seen[r.Header.Get("X-Request-ID")] = s
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

func (f *forwardRecorder) get(id string) (seenByEdge, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.seen[id]
	return s, ok
}

// edgeUpstreamVar — переменная окружения раздачи с адресом края; полоса к краю —
// та, чей `proxy_pass` идёт на неё.
const edgeUpstreamVar = "${KACHO_UI_API_GATEWAY_UPSTREAM}"

var firstAlternation = regexp.MustCompile(`\(([^|()]+)(\|[^()]*)?\)`)

// edgeLanePath — путь, который попадает в полосу `location <head>`. Образец
// полосы проверяется на выведенном пути: промах — отказ, а не тихая проба
// чужой полосы.
func edgeLanePath(t *testing.T, head string) string {
	t.Helper()
	f := strings.Fields(head)
	var path string
	switch {
	case len(f) == 2 && f[0] == "=":
		return f[1]
	case len(f) == 2 && (f[0] == "~" || f[0] == "~*"):
		path = strings.TrimSuffix(strings.TrimPrefix(f[1], "^"), "$")
		path = firstAlternation.ReplaceAllString(path, "$1")
		if strings.HasSuffix(path, "/") {
			path += "probe"
		}
		if ok, err := regexp.MatchString(f[1], path); err != nil || !ok {
			t.Fatalf("путь %q не попадает в полосу `location %s` (%v) — проба пошла бы в чужую полосу", path, head, err)
		}
		return path
	case len(f) == 2 && f[0] == "^~", len(f) == 1:
		return strings.TrimSuffix(f[len(f)-1], "/") + "/probe"
	}
	t.Fatalf("полоса `location %s`: форма заголовка пробе неизвестна — путь не выведен", head)
	return ""
}

func TestConsolePublicFrontCarriesTheClientAddressNotTheClientClaim(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker не в PATH — раздачу поднять нечем, условие пробы не создано")
	}
	front := frontFromRender(t)
	locs, err := nginxLocations(front.Conf)
	if err != nil {
		t.Fatalf("карта настройки раздачи не разбирается на полосы: %v", err)
	}
	type lane struct{ head, path string }
	var lanes []lane
	for _, l := range locs {
		if strings.Contains(l.Body, edgeUpstreamVar) && l.ProxyPass != "" {
			lanes = append(lanes, lane{l.Head, edgeLanePath(t, l.Head)})
		}
	}
	if len(lanes) == 0 {
		t.Fatalf("в карте настройки раздачи нет ни одной полосы к краю (%s) — судить нечего", edgeUpstreamVar)
	}
	t.Logf("полос раздачи %d · из них к краю %d", len(locs), len(lanes))

	rec := &forwardRecorder{seen: map[string]seenByEdge{}}
	run := startFront(t, front, rec)
	httpsAddr := run.Mapped(front.HTTPS)
	frontIP := dockerOut(t, "inspect", "-f", "{{.NetworkSettings.Networks.bridge.IPAddress}}", run.ID)

	// Клиент A — хост, через проброшенный порт TLS-входа.
	hostClient := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: run.Pool, MinVersion: tls.VersionTLS12}}}
	deadline := time.Now().Add(30 * time.Second)
	for {
		// На внешнем входе точка живости служебная и отказывает (kacho#3030):
		// готовность — любой ответ HTTP, его даёт уже поднятая раздача. Код
		// судит assertHealthPointInsideOnly в пробе формы.
		resp, err := hostClient.Get("https://" + httpsAddr + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("раздача не поднялась на %s за 30 с (последняя ошибка %v)", httpsAddr, err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	// Клиент B — отдельный контейнер той же сети: другой адрес источника.
	sidecar := dockerOut(t, "run", "-d", "--entrypoint", "sleep", front.Image, "300")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", sidecar).Run() })
	clientBIP := dockerOut(t, "inspect", "-f", "{{.NetworkSettings.Networks.bridge.IPAddress}}", sidecar)

	for i, ln := range lanes {
		idA, idB := fmt.Sprintf("lane-%d-a", i), fmt.Sprintf("lane-%d-b", i)
		req, _ := http.NewRequest(http.MethodGet, "https://"+httpsAddr+ln.path, nil)
		req.Header.Set("X-Forwarded-For", forgedForwardedFor)
		req.Header.Set("X-Real-IP", forgedForwardedFor)
		req.Header.Set("X-Request-ID", idA)
		resp, err := hostClient.Do(req)
		if err != nil {
			t.Fatalf("полоса `location %s`, клиент A: %v", ln.head, err)
		}
		resp.Body.Close()
		dockerOut(t, "exec", sidecar, "curl", "-sk", "--max-time", "10", "-o", "/dev/null",
			"-H", "X-Forwarded-For: "+forgedForwardedFor, "-H", "X-Real-IP: "+forgedForwardedFor, "-H", "X-Request-ID: "+idB,
			fmt.Sprintf("https://%s%s", net.JoinHostPort(frontIP, fmt.Sprint(front.HTTPS)), ln.path))

		a, okA := rec.get(idA)
		b, okB := rec.get(idB)
		if !okA || !okB {
			t.Fatalf("полоса `location %s` (%s): до края дошли не оба запроса (A %v, B %v) — условие пробы не создано",
				ln.head, ln.path, okA, okB)
		}
		t.Logf("полоса `location %s` (%s): A X-Forwarded-For=%q X-Real-IP=%q · B X-Forwarded-For=%q X-Real-IP=%q (адрес B %s)",
			ln.head, ln.path, a.ForwardedFor, a.RealIP, b.ForwardedFor, b.RealIP, clientBIP)

		t.Run(fmt.Sprintf("предмет %s: заголовок к краю — адрес пира раздачи", ln.path), func(t *testing.T) {
			for name, s := range map[string]seenByEdge{"A": a, "B": b} {
				if strings.Contains(s.ForwardedFor, forgedForwardedFor) {
					t.Errorf("полоса `location %s`, клиент %s: подделанный адрес дошёл до края в X-Forwarded-For=%q",
						ln.head, name, s.ForwardedFor)
				}
				if strings.Contains(s.RealIP, forgedForwardedFor) {
					t.Errorf("полоса `location %s`, клиент %s: подделанный адрес дошёл до края в X-Real-IP=%q",
						ln.head, name, s.RealIP)
				}
				if s.ForwardedFor != s.RealIP {
					t.Errorf("полоса `location %s`, клиент %s: X-Forwarded-For=%q не равен адресу пира раздачи %q",
						ln.head, name, s.ForwardedFor, s.RealIP)
				}
			}
			if b.RealIP != clientBIP {
				t.Errorf("клиент B: раздача видит пир %q, а клиент пришёл с %s — проба меряет не тот адрес", b.RealIP, clientBIP)
			}
		})
		t.Run(fmt.Sprintf("предмет %s: раздача предъявила краю лист звена", ln.path), func(t *testing.T) {
			for name, s := range map[string]seenByEdge{"A": a, "B": b} {
				if s.LinkSAN != probeLinkSAN {
					t.Errorf("полоса `location %s`, клиент %s: край видит лист звена %q, ожидался %q — "+
						"без листа звена край не принял бы адрес клиента от раздачи", ln.head, name, s.LinkSAN, probeLinkSAN)
				}
			}
		})
		t.Run(fmt.Sprintf("близнец %s: два клиента — два источника", ln.path), func(t *testing.T) {
			if a.ForwardedFor == b.ForwardedFor {
				t.Errorf("полоса `location %s`: два клиента с разных адресов дали краю один источник %q",
					ln.head, a.ForwardedFor)
			}
		})
	}
}
