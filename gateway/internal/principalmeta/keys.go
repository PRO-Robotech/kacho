// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package principalmeta is the single source of truth for the principal-identity
// header/metadata key contract that authorization decisions,
// operation-ownership enforcement, and principal forwarding all depend on.
//
// Producers (auth / DPoP middleware) set these on the request; consumers
// (authz, idempotency, restmux WithMetadata, opsproxy) read them. Previously
// each side re-typed the bare string literal independently, so a rename on one
// side that missed the other would compile cleanly and silently drop the
// principal at runtime (anonymous → PermissionDenied), a security-relevant break
// with no compile-time or test signal. Referencing these constants makes any
// divergence a compile error.
//
// Three surface forms exist for the SAME logical key:
//
//   - Header*      — canonical HTTP header (http.Header.Set/Get canonicalises to
//     this casing). Used by HTTP-side producers/consumers and downstream audit.
//   - HeaderGRPCMeta* — the "Grpc-Metadata-"-prefixed HTTP header. grpc-gateway
//     forwards an HTTP header `Grpc-Metadata-Foo` to the backend as gRPC
//     metadata `foo`, so producers set BOTH the bare and the prefixed header.
//   - Meta*        — the lowercase gRPC metadata key (metadata.MD is lowercased)
//     read by a backend / opsproxy via metadata.FromIncomingContext.
package principalmeta

import (
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/principalwire"
)

// Canonical HTTP header names.
//
// ИМЕНА ОБЪЯВЛЕНЫ НЕ ЗДЕСЬ. Их единственное объявление — `pkg/principalwire`,
// откуда те же имена берёт фундамент. Здесь стоят только псевдонимы: имя,
// написанное тут своей рукой, было бы вторым объявлением одного предмета, а
// расходятся такие два МОЛЧА — переименование одной стороны собирается чисто и
// кончается ПОТЕРЕЙ личности, а не отказом. Разбор — в шапке
// `pkg/principalwire`; единственность держит гейт дерева `internal/repohygiene`
// `TestIdentityWireNamespaceIsDeclaredOnce`.
const (
	HeaderPrincipalType    = principalwire.HeaderPrincipalType
	HeaderPrincipalID      = principalwire.HeaderPrincipalID
	HeaderPrincipalDisplay = principalwire.HeaderPrincipalDisplay
	HeaderTokenACR         = principalwire.HeaderTokenACR
	HeaderTokenJti         = principalwire.HeaderTokenJti
	HeaderTokenScope       = principalwire.HeaderTokenScope
	HeaderTokenExp         = principalwire.HeaderTokenExp

	// HeaderTokenAMR — ПЕРЕЧЕНЬ СПОСОБОВ, которыми предъявитель себя подтвердил
	// (`amr` у подписанного предъявителя, `authentication_methods` у браузерной
	// сессии). Довод условия модели прав `mfa_fresh`.
	//
	// Форма значения — набор, разделённый пробелом (см. EncodeAuthMethods):
	// ПОРЯДОК НЕ ЗНАЧИМ и не сохраняется, читатель обязан спрашивать о членстве.
	HeaderTokenAMR = principalwire.HeaderTokenAMR

	// HeaderTokenMfaAt — МОМЕНТ ПОДТВЕРЖДЕНИЯ личности, секунды эпохи. Второй
	// довод того же условия: без него свежесть не с чем сравнивать.
	//
	// Отсутствующий заголовок означает «источник момента не назвал» — это НЕ
	// ноль и не «давно»: ноль читался бы как подтверждение в 1970 году, то есть
	// как величина, над которой можно считать. Довода нет ⇒ довода нет.
	HeaderTokenMfaAt = principalwire.HeaderTokenMfaAt

	// HeaderTokenBasicCredentialID — ИДЕНТИФИКАТОР СТРОКИ базового удостоверения,
	// которым предъявитель открыл запрос (kacho#1450).
	//
	// ЗАЧЕМ ОН ЕСТЬ. Про две другие полосы отзыва наш авторитет спрашивается уже
	// сегодня: удостоверение с идентификатором — по `jti`, браузерная сессия —
	// по паре (человек, момент). Базовый секрет не спрашивался НИ ПО ЧЕМУ:
	// вопрос о нём требовал самой предъявленной строки, а держать её живой весь
	// срок открытого соединения значило бы завести поверхность хранения ради
	// контроля. Владелец завёл вопрос ПО ИДЕНТИФИКАТОРУ
	// (`InternalIAMService.CheckBasicCredentialLive`) — этот заголовок и есть то,
	// чем спрашивающий его называет.
	//
	// ЗНАЧЕНИЕ СЕКРЕТА НЕ НЕСЁТ, и это утверждение о значении, а не о форме:
	// сюда кладётся `credential_id` из ответа авторитета, а не предъявленная
	// строка. Владелец энфорсит это на своей стороне — значение, разбираемое как
	// предъявленная строка, отвергается им единым отказом до вопроса.
	//
	// ОСТАЁТСЯ НА КРАЮ (edgeOnlyKeys): за краем у него потребителя нет, а мост
	// пропустил бы его наравне с прочими — префикс мост снимает сам.
	HeaderTokenBasicCredentialID = principalwire.HeaderTokenBasicCredentialID
)

// Grpc-Metadata-prefixed HTTP header names (grpc-gateway → gRPC metadata bridge).
// Псевдонимы единственного объявления — см. выше.
const (
	HeaderGRPCMetaPrincipalType    = principalwire.HeaderGRPCMetaPrincipalType
	HeaderGRPCMetaPrincipalID      = principalwire.HeaderGRPCMetaPrincipalID
	HeaderGRPCMetaPrincipalDisplay = principalwire.HeaderGRPCMetaPrincipalDisplay
	HeaderGRPCMetaTokenACR         = principalwire.HeaderGRPCMetaTokenACR
	HeaderGRPCMetaTokenJti         = principalwire.HeaderGRPCMetaTokenJti
	HeaderGRPCMetaTokenScope       = principalwire.HeaderGRPCMetaTokenScope
)

// Мостовой формы у доводов условия (`amr` / `mfa-at`) НЕТ, и одного её
// отсутствия НЕ ДОСТАТОЧНО, чтобы они остались на краю: мост снимает префикс
// сам, поэтому голая форма пересекает его наравне с префиксованной (замерено —
// см. пробу отбора в restmux). Решение «остаются на краю» поэтому объявлено
// явно, ниже (edgeOnlyKeys), а не выведено из формы имени.

// Lowercase gRPC metadata keys (metadata.MD.Get/Append lowercases its argument;
// backends read exactly these). Псевдонимы единственного объявления — см. выше.
const (
	MetaPrincipalType    = principalwire.MetaPrincipalType
	MetaPrincipalID      = principalwire.MetaPrincipalID
	MetaPrincipalDisplay = principalwire.MetaPrincipalDisplay

	// MetaPrincipalDisplayBin — ДВОИЧНАЯ форма отображаемого имени: обычный ключ
	// роняет весь вызов на первом не-латинском символе. Разбор — у объявления.
	MetaPrincipalDisplayBin = principalwire.MetaPrincipalDisplayBin

	MetaTokenACR   = principalwire.MetaTokenACR
	MetaTokenJti   = principalwire.MetaTokenJti
	MetaTokenScope = principalwire.MetaTokenScope
	MetaTokenAMR   = principalwire.MetaTokenAMR
	MetaTokenMfaAt = principalwire.MetaTokenMfaAt

	// MetaTokenBasicCredentialID — нижнерегистровая форма
	// [HeaderTokenBasicCredentialID]. Объявлена не ради потребителя за краем —
	// его нет, — а ради ЗАПРЕТА: набор ключей края (edgeOnlyKeys) ведётся именно
	// этими именами, и ключ, которого в нём нет, мост пропустил бы молча.
	MetaTokenBasicCredentialID = principalwire.MetaTokenBasicCredentialID
)

// Lowercase prefixes used to strip forgeable client-supplied identity
// headers/metadata before the gateway sets its own trusted values.
// Псевдонимы единственного объявления — см. выше.
const (
	MetaPrincipalPrefix = principalwire.MetaPrincipalPrefix
	MetaTokenPrefix     = principalwire.MetaTokenPrefix
)

// Namespace is the reserved prefix of EVERY Kachō identity/context key on the
// wire. Anything under it is gateway-owned by definition: it is derived from a
// validated credential and a client may never supply it.
//
// BridgePrefix is grpc-gateway's REST→gRPC bridge prefix: an inbound HTTP header
// `Grpc-Metadata-Foo` is forwarded to the backend as gRPC metadata `foo`
// (runtime.DefaultHeaderMatcher). Both surface forms of the same logical key must
// therefore be treated identically by any policy over the namespace.
const (
	Namespace    = principalwire.Namespace
	BridgePrefix = principalwire.BridgePrefix
)

// gatewayProducedPrefixes — the CLOSED set of `x-kacho-` sub-families the
// gateway itself produces after a validated credential and is therefore allowed
// to forward to a backend:
//
//   - `x-kacho-principal-` (type / id / display-name) — set by the Bearer, Kratos,
//     DPoP and mTLS auth paths (setPrincipalHeaders / injectVerifiedTokenHeaders).
//   - `x-kacho-token-` (acr / jti / scope / exp / amr / mfa-at) — the validated
//     credential's own context, consumed by the step-up gate, by the iam
//     acr-floor on the internal re-dial, and — for amr / mfa-at — by the
//     condition arguments the edge builds for the rights model.
//
// Everything else in the namespace — `x-kacho-admin`, `x-kacho-project-id`,
// `x-kacho-actor`, and any key added tomorrow — is client-forgeable input and is
// dropped at the edge.
//
// # Why a closed set and not a list of banned names
//
// The previous form enumerated the FORBIDDEN keys (principal + token). It was
// correct on the day it was written and silently wrong the day a backend started
// reading a third key: `x-kacho-admin` and `x-kacho-project-id` were never added
// to the list, so a client could set `Grpc-Metadata-X-Kacho-Admin: true`, the
// bridge forwarded it verbatim, and kacho-compute raised its cluster-admin flag
// on the PUBLIC listener — lifting the operation-ownership predicate (an
// Operation response carries the created resource in full). A deny-list has to
// be extended for every new key or it leaks; an allow-list has to be extended for
// every new key or the key simply does not work. Only the second failure mode is
// safe, and only the second one is noticed immediately.
var gatewayProducedPrefixes = []string{MetaPrincipalPrefix, MetaTokenPrefix}

// KachoNamespaceKey normalises an inbound HTTP header name or gRPC metadata key
// to its bare lower-cased form and reports whether it belongs to the reserved
// `x-kacho-` namespace. The grpc-gateway bridge prefix is stripped first, so
// `Grpc-Metadata-X-Kacho-Admin` and `x-kacho-admin` both yield
// ("x-kacho-admin", true).
func KachoNamespaceKey(key string) (string, bool) {
	lower := strings.ToLower(key)
	lower = strings.TrimPrefix(lower, BridgePrefix)
	if !strings.HasPrefix(lower, Namespace) {
		return "", false
	}
	return lower, true
}

// IsGatewayProducedKey reports whether a bare lower-cased `x-kacho-` key (as
// returned by KachoNamespaceKey) is one the gateway itself produces — i.e. one
// that may legitimately cross the REST→gRPC bridge to a backend.
// annotatorProducedKeys — ключи, которые кладёт в metadata САМ аннотатор
// (`buildPrincipalMetadata` в `restmux`), и потому мост REST→gRPC обязан их
// отбрасывать: иначе одно логическое значение едет по проводу тремя копиями —
// одной от аннотатора и двумя от моста, по одной на каждую форму заголовка.
//
// # Почему единственным производителем оставлен аннотатор, а не мост
//
// Он умеет то, чего мост не умеет by construction:
//
//   - кладёт отображаемое имя ДВОИЧНЫМ ключом (`…-display-name-bin`). Обычный
//     ключ роняет вызов на первом же не-латинском символе, и падает он не на
//     краю, а на любом последующем запросе арендатора;
//   - читает ОБЕ формы входного заголовка и сводит их к одному значению, то
//     есть число копий не зависит от того, какую форму поставил слой аутентификации.
//
// # Что мост продолжает пропускать
//
// Ключи закрытого набора, у которых есть потребитель за краем
// (`x-kacho-token-jti`/`-scope`/`-exp`): аннотатор их не кладёт, и мост — их
// ЕДИНСТВЕННЫЙ производитель. Снять его целиком значило бы потерять их молча.
//
// Доводы условия модели прав (`-amr`/`-mfa-at`) мост не пропускает вовсе — не
// потому, что у них нет мостовой формы (её отсутствия для этого мало: префикс
// мост снимает сам), а потому что так объявлено — см. edgeOnlyKeys.
var annotatorProducedKeys = map[string]bool{
	MetaPrincipalType:    true,
	MetaPrincipalID:      true,
	MetaPrincipalDisplay: true, // мост знает только обычную форму имени
	MetaTokenACR:         true,
}

// IsAnnotatorProducedKey — кладёт ли этот ключ аннотатор metadata.
//
// Имя приходит уже нормализованным (нижний регистр, без мостового префикса) —
// так его отдаёт KachoNamespaceKey, и обе формы заголовка сводятся к одному
// имени ещё до этого вопроса.
func IsAnnotatorProducedKey(name string) bool { return annotatorProducedKeys[name] }

// edgeOnlyKeys — ключи, которые край производит ДЛЯ СЕБЯ и за мост не пускает.
//
// Оба несут доводы условия модели прав: перечень способов подтверждения и его
// момент. Решение о правах, ради которого они собраны, принимает САМ край —
// восстанавливая удостоверение из этих же заголовков перед вопросом к модели.
// За краем их сегодня не читает никто.
//
// ПОЧЕМУ НЕ ПУСТИТЬ «НА ВСЯКИЙ СЛУЧАЙ». Ключ, доехавший до соседа без
// потребителя, — мёртвая поверхность: завтра её прочтут, не разобравшись, кто
// её ставит и при каких условиях, и она станет входом решения, за которым никто
// не следит. Появится потребитель за краем — запись уходит отсюда вместе с
// заведением мостовой формы, и это осознанное изменение, а не умолчание.
//
// НАБОР ОБЯЗАН ИМЕТЬ ПРЕДМЕТ: запись про ключ, которого нет среди производимых
// краем, — исключение, потерявшее предмет, и гейт пакета считает её находкой.
// РЕШЕНИЕ живёт в каталоге `pkg/principalwire` (поле Key.EdgeOnly) — там же,
// где имена, о которых край и фундамент договариваются. Держать здесь второй
// перечень значило бы завести второе место об одном предмете: ключ, снятый с
// края в каталоге и оставшийся в перечне, разошёлся бы молча.

// IsEdgeOnlyKey — остаётся ли этот ключ на краю (мост его не пропускает).
//
// Имя приходит нормализованным (нижний регистр, без мостового префикса) — так
// его отдаёт KachoNamespaceKey, и обе поверхностные формы сводятся к одному
// имени ещё до этого вопроса.
func IsEdgeOnlyKey(name string) bool { return principalwire.IsEdgeOnly(name) }

func IsGatewayProducedKey(name string) bool {
	for _, p := range gatewayProducedPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// IsClientForgeableKey reports whether an inbound header name / metadata key
// must be stripped before any auth path runs. True for the ENTIRE `x-kacho-`
// namespace in both surface forms: every key under it is gateway-derived, so a
// client-supplied one is by definition a forgery — including the keys the
// gateway produces itself (they are re-set from the validated credential right
// after the strip).
//
// No client-supplied `X-Kacho-…` header is legitimate at the gateway edge: the
// UI and the SDKs authenticate with `Authorization`/`Cookie`/`DPoP`, and the one
// shared-secret Kachō header that does exist (`X-Kacho-Hook-Token`, the
// Hydra→iam webhook) is served by iam's own HTTP listener, not by this gateway.
// If that ever changes, add the key to an explicit allow-list here rather than
// narrowing the namespace sweep.
func IsClientForgeableKey(key string) bool {
	_, ok := KachoNamespaceKey(key)
	return ok
}
