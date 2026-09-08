// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package clients — единая точка сборки gRPC-клиентских соединений из kacho-vpc к
// peer-сервисам (kaname, kacho-geo) единым паттерном (retries, LB, TLS,
// metrics), без отдельного dial-кода на каждый клиент.
//
// Builder — обёртка над `grpcclient.DialPeer` (`pkg/grpcclient/dial.go`) с
// дефолтами kacho-vpc (retries=3, dialTimeout=10s, KeepAlive 30s,
// userAgent="kacho-vpc"). Client-side round_robin LB включается флагом DNSLB.
//
// ПУТЬ ОДИН НА ОБА ФЛАГА. Прежде их было два: сторонний строитель не давал
// объявить конфигурацию службы, поэтому распределение собиралось отдельной
// веткой «вручную», зеркалившей первую. Зеркало и оригинал — два места об одном
// предмете; со снятием стороннего строителя (его модуль не нёс лицензии)
// предмет зеркала исчез, и DNSLB стал тем, чем всегда был по смыслу, —
// ОДНИМ ПАРАМЕТРОМ соединения, а не второй его сборкой.
//
// Возвращает `Conn` — interface `grpc.ClientConnInterface + io.Closer`. Generated
// proto-клиенты (`iamv1.NewProjectServiceClient(conn)` и т.п.) принимают
// `grpc.ClientConnInterface`.
package clients

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/resolver/dns" // регистрирует dns:/// resolver (для DNSLB)
)

// Conn — то, что нужно generated proto-клиентам (`grpc.ClientConnInterface`)
// плюс возможность Close.
type Conn interface {
	grpc.ClientConnInterface
	io.Closer
}

// BuildOptions — параметры сборки cross-service gRPC-клиента.
//
// Endpoint — host:port (или `dns:///host:port`, если уже с префиксом).
// TLS=true → credentials.NewTLS(MinVersion=1.2); иначе insecure (dev).
// DNSLB=true → префикс `dns:///` + service-config с round_robin LB.
//
// Retries / DialTimeout / KeepAliveTime — дефолты задаются через withDefaults().
type BuildOptions struct {
	Endpoint      string        // host:port (либо уже dns:///host:port)
	TLS           bool          // true → TLS 1.2+; false → insecure (dev)
	DNSLB         bool          // true → dns:///prefix + round_robin LB
	Retries       uint          // gRPC retries on Unavailable (default 3)
	DialTimeout   time.Duration // dial backoff target (default 10s)
	KeepAliveTime time.Duration // ping every (default 30s)
	UserAgent     string        // gRPC User-Agent (default "kacho-vpc")
}

// defaultBuildOptions — дефолты для kacho-vpc cross-service вызовов
// (retries=3, dial 10s, keepalive 30s). Подбираются под profile peer-сервисов
// (Project.Get / Zone.Get — short calls, цена retry мала; idle longer для
// низкочастотных кешированных путей).
const (
	defaultRetries       = 3
	defaultDialTimeout   = 10 * time.Second
	defaultKeepAliveTime = 30 * time.Second
	defaultUserAgent     = "kacho-vpc"
)

// defaultPeerCallTimeout — per-call deadline на КАЖДЫЙ исходящий peer-gRPC вызов
// cross-service peer-клиентов (geo Zone/Region Get, iam Project Exists). Эти
// вызовы идут в том числе из async Operation-worker'а, чей ctx лишён дедлайна
// (operations baggage.Extract снимает deadline/cancel) и ограничен только грубым
// opTimeout; без собственного per-call дедлайна alive-but-unresponsive peer
// (deadlocked handler / GC-pause / slow query — gRPC keepalive не срабатывает,
// пока stream активен) вешает worker-горутину надолго → исчерпание LRO-слотов
// (DoS-амплификация). Зеркалит sibling SyncRegistrar (5s). См. architecture.md
// «per-call deadline на КАЖДОМ внешнем вызове».
const defaultPeerCallTimeout = 5 * time.Second

// peerCallCtx оборачивает ctx per-call дедлайном (если timeout>0), иначе возвращает
// ctx как есть. Применяется единообразно ко ВСЕМ sibling-методам peer-клиентов —
// не «часть — да, часть — нет». Возвращаемый cancel обязателен к вызову (defer).
func peerCallCtx(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (o BuildOptions) withDefaults() BuildOptions {
	if o.Retries == 0 {
		o.Retries = defaultRetries
	}
	if o.DialTimeout == 0 {
		o.DialTimeout = defaultDialTimeout
	}
	if o.KeepAliveTime == 0 {
		o.KeepAliveTime = defaultKeepAliveTime
	}
	if o.UserAgent == "" {
		o.UserAgent = defaultUserAgent
	}
	return o
}

// Build открывает gRPC-клиентское соединение по BuildOptions.
//
// DNSLB — ПАРАМЕТР соединения, а не вторая его сборка: при DNSLB=true адрес
// резолвится схемой `dns:///` (она отдаёт ВСЕ адреса Headless Service) и
// объявляется `round_robin`; при DNSLB=false адрес отдаётся набирателю как есть
// (`passthrough`), и балансировать нечего. Всё остальное — retries на
// UNAVAILABLE, отступ переподключения, keepalive, creds, userAgent — на обоих
// флагах одно и то же by construction.
//
// ctx принимается ради формы вызова и НЕ участвует в наборе: `grpc.NewClient`
// откладывает соединение до первого вызова.
//
// Возвращает `Conn` — interface с grpc.ClientConnInterface + io.Closer.
// Подходит для передачи в generated `xxxv1.NewXxxServiceClient(conn)`.
func Build(_ context.Context, opts BuildOptions) (Conn, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return nil, fmt.Errorf("clients.Build: empty Endpoint")
	}
	opts = opts.withDefaults()

	cc, err := grpcclient.DialPeer(grpcclient.PeerDialOptions{
		Endpoint:      opts.Endpoint,
		Creds:         buildCreds(opts.TLS),
		Retries:       opts.Retries,
		DialTimeout:   opts.DialTimeout,
		KeepAliveTime: opts.KeepAliveTime,
		UserAgent:     opts.UserAgent,
		RoundRobin:    opts.DNSLB,
	})
	if err != nil {
		return nil, fmt.Errorf("clients.Build: dial %q (DNSLB=%v): %w", opts.Endpoint, opts.DNSLB, err)
	}
	return cc, nil
}

// buildCreds — единый source-of-truth TLS / insecure для всех cross-service
// клиентов; TLS MinVersion=1.2 верифицирует server-сертификат по системному
// trust store (production-strict mode требует TLS, см. validateAuthMode).
func buildCreds(useTLS bool) credentials.TransportCredentials {
	if useTLS {
		return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	return insecure.NewCredentials()
}
