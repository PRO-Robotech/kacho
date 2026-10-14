// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// context_extractor.go — Build the Condition-evaluation context map from the
// verified JWT + HTTP request.
//
// `CheckRequest.Context` is a `google.protobuf.Struct` of arbitrary keys
// consumed by the rights model's predicate-conditions:
//
//	mfa_fresh(amr_claims, acr_value, current_time, mfa_at)
//	non_expired(current_time, valid_until)
//	source_ip_in_range(client_ip, allowed_cidrs)
//	business_hours(current_time, tz, start_h, end_h)
//	device_compliant(device_attestation, allowed_attestations)
//	jit_window(current_time, activated_at, ttl_seconds)
//
// This extractor builds the *caller-side* half of those keys — the ones
// derivable from the JWT and the incoming HTTP request. Predicate-side
// parameters (`allowed_cidrs`, `valid_until`, `expires_at`, `tz`, ...) live on
// the direct fact itself and are merged by iam when it computes the verdict.
//
// Reserved keys this extractor emits:
//
//	current_time        timestamp (seconds since epoch) — always
//	client_ip           string (canonical IP literal)   — when resolvable
//	acr_value           string ("0".."3")               — from token.ACR
//	amr_claims          []string                        — from token.AMR
//	mfa_at              timestamp                       — from ext_claims.kaname_mfa_at
//	device_attestation  string                          — from ext_claims.kaname_device_compliance
//	dpop_jkt            string                          — from token.Cnf.Jkt
//	auth_time           timestamp                       — from token.AuthTime
//	jti                 string                          — from token.JTI (for replay-trace correlation)
//	subject_kind        string ("user"/"service_account"/"workload"/"external")
//
// Beyond the reserved keys above, any remaining `kaname_*`-prefixed ext_claims
// are forwarded verbatim under their ORIGINAL key name (no prefix rewrite), so
// future Conditions can read them without an extractor change. Non-`kaname_*`
// ext_claims keys are dropped entirely. The condition set of the rights model is
// closed, so tenant-supplied junk never participates in condition evaluation.
package middleware

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/gateway/internal/linktls"
)

// ContextExtractor — stateless builder.
type ContextExtractor struct {
	// now — injectable clock for tests; defaults to time.Now.
	now func() time.Time

	// trustedXForwardedFor controls whether `X-Forwarded-For` / `X-Real-IP`
	// headers are honoured when computing `client_ip`. In production we sit
	// behind an L7 LB that strips client-supplied values and inserts the
	// trusted peer; on a misconfigured deploy a tenant could spoof
	// `source_ip_in_range` via a forged X-Forwarded-For. Default = true
	// (typical k8s ingress topology); operators can flip to false when
	// running api-gateway directly on the wire.
	trustedXForwardedFor bool

	// trustedProxyCount is the number of trusted reverse-proxy hops in front of
	// the gateway. X-Forwarded-For is read from the RIGHT — the client IP is the
	// entry the OUTERMOST trusted proxy recorded (parts[len-trustedProxyCount]).
	// A client can only forge entries to the LEFT of that trusted block, which we
	// never select, so a spoofed leftmost XFF can no longer drive `client_ip`.
	// 0 disables forwarded-header trust entirely (TCP peer is authoritative).
	// Default 1 (single k8s ingress).
	trustedProxyCount int

	// trustedProxies — КРУГ ДОВЕРЕННЫХ ЗВЕНЬЕВ (kacho#3028): заголовки
	// пересылки читаются, ТОЛЬКО если TCP-пир запроса лежит в одной из этих
	// сетей. Заголовок пишет кто угодно; число прыжков говорит, СКОЛЬКО звеньев
	// стоит перед краем, но не КТО они, — без круга любой под кластера, дошедший
	// до края напрямую, сдвигал бы `client_ip` и ключ ограничения частоты
	// службы доступа одной строкой заголовка. Пустой круг — «не доверяю
	// никому»: источник — сам TCP-пир. Это НЕ «не сужаю».
	trustedProxies []netip.Prefix

	// trustedPeers — УЗКИЙ КРУГ (kacho#3028, круг 3): звено фронта поимённо.
	// Пир доверен, только если он и в сети круга, и в этом перечне; nil —
	// «никому».
	trustedPeers PeerSet

	// trustedSANs — ИМЕНА ЗВЕНЬЕВ В СЕРТИФИКАТЕ (kacho#3028, C4): пир — звено,
	// только если он предъявил клиентский сертификат, проверенный якорем
	// установки, с одним из этих имён (URI либо DNS). Адрес пода — не
	// личность: под с теми же метками попадает в службу фронта, адрес ушедшего
	// пода выдаётся другому. Пусто — «никому».
	trustedSANs map[string]struct{}

	// linkAnchor — ЯКОРЬ ЗВЕНЬЕВ (kacho#3028, круг 5): имя звена читается
	// только в листе, чья проверенная цепочка кончается корнем этого якоря.
	// Якорь установки выпускает листы кластерным выпускающим — имя звена в его
	// листе выдаёт себе всякий, кто заводит запрос на сертификат в любом
	// пространстве имён. Пусто — «никому».
	linkAnchor linktls.Anchor
}

// ExtractorOption configures a ContextExtractor at construction.
type ExtractorOption func(*ContextExtractor)

// WithTrustedProxyHops sets the number of trusted reverse-proxy hops in front of
// the gateway (see ContextExtractor.trustedProxyCount). 0 disables
// forwarded-header trust; the TCP peer becomes authoritative.
func WithTrustedProxyHops(n int) ExtractorOption {
	return func(e *ContextExtractor) {
		if n < 0 {
			n = 0
		}
		e.trustedProxyCount = n
	}
}

// WithTrustedProxies объявляет круг доверенных звеньев (см.
// ContextExtractor.trustedProxies). Без этой опции заголовки пересылки не
// принимаются ни от одного пира.
func WithTrustedProxies(prefixes ...netip.Prefix) ExtractorOption {
	return func(e *ContextExtractor) {
		e.trustedProxies = append([]netip.Prefix(nil), prefixes...)
	}
}

// PeerSet — звенья фронта поимённо (kacho#3028, круг 3): адреса подов, которые
// выбирают безголовые службы фронта (gateway/internal/frontpeers). Сеть круга
// говорит «под кластера», перечень — «звено фронта».
type PeerSet interface{ Trusts(netip.Addr) bool }

// WithTrustedPeers объявляет звенья фронта поимённо. Без этой опции заголовки
// пересылки не принимаются ни от одного пира, каков бы ни был круг сетей:
// сеть подов общая, и доверие ей — доверие любому поду кластера.
func WithTrustedPeers(p PeerSet) ExtractorOption {
	return func(e *ContextExtractor) { e.trustedPeers = p }
}

// WithTrustedLinkSANs объявляет имена звеньев в сертификате (см.
// ContextExtractor.trustedSANs). Без этой опции заголовки пересылки не
// принимаются ни от одного пира, каковы бы ни были круг и перечень поимённо.
func WithTrustedLinkSANs(sans ...string) ExtractorOption {
	return func(e *ContextExtractor) {
		e.trustedSANs = make(map[string]struct{}, len(sans))
		for _, s := range sans {
			e.trustedSANs[s] = struct{}{}
		}
	}
}

// WithTrustedLinkAnchor объявляет якорь звеньев (см.
// ContextExtractor.linkAnchor). Без этой опции заголовки пересылки не
// принимаются ни от одного пира.
func WithTrustedLinkAnchor(a linktls.Anchor) ExtractorOption {
	return func(e *ContextExtractor) { e.linkAnchor = a }
}

// NewContextExtractor constructs an extractor. now=nil falls back to
// time.Now; trustedXForwardedFor toggles X-Forwarded-For honour (see field
// comment). The number of trusted proxy hops defaults to 1 and can be overridden
// with WithTrustedProxyHops.
func NewContextExtractor(now func() time.Time, trustedXForwardedFor bool, opts ...ExtractorOption) *ContextExtractor {
	if now == nil {
		now = time.Now
	}
	e := &ContextExtractor{now: now, trustedXForwardedFor: trustedXForwardedFor, trustedProxyCount: 1}
	for _, o := range opts {
		o(e)
	}
	return e
}

// BuildHTTP composes the context map for an HTTP request path.
//
// `subject` may be empty when the caller is anonymous; the function still
// builds a map (with `current_time` always present) so the FGA Check can run
// over `<exempt>`-like cases consistently.
func (e *ContextExtractor) BuildHTTP(t *VerifiedToken, r *http.Request, subj ResolvedSubject) map[string]any {
	out := map[string]any{
		// truncated to seconds to match the model's Condition timestamps.
		"current_time": e.now().UTC().Truncate(time.Second).Unix(),
	}
	if r != nil {
		if ip := e.clientIP(httpForwarded(r)); ip != "" {
			out["client_ip"] = ip
		}
	}
	e.fillFromToken(out, t)
	if subj.FGA != "" {
		out["subject_kind"] = subjectKindString(subj.Kind)
	}
	return out
}

// BuildPeerAddr is the gRPC counterpart of BuildHTTP — when there is no
// http.Request: the peer address, the TLS state of the peer connection (nil
// when it is not TLS) and the incoming metadata.
func (e *ContextExtractor) BuildPeerAddr(t *VerifiedToken, peerAddr net.Addr, link *tls.ConnectionState, md metadata.MD, subj ResolvedSubject) map[string]any {
	out := map[string]any{
		"current_time": e.now().UTC().Truncate(time.Second).Unix(),
	}
	if peerAddr != nil {
		if ip := e.clientIP(grpcForwarded(peerAddr, link, md)); ip != "" {
			out["client_ip"] = ip
		}
	}
	e.fillFromToken(out, t)
	if subj.FGA != "" {
		out["subject_kind"] = subjectKindString(subj.Kind)
	}
	return out
}

func (e *ContextExtractor) fillFromToken(out map[string]any, t *VerifiedToken) {
	if t == nil {
		return
	}
	if t.ACR != "" {
		out["acr_value"] = t.ACR
	}
	if len(t.AMR) > 0 {
		// Copy to avoid the caller mutating shared slice.
		cp := make([]string, len(t.AMR))
		copy(cp, t.AMR)
		out["amr_claims"] = cp
	}
	if !t.AuthTime.IsZero() {
		out["auth_time"] = t.AuthTime.UTC().Truncate(time.Second).Unix()
	}
	if t.JTI != "" {
		out["jti"] = t.JTI
	}
	if t.Cnf.HasJkt {
		out["dpop_jkt"] = t.Cnf.Jkt
	}
	if ext := t.ExtClaims; ext != nil {
		// Recognised kaname_* claims — extracted with the canonical condition
		// key.
		if v, ok := ext["kaname_mfa_at"]; ok {
			if ts, ok := coerceUnixSeconds(v); ok {
				out["mfa_at"] = ts
			}
		}
		if v, ok := ext["kaname_device_compliance"].(string); ok && v != "" {
			out["device_attestation"] = v
		}
		// Forward any other kaname_* claims under their original name so
		// future Conditions can read them without an extractor change.
		for k, v := range ext {
			if !strings.HasPrefix(k, "kaname_") {
				continue
			}
			// Already extracted above, or already carried as the principal
			// itself — a claim that names WHO is calling is not an input to a
			// condition about the call.
			switch k {
			case "kaname_mfa_at", "kaname_device_compliance",
				"kaname_principal_type", "kaname_principal_id", "kaname_user_id":
				continue
			}
			out[k] = v
		}
	}
}

// ClientIP — адрес клиента, выведенный ТЕМ ЖЕ оператором, что кормит условие
// `client_ip` модели прав: доверенные заголовки пересылки читаются справа по
// числу доверенных прыжков, иначе — TCP-пир. Экспортирован для ретрансляции
// полосы формы (приёмка Ф3 Р2): служба читает `X-Forwarded-For` как ОДИН адрес,
// и адрес этот выводит край — не цепочка, которую строит раздача консоли перед
// ним. Второй оператор чтения цепочки разошёлся бы с первым молча.
func (e *ContextExtractor) ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	return e.clientIP(httpForwarded(r))
}

// forwarded — что запрос говорит о своём источнике, как он пришёл: TCP-пир,
// состояние TLS соединения с ним и заголовки пересылки. Собирают его ТОЛЬКО
// два читателя ниже — по одному на транспорт; судит один clientIP. Других
// чтений заголовков пересылки в крае нет (гейт
// internal/repohygiene/clientaddressreader.go).
type forwarded struct {
	peer string
	link *tls.ConnectionState
	// xff — ВСЕ значения X-Forwarded-For, склеенные по порядку: клиент вправе
	// прислать заголовок дважды, и взятое первое значение отдавало бы выбор ему.
	xff string
	// xRealIP — единственное значение X-Real-IP; при нескольких — пусто
	// (неоднозначность не разрешается в пользу клиента).
	xRealIP string
}

// httpForwarded — читатель HTTP. Состояние TLS — linktls-контекст соединения
// либо r.TLS (gateway/internal/linktls.FromRequest).
func httpForwarded(r *http.Request) forwarded {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	f := forwarded{peer: host, link: linktls.FromRequest(r), xff: strings.Join(r.Header.Values("X-Forwarded-For"), ",")}
	if v := r.Header.Values("X-Real-IP"); len(v) == 1 {
		f.xRealIP = v[0]
	}
	return f
}

// grpcForwarded — читатель нативного gRPC. Читается ТОЛЬКО `x-forwarded-for`,
// все значения по порядку. Метаданные `grpcgateway-*` не читаются НИКОГДА: их
// пишет наш мост в процессе, а мост на нативный слушатель не ходит (REST
// судится по самому http.Request, BuildHTTP), — на нативном пути их пишет
// клиент. `x-real-ip` на gRPC не читается: звено фронта пишет адрес в
// `x-forwarded-for`.
func grpcForwarded(peerAddr net.Addr, link *tls.ConnectionState, md metadata.MD) forwarded {
	host, _, err := net.SplitHostPort(peerAddr.String())
	if err != nil {
		host = peerAddr.String()
	}
	return forwarded{peer: host, link: link, xff: strings.Join(md.Get("x-forwarded-for"), ",")}
}

// clientIP — ЕДИНСТВЕННЫЙ оператор адреса клиента края: заголовки пересылки
// принимаются только от звена фронта (isLink), иначе источник — TCP-пир.
//
// X-Forwarded-For разбирается СПРАВА: при N доверенных прыжках адрес клиента —
// parts[len-N], запись, которую сделало внешнее доверенное звено. Подделка
// клиента ложится ЛЕВЕЕ этого блока и не выбирается. X-Real-IP (одно значение,
// вычисленное звеном) — запасной путь и тоже только от звена.
func (e *ContextExtractor) clientIP(f forwarded) string {
	if e.isLink(f) {
		if f.xff != "" {
			parts := strings.Split(f.xff, ",")
			if idx := len(parts) - e.trustedProxyCount; idx >= 0 && idx < len(parts) {
				if ip := strings.TrimSpace(parts[idx]); validIP(ip) {
					return canonicaliseIP(ip)
				}
			}
		}
		if v := strings.TrimSpace(f.xRealIP); v != "" && validIP(v) {
			return canonicaliseIP(v)
		}
	}
	if validIP(f.peer) {
		return canonicaliseIP(f.peer)
	}
	return ""
}

// TrustsNobody — не принимает ли оператор заголовки пересылки ни от одного
// пира: доверие выключено флагом, нулём прыжков, круг пуст, звеньев поимённо
// нет, имён звеньев в сертификате нет либо нет якоря звеньев.
func (e *ContextExtractor) TrustsNobody() bool {
	return !e.trustedXForwardedFor || e.trustedProxyCount <= 0 || len(e.trustedProxies) == 0 ||
		e.trustedPeers == nil || len(e.trustedSANs) == 0 || e.linkAnchor.Empty()
}

// isLink — ОДИН предикат доверия к пиру: звено ли он. Звено — пир, который
// лежит в сети круга, предъявил проверенный якорем сертификат с именем звена и
// назван поимённо перечнем звеньев фронта. Порядок несущий: промах перечня
// будит его обновление, и будить его вправе только пир сети круга с
// сертификатом звена. Любое «нет» — источник сам пир.
func (e *ContextExtractor) isLink(f forwarded) bool {
	if e.TrustsNobody() {
		return false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(f.peer))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	inCircle := false
	for _, p := range e.trustedProxies {
		if p.Contains(addr) {
			inCircle = true
			break
		}
	}
	return inCircle && e.linkNamed(f.link) && e.trustedPeers.Trusts(addr)
}

// linkNamed — несёт ли ПРОВЕРЕННЫЙ лист ЯКОРЯ ЗВЕНЬЕВ имя звена.
// Предъявленный, но не проверенный сертификат (PeerCertificates без
// VerifiedChains) — не звено: имя в нём написал кто угодно. Проверенный
// якорем установки — тоже не звено (круг 5): лист с любым именем там выдаёт
// себе всякий, кто заводит запрос на сертификат в любом пространстве имён.
func (e *ContextExtractor) linkNamed(st *tls.ConnectionState) bool {
	if st == nil || !st.HandshakeComplete {
		return false
	}
	leaf := e.linkAnchor.Issued(st.VerifiedChains)
	if leaf == nil {
		return false
	}
	for _, u := range leaf.URIs {
		if _, ok := e.trustedSANs[u.String()]; ok {
			return true
		}
	}
	for _, d := range leaf.DNSNames {
		if _, ok := e.trustedSANs[d]; ok {
			return true
		}
	}
	return false
}

// canonicaliseIP normalises an IP literal (trims, lowercases IPv6) so cache
// keys derived from it are stable.
func canonicaliseIP(s string) string {
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return s
}

// validIP reports whether s parses as an IP literal.
func validIP(s string) bool {
	return net.ParseIP(s) != nil
}

// subjectKindString — stable string label for SubjectKind. Used as
// `subject_kind` context key.
func subjectKindString(k SubjectKind) string {
	switch k {
	case SubjectKindUser:
		return "user"
	case SubjectKindServiceAccount:
		return "service_account"
	case SubjectKindWorkload:
		return "workload"
	case SubjectKindExternal:
		return "external"
	default:
		return ""
	}
}

// coerceUnixSeconds reads a JSON-decoded value (likely float64 from JWT
// claims) into a Unix-seconds int64. Returns false on type mismatch.
func coerceUnixSeconds(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case string:
		// Try parse as RFC3339 first, then unix-seconds string.
		if ts, err := time.Parse(time.RFC3339, n); err == nil {
			return ts.UTC().Truncate(time.Second).Unix(), true
		}
	}
	return 0, false
}
