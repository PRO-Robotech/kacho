// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// jwt_verifier.go — JWKS-cached JWT verifier for Hydra-issued access tokens.
//
// Pipeline (RFC 8725 hardening applied):
//  1. Parse JWT header → require `kid` + alg ∈ {RS256, ES256, EdDSA}; reject
//     `alg=none` / `HS*` BEFORE key resolution (algorithm-confusion mitigation).
//  2. JWKS fetch (cached); resolve `kid` → JWK; enforce per-kid alg pinning
//     (JWT alg MUST equal JWK alg or kty-derived alg).
//  3. Convert JWK → crypto.PublicKey; verify signature via golang-jwt/jwt/v5.
//  4. Validate `iss` (exact match), `aud` (contains expected), `exp`/`nbf`/`iat`
//     with configurable clock-skew (default ±30s).
//  5. Extract custom claims: `acr`, `amr`, `auth_time`, `cnf` (jkt | x5t#S256),
//     `scope`, `ext_claims` (kacho_*). The enrichment map and the assurance
//     level are resolved across BOTH placements the provider produces — see
//     extClaimsMap / extractACR for why one placement is not enough.
//
// JWKS cache: in-memory, TTL configurable. Refresh is LAZY, on the request path
// (no background ticker / prewarm exists — do not read the word "background"
// into it), and has two distinct triggers: the snapshot went stale by time, and
// the signer named a `kid` the snapshot does not carry. The second one is what
// absorbs a mid-grace-window key rotation, and it deliberately ignores the TTL:
// a snapshot can be perfectly fresh and already incomplete. Its cost is bounded
// by forcedRefreshMinInterval so an unknown `kid` cannot become a load
// amplifier.
//
// Thread safety: methods are safe for concurrent use; cache uses sync.RWMutex.
package middleware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

// VerifiedToken — output of JWTVerifier.Verify. Carries all claims required by
// downstream middleware (DPoP binding check, step-up gate, principal
// injection).
//
// # ИСТОЧНИК НАЗВАН У КАЖДОГО ПОЛЯ — и это не косметика
//
// Поле, которое полоса не заполнила, делает нижележащий контроль ПРОЙДЕННЫМ
// МИМО, а не успешно, — и это неотличимо от исправной работы. Поэтому у
// каждого поля ниже названо, ОТКУДА берётся значение и бывает ли оно пустым
// законно (приёмка BAT-1 §5.2, гейт BAT-1-68).
//
// Источников у полосы Verify ЧЕТЫРЕ, и смешивать их нельзя:
//
//	предъявленная строка целиком            Raw;
//	JOSE-заголовок предъявленного токена    Kid, Alg — разбирается ДО проверки
//	                                        подписи, ключ иначе не выбрать;
//	утверждения ПРОВЕРЕННОГО тела           всё остальное содержательное;
//	запись объявленного издателя            ReadRevocation — наш справочник, а
//	                                        не утверждение предъявителя.
//
// Плюс вычисление самого проверяющего: Cnf.IsBearer — не утверждение токена, а
// вывод из ОТСУТСТВИЯ утверждения cnf.
//
// # ОДНО ПОЛЕ НАМЕРЕННО ОСТАВЛЕНО БЕЗ ИСТОЧНИКА, и это не пропуск
//
// NotBefore — утверждение nbf проверенного тела, как и его соседи по группе, но
// в групповом комментарии оно НЕ названо. Причина: его не читает никто, и
// самопроверка гейта BAT-1-68 держит его законным близнецом — доказательством,
// что источника не требуют у величины, которую никто не спрашивает
// (TestBAT1_68_InjectionUnreadFieldNeedsNoSource). Предикат гейта именной,
// поэтому назвать поле в ГРУППОВОМ комментарии значит пометить у него источник
// и разрушить близнеца; здесь, в шапке типа, имя предикатом не читается.
//
// Появится читатель — близнец самоистечёт красным, и тогда источник называется
// в группе выше, а не здесь.
//
// # ПРОИЗВОДИТЕЛЕЙ У НОСИТЕЛЯ ДВА, и второй заполняет НЕ ВСЁ
//
// Помимо Verify тот же тип собирает verifiedTokenFromCtxOrHTTP (authz_util.go) —
// тонкое восстановление уже проверенного удостоверения из собственной разметки края
// (x-kacho-заголовки либо одноимённые ключи metadata). Он заполняет РОВНО ПЯТЬ
// полей — Subject, JTI, ACR, Scope, ExtClaims, — а остальные оставляет нулевыми.
//
// Значения туда попадают из того же проверенного токена: проставляет их сам
// край после того, как подпись сошлась (auth.go, auth_stepup.go,
// dpop_http_middleware.go), а всё пространство имён x-kacho- снимается со
// входящего запроса ДО любой полосы (stripForgeableIdentityHeaders →
// principalmeta.IsClientForgeableKey). То есть это наш собственный разбор,
// переданный себе же, а не утверждение предъявителя.
//
// Различать два производителя ОБЯЗАТЕЛЬНО вот почему: на тонком
// носителе Issuer, ExpiresAt и Audience пусты ВСЕГДА, хотя у Verify первые два
// непусты by construction. Контроль, читающий их без оглядки на
// производителя, прочтёт «здесь всегда пусто» как факт о токене.
type VerifiedToken struct {
	Raw string // original compact JWT (for downstream introspection / audit)

	// Kid / Alg — из JOSE-ЗАГОЛОВКА предъявленного токена, а не из его тела.
	//
	// Заголовок разбирается ДО проверки подписи (шаг 1 Verify): выбрать ключ
	// иначе нечем. Поэтому оба значения ограничиваются ещё на разборе — Alg
	// обязан состоять в разрешённом наборе (tokenpolicy.AlgorithmAllowed) и
	// совпасть с алгоритмом, который объявил САМ ключ (jwk.AlgForJWT), Kid
	// обязан пройти keyIDWellFormed, — а в носитель попадают только после того,
	// как подпись сошлась. Пустыми не бывают: и AlgorithmAllowed, и
	// keyIDWellFormed отвергают пустую строку явно, а не по совпадению.
	Kid string
	Alg string

	// Subject / Issuer / Audience / IssuedAt / ExpiresAt / JTI — утверждения
	// ПРОВЕРЕННОГО тела (jwt.MapClaims после ParseWithClaims): sub / iss / aud /
	// iat / exp / jti соответственно.
	//
	// Заполнены не все и не всегда. Различие названо поимённо, потому что нулевое
	// значение читается нижележащим контролем как факт о токене:
	//
	//	Issuer    — непуст ВСЕГДА: разбор объявил iss обязательным и сверил его с
	//	            издателем ВЫБРАННОЙ записи (WithIssuer(rec.issuer)), поэтому
	//	            Issuer равен rec.issuer by construction;
	//	ExpiresAt — непуст ВСЕГДА: WithExpirationRequired;
	//	Audience  — нормализованный aud, одиночная строка либо список
	//	            (extractAudience). Пуст, когда утверждения aud нет вовсе, а
	//	            полоса его отсутствие допускает (allowMissingAudience);
	//	Subject / IssuedAt / JTI — утверждения НЕОБЯЗАТЕЛЬНЫЕ: утверждения нет
	//	            либо пришло не того типа — поле остаётся нулём, и это
	//	            «не заявлено», а не «заявлено пусто».
	//
	// Subject и JTI заполняет также второй производитель носителя — см. шапку типа.
	Subject   string
	Issuer    string
	Audience  []string
	IssuedAt  time.Time
	ExpiresAt time.Time
	NotBefore time.Time
	JTI       string

	// Authentication context — drives step-up gating.
	// ACR: "0" | "1" | "2" | "3"; "" when the token asserts no level, which the
	// gate ranks 0 and denies. Sourced by extractACR from the standard top-level
	// `acr` claim, falling back to `kacho_acr` in ExtClaims — always from inside
	// the signed token, never from a header ON THIS LANE (второй производитель
	// носителя читает ACR из разметки, которую край проставил себе сам, — см.
	// шапку типа).
	//
	// AMR / AuthTime — оттуда же, из утверждений проверенного тела: amr и
	// auth_time. AMR нормализуется stringSlice — одиночная строка читается как
	// список из одного элемента, элементы не-строки отбрасываются. Оба
	// утверждения НЕОБЯЗАТЕЛЬНЫЕ: их отсутствие даёт пустой список и нулевое
	// время, и страж повышения уровня обязан читать это как «не заявлено», а не
	// как «заявлено пусто». Второй производитель носителя не заполняет ни то, ни
	// другое.
	ACR      string
	AMR      []string
	AuthTime time.Time

	// Sender-constrained binding — exactly one of Jkt / X5tS256 may be set.
	// Empty → bearer token (legacy).
	Cnf TokenConfirmation

	// Scope — утверждение `scope` проверенного тела, строкой КАК ЕСТЬ, без
	// разбора на элементы. Утверждение НЕОБЯЗАТЕЛЬНОЕ: его нет либо пришло не
	// строкой — поле остаётся пустым. Заполняет также второй производитель
	// носителя (см. шапку типа).
	Scope string

	// ExtClaims — Kachō custom claims emitted by the iam token hook (kacho_*).
	// Resolved by extClaimsMap from either placement the provider uses (promoted
	// to the top level, or nested under its `ext` wrapper), so consumers never
	// have to know which deployment profile minted the token.
	ExtClaims map[string]any

	// Raw claims for callers that need fields we did not explicitly extract.
	Claims jwt.MapClaims

	// ReadRevocation — объявила ли ЗАПИСЬ этого издателя чтение отзыва на
	// предъявлении.
	//
	// Полоса выбирается по издателю, а не по настройке процесса: отзыв нашего
	// токена знает только НАШ авторитет — прежний провайдер о наших токенах не
	// знает by construction, и его ответ на наш токен есть утверждение о чужом
	// предмете, а не «действует» или «отозван».
	ReadRevocation bool
}

// TokenConfirmation — RFC 7800 §3 confirmation method. Either DPoP-bound
// (jkt) or mTLS-bound (x5t#S256). Mutually exclusive in practice.
type TokenConfirmation struct {
	Jkt      string // RFC 9449 §6.1 — DPoP JWK SHA-256 thumbprint (base64url)
	X5tS256  string // RFC 8705 §3 — client certificate SHA-256 thumbprint
	HasJkt   bool
	HasX5tS  bool
	IsBearer bool // no cnf claim present → plain bearer token
}

// JWTVerifier — RFC 8725-hardened access-token validator.
//
// Издатель здесь — МНОЖЕСТВО объявленных записей, а не скаляр: край принимает
// нашу чеканку наравне с прежним издателем, и у каждого свой набор
// проверочных ключей (token_acceptance.go).
type JWTVerifier struct {
	records          map[string]*issuerRecord
	expectedAudience string
	clockSkew        time.Duration

	// allowMissingAudience — for tests / dev mode where the provider may not
	// yet inject the gateway audience.
	allowMissingAudience bool

	// now — источник времени. ВХОД, а не окружение: без этого проба допуска на
	// расхождение часов недетерминирована, то есть не может упасть предсказуемо
	// и держится широкими допусками вместо утверждения. Ровно та же причина и
	// та же форма, что у первой конфигурации проверяющего.
	now func() time.Time
}

// JWTVerifierConfig — construction parameters.
type JWTVerifierConfig struct {
	// Issuers — ОБЪЯВЛЕННЫЕ записи приёма: по одной на каждого принимаемого
	// издателя. Обязательны: пустой перечень означает «принимаем любого».
	Issuers []IssuerKeySet

	JWKSCacheTTL         time.Duration
	JWKSFetchTimeout     time.Duration
	HTTPClient           *http.Client // optional; nil → default
	ExpectedAudience     string
	ClockSkew            time.Duration
	AllowMissingAudience bool

	// Clock подменяет источник времени. nil → системное время.
	Clock func() time.Time
}

// NewJWTVerifier constructs a verifier over the declared acceptance records.
//
// Отказ вместо построения на всяком состоянии, которое при пустом значении
// означает «не сужаем»: записей нет · пустой издатель, адрес или набор типов ·
// издатель объявлен дважды · неабсолютный адрес · тип доказательства владения
// объявлен принимаемым.
func NewJWTVerifier(cfg JWTVerifierConfig) (*JWTVerifier, error) {
	records, err := normaliseIssuerKeySets(cfg.Issuers)
	if err != nil {
		return nil, err
	}
	if cfg.JWKSCacheTTL <= 0 {
		cfg.JWKSCacheTTL = 5 * time.Minute
	}
	// Потолок срока снимка объявлен политикой платформы, а не выбран здесь: он
	// второе слагаемое отсрочки снятия подписного ключа, и слагаемое, известное
	// только одной стороне, делает арифметику неисчислимой.
	if cfg.JWKSCacheTTL > tokenpolicy.ConsumerKeySetCacheCeiling {
		cfg.JWKSCacheTTL = tokenpolicy.ConsumerKeySetCacheCeiling
	}
	if cfg.JWKSFetchTimeout <= 0 {
		cfg.JWKSFetchTimeout = 5 * time.Second
	}
	if cfg.ClockSkew <= 0 {
		cfg.ClockSkew = tokenpolicy.ClockSkew
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.JWKSFetchTimeout}
	}
	for _, rec := range records {
		rec.jwks = NewJWKSCache(rec.keySetURL, cfg.JWKSCacheTTL, httpClient)
	}
	now := cfg.Clock
	if now == nil {
		now = time.Now
	}
	return &JWTVerifier{
		records:              records,
		expectedAudience:     cfg.ExpectedAudience,
		clockSkew:            cfg.ClockSkew,
		allowMissingAudience: cfg.AllowMissingAudience,
		now:                  now,
	}, nil
}

// Issuers возвращает объявленных принимаемых издателей. Порядок не значим —
// это НАБОР, а не последовательность: запись выбирается точным равенством
// объявленного токеном издателя, никогда перебором.
func (v *JWTVerifier) Issuers() []string {
	out := make([]string, 0, len(v.records))
	for iss := range v.records {
		out = append(out, iss)
	}
	sort.Strings(out)
	return out
}

// ReadsRevocationFor отвечает, объявлено ли чтение отзыва на предъявлении для
// токенов этого издателя.
func (v *JWTVerifier) ReadsRevocationFor(issuer string) bool {
	rec, ok := v.records[issuer]
	return ok && rec.readRevocation
}

// JWKSCache — thread-safe TTL cache for a single JWKS endpoint. Refreshes on
// miss or stale; force-refresh on unknown `kid` (TTL not consulted) to absorb
// mid-grace-window rotations, rate-limited by forcedRefreshMinInterval.
type JWKSCache struct {
	url        string
	ttl        time.Duration
	httpClient *http.Client

	// fetchMu single-flights the HTTP fetch WITHOUT holding mu across the
	// blocking round-trip, so a slow JWKS endpoint never stalls concurrent
	// token verifications (they keep taking mu.RLock while a fetch is in flight).
	fetchMu sync.Mutex

	mu        sync.RWMutex
	set       *JWKSet
	fetchedAt time.Time
	// forcedAt — время последней ПОПЫТКИ вынужденного перезапроса (повод —
	// неизвестный kid). Отмечается до сетевого вызова, а не после успеха: иначе
	// неотвечающий источник ключей опрашивался бы на каждый запрос.
	forcedAt time.Time
}

// forcedRefreshMinInterval — минимальный интервал между ВЫНУЖДЕННЫМИ
// перезапросами набора ключей (повод — неизвестный kid, а не истёкший TTL).
//
// Он существует для того, чтобы неизвестный kid не стал усилителем нагрузки:
// поток подделок с произвольными kid иначе означал бы обращение к источнику
// ключей на каждый запрос. Интервал НАМЕРЕННО на порядки короче TTL кэша —
// смысл вынужденного перезапроса именно в том, чтобы не ждать TTL, — и это
// соотношение закреплено пробой предпосылки.
const forcedRefreshMinInterval = 3 * time.Second

// keySourceUnavailableReason — что говорят вызывающему, когда набор проверочных
// ключей добыть не удалось. Текст постоянный и не несёт ни адреса источника, ни
// причины отказа: тон отказа authN на крае стабилен намеренно, а разбор идёт в
// журнал.
const keySourceUnavailableReason = "token key source unavailable"

// keySourceUnanswerable отвечает на вопрос, который до задачи #1194 не задавался
// вовсе: ЧЬЯ это ошибка — предъявителя или наша.
//
// Проверка подписи возвращает один тип ошибки на два разных исхода, и склеивать
// их нельзя:
//
//   - токен НЕГОДЕН (подпись не сходится, срок вышел, издатель чужой, `kid`
//     отсутствует в ЖИВОМ наборе) — исход терминальный, повтор бессмыслен,
//     вызывающему нужен новый токен;
//   - НАБОР КЛЮЧЕЙ НЕ ДОБЫТ (источник не ответил либо ответил не набором) —
//     исход временный, повтор осмыслен, а удостоверение предъявителя тут ни при
//     чём.
//
// Ответив на второе кодом первого, край велит КАЖДОМУ клиенту выбросить годный
// токен и пройти вход заново — то есть добавляет нагрузку ровно на тот
// компонент, который лежит.
//
// ГРАНИЦА ПРОВЕДЕНА ПО ТОМУ, ОТВЕТИЛ ЛИ ИСТОЧНИК, а не по тому, нашёлся ли ключ.
// `ErrKeyNotFound` сюда НЕ входит: набор получен, и подписант назвал ключ,
// которого мы не публикуем, — это негодный токен. Иначе подделка с произвольным
// `kid` получала бы «повтори позже».
//
// `ErrJWKSFetchFailed` покрывает и ответ не по адресу (не 200, не JSON, пустой
// набор). Для ВЫЗЫВАЮЩЕГО это тот же исход — «не твоя вина», — а различать
// сбой и неверную настройку обязан журнал (`security.md` §Hardening инв. 8),
// поэтому вызывающие пишут эту ветвь громко.
func keySourceUnanswerable(err error) bool {
	return errors.Is(err, ErrJWKSUnreachable) || errors.Is(err, ErrJWKSFetchFailed)
}

// NewJWKSCache constructs a cache; first fetch is lazy on Get.
func NewJWKSCache(url string, ttl time.Duration, httpClient *http.Client) *JWKSCache {
	return &JWKSCache{url: url, ttl: ttl, httpClient: httpClient}
}

// FetchedAt returns the timestamp of the most recent successful fetch.
// Exported for tests / observability.
func (c *JWKSCache) FetchedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fetchedAt
}

// Resolve looks up a JWK by kid, refreshing the cache when stale or when the
// kid is unknown. Returns ErrKeyNotFound after a force-refresh if kid still
// unknown.
//
// Два повода перезапроса РАЗНЫЕ и обрабатываются разными ветками. «Снимок
// устарел по времени» — обычный перезапрос. «Подписант назвал ключ, которого в
// снимке нет» — ВЫНУЖДЕННЫЙ: снимок при этом может быть совершенно свежим и всё
// равно неполным, что и происходит при ротации подписного ключа у провайдера.
// Если бы вынужденный перезапрос проходил через предикат свежести, ротация
// оборачивалась бы отказом всех НОВОВЫДАННЫХ токенов до истечения TTL.
func (c *JWKSCache) Resolve(ctx context.Context, kid string) (*JWK, error) {
	c.mu.RLock()
	stale := c.set == nil || time.Since(c.fetchedAt) > c.ttl
	forced := false
	if !stale && c.set != nil {
		k, err := c.set.FindByKid(kid)
		c.mu.RUnlock()
		if err == nil {
			return k, nil
		}
		// Свежий снимок, но kid в нём отсутствует → вынужденный перезапрос.
		forced = true
	} else {
		c.mu.RUnlock()
	}

	if err := c.refresh(ctx, forced); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.set == nil {
		return nil, ErrJWKSFetchFailed
	}
	return c.set.FindByKid(kid)
}

// refresh перезапрашивает набор ключей. forced=true означает «повод — не время,
// а неизвестный kid»: предикат свежести такому вызывающему не применяется, но
// вместо него действует собственный минимальный интервал
// (forcedRefreshMinInterval), чтобы поток неизвестных kid не превратился в
// усилитель нагрузки на источник ключей.
func (c *JWKSCache) refresh(ctx context.Context, forced bool) error {
	// Serialize fetches on fetchMu (single-flight) but do NOT hold the RWMutex
	// across the network I/O below — mu is taken only for the short double-check
	// read and the final publish. This bounds the critical section on mu to a
	// map assignment so concurrent verifications keep resolving during a fetch.
	c.fetchMu.Lock()
	defer c.fetchMu.Unlock()
	// Double-check: another goroutine may have refreshed while we waited on fetchMu.
	c.mu.RLock()
	now := time.Now()
	fresh := c.set != nil && now.Sub(c.fetchedAt) < c.ttl
	forcedRecently := !c.forcedAt.IsZero() && now.Sub(c.forcedAt) < forcedRefreshMinInterval
	c.mu.RUnlock()
	if forced {
		// Уже сходили за набором только что (в том числе — соседняя горутина,
		// пока мы ждали на fetchMu): её результат уже опубликован, повторное
		// обращение ничего не добавит. Вызывающий перечитает снимок сам.
		if forcedRecently {
			return nil
		}
		c.mu.Lock()
		c.forcedAt = now
		c.mu.Unlock()
	} else if fresh {
		return nil
	}
	// c.url is the operator-configured JWKS endpoint (KACHO_HYDRA_JWKS_URL /
	// derived from KACHO_API_DOMAIN), never request-derived — not an SSRF sink.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil) // #nosec G704 -- JWKS URL is operator config, not user input
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrJWKSFetchFailed, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req) // #nosec G704 -- JWKS URL is operator config, not user input
	if err != nil {
		return fmt.Errorf("%w: %v", ErrJWKSUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: status=%d body=%q", ErrJWKSFetchFailed, resp.StatusCode, string(body))
	}
	// Cap body to prevent DoS via massive JWKS document.
	limited := io.LimitReader(resp.Body, 1<<20) // 1 MiB
	var set JWKSet
	if err := json.NewDecoder(limited).Decode(&set); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrJWKSFetchFailed, err)
	}
	if len(set.Keys) == 0 {
		return fmt.Errorf("%w: empty key set", ErrJWKSFetchFailed)
	}
	// Publish under the write lock — bounded to field assignment, no I/O.
	c.mu.Lock()
	c.set = &set
	c.fetchedAt = time.Now()
	c.mu.Unlock()
	return nil
}

// Verify parses and validates the access token. Returns sentinel-typed errors
// callers can map to `invalid_token` / `insufficient_user_authentication`
// WWW-Authenticate challenges.
func (v *JWTVerifier) Verify(ctx context.Context, token string) (*VerifiedToken, error) {
	if token == "" {
		return nil, errors.New("empty token")
	}

	// 1. Parse header without verifying signature — we need kid + alg to
	//    select the key.
	header, err := splitJWT(token)
	if err != nil {
		return nil, fmt.Errorf("invalid jwt structure: %w", err)
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
		// Crit — параметры, которые ОТПРАВИТЕЛЬ пометил обязательными к пониманию
		// (RFC 7515 §4.1.11). Не читая его, край исполнял бы условие, которого не
		// понял, — то есть принимал токен на непроверенном основании.
		Crit []string `json:"crit"`
	}
	if jerr := json.Unmarshal(header, &hdr); jerr != nil {
		return nil, fmt.Errorf("invalid jwt header: %w", jerr)
	}
	if !tokenpolicy.AlgorithmAllowed(hdr.Alg) {
		return nil, fmt.Errorf("%w: alg=%q", ErrUnsupportedAlg, hdr.Alg)
	}
	// Идентификатор ключа — НЕДОВЕРЕННЫЙ вход, и его форма ограничивается ДО
	// того, как он попадёт в поиск по снимку, в повод вынужденного перезапроса
	// и в журнал.
	if !keyIDWellFormed(hdr.Kid) {
		return nil, ErrMalformedKeyID
	}
	// Помеченное обязательным — исполнить или отвергнуть токен целиком. Обратная
	// сторона того же требования: НЕ помеченное неизвестное игнорируется
	// (RFC 7519, EID 8060) — на этом держится совместимость, поэтому разбор
	// неизвестные поля молча пропускает и здесь, и в утверждениях.
	if ok, name := tokenpolicy.CriticalHeadersUnderstood(hdr.Crit); !ok {
		return nil, fmt.Errorf("%w: crit=%q", ErrUnknownCriticalHeader, name)
	}

	// 2. Издатель — тоже НЕДОВЕРЕННЫЙ вход, и здесь он служит ИСКЛЮЧИТЕЛЬНО
	//    ключом поиска в таблице объявленных записей. Издателя, для которого
	//    записи нет, не бывает: он даёт отказ, а не перебор записей подряд и не
	//    адрес, выведенный из него самого.
	//
	//    Разбор утверждений ДО проверки подписи здесь неизбежен — выбрать
	//    ключ иначе нечем, — поэтому ничто из разобранного не используется ни
	//    для чего, кроме выбора записи, пока подпись не сошлась.
	unverified, err := unverifiedIssuer(token)
	if err != nil {
		return nil, fmt.Errorf("invalid jwt claims: %w", err)
	}
	rec, ok := v.records[unverified]
	if !ok {
		// Издатель в текст не уносится: он пришёл от предъявителя.
		return nil, ErrNoIssuerRecord
	}
	// Тип равен ожидаемому для ЭТОЙ полосы. ОТСУТСТВИЕ и НЕСОВПАДЕНИЕ — разные
	// вещи, и различает их только эта ветка.
	if !rec.acceptsTokenType(hdr.Typ) {
		return nil, ErrUnexpectedTokenType
	}

	// 3. Resolve key from the record's OWN key set.
	jwk, err := rec.jwks.Resolve(ctx, hdr.Kid)
	if err != nil {
		return nil, fmt.Errorf("jwks resolve kid=%q: %w", hdr.Kid, err)
	}
	if expected := jwk.AlgForJWT(); expected != "" && expected != hdr.Alg {
		return nil, fmt.Errorf("%w: jwk_alg=%q jwt_alg=%q", ErrAlgMismatch, expected, hdr.Alg)
	}
	pubKey, err := jwk.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("jwk to public key: %w", err)
	}

	// 4. Parse + verify via golang-jwt; supply our pinned key.
	//
	//    Срок ОБЯЗАТЕЛЕН, и это включено ЯВНО: разбор, встретив срок, его
	//    проверит, а не встретив — не возразит. Токен без срока живёт вечно, и
	//    заметить это на положительном пути нельзя.
	//
	//    Издатель сверяется ТОЙ ЖЕ библиотекой, что и подпись, и сверяется с
	//    издателем ВЫБРАННОЙ записи: разобранное до проверки подписи значение
	//    выбрало запись и на этом свою роль исчерпало.
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{hdr.Alg}),
		jwt.WithLeeway(v.clockSkew),
		jwt.WithIssuedAt(),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(rec.issuer),
		jwt.WithTimeFunc(v.now),
	)
	claims := jwt.MapClaims{}
	parsed, err := parser.ParseWithClaims(token, claims, func(_ *jwt.Token) (any, error) {
		return pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("jwt verify: %w", err)
	}
	if !parsed.Valid {
		return nil, errors.New("jwt invalid")
	}

	// 5. Validate aud (golang-jwt checked exp/nbf/iat with leeway and iss above).
	iss, _ := claims["iss"].(string)
	auds, err := extractAudience(claims)
	if err != nil {
		return nil, err
	}
	if v.expectedAudience != "" {
		if !audienceContains(auds, v.expectedAudience) {
			if !v.allowMissingAudience {
				return nil, fmt.Errorf("aud does not contain %q (got %v)", v.expectedAudience, auds)
			}
		}
	}

	out := &VerifiedToken{
		Raw:            token,
		Kid:            hdr.Kid,
		Alg:            hdr.Alg,
		Issuer:         iss,
		Audience:       auds,
		Claims:         claims,
		ReadRevocation: rec.readRevocation,
	}

	if sub, ok := claims["sub"].(string); ok {
		out.Subject = sub
	}
	if jti, ok := claims["jti"].(string); ok {
		out.JTI = jti
	}
	if iat, ok := numericTime(claims["iat"]); ok {
		out.IssuedAt = iat
	}
	if exp, ok := numericTime(claims["exp"]); ok {
		out.ExpiresAt = exp
	}
	if nbf, ok := numericTime(claims["nbf"]); ok {
		out.NotBefore = nbf
	}
	if at, ok := numericTime(claims["auth_time"]); ok {
		out.AuthTime = at
	}
	out.ExtClaims = extClaimsMap(claims)
	out.ACR = extractACR(claims, out.ExtClaims)
	out.AMR = stringSlice(claims["amr"])
	if scope, ok := claims["scope"].(string); ok {
		out.Scope = scope
	}

	// Cnf extraction.
	if cnfRaw, ok := claims["cnf"].(map[string]any); ok {
		if jkt, ok := cnfRaw["jkt"].(string); ok && jkt != "" {
			out.Cnf.Jkt = jkt
			out.Cnf.HasJkt = true
		}
		if x5t, ok := cnfRaw["x5t#S256"].(string); ok && x5t != "" {
			out.Cnf.X5tS256 = x5t
			out.Cnf.HasX5tS = true
		}
	}
	if !out.Cnf.HasJkt && !out.Cnf.HasX5tS {
		out.Cnf.IsBearer = true
	}

	return out, nil
}

// splitJWT decodes the header segment of a compact JWS and validates that the
// payload + signature segments are well-formed base64url. Only the header bytes
// are returned: callers need it to select the key (kid/alg), while the payload
// claims and signature are decoded+verified by the jwt library (JWT path) or the
// DPoP proof verifier. Returning parallel payload/sig copies here would only
// invite a decode/enforcement mismatch, so they are validated then dropped.
func splitJWT(token string) (header []byte, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("jwt must have 3 parts, got %d", len(parts))
	}
	if header, err = base64.RawURLEncoding.DecodeString(parts[0]); err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	if _, err = base64.RawURLEncoding.DecodeString(parts[1]); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	if _, err = base64.RawURLEncoding.DecodeString(parts[2]); err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	return header, nil
}

// extClaimsMap resolves the Kachō enrichment map (the `kacho_*` claims stamped
// by the iam token hook) from a verified claim set, accepting BOTH placements
// the provider produces. There is exactly one resolution point on purpose:
// every reader of these claims — principal type/id, the subject extractor, the
// FGA context extractor, the step-up level below — must see the same map, or a
// claim becomes visible to some consumers and invisible to others depending on
// the deployment profile.
//
// The two placements are not alternatives we chose; they are what
// `oauth2.allowed_top_level_claims` does:
//
//   - listed  → the provider mirrors the map to the TOP level (`ext_claims`),
//     which is the dev profile (deploy/helm/umbrella/values.dev.yaml);
//   - not listed → the map stays inside the provider's own `ext` wrapper
//     (`ext.ext_claims`). That is the prod profile, which lists nothing at all,
//     and it is the same nested shape the registry data-plane verifier decodes.
//
// Same signed map either way. Reading only one placement means being blind on
// one of the two profiles — and the blind one is production.
func extClaimsMap(claims jwt.MapClaims) map[string]any {
	if m, ok := claims["ext_claims"].(map[string]any); ok {
		return m
	}
	if ext, ok := claims["ext"].(map[string]any); ok {
		if m, ok := ext["ext_claims"].(map[string]any); ok {
			return m
		}
	}
	return nil
}

// extractACR resolves the authentication assurance level that the step-up gate
// ranks (grpcsrv.ACRRank / EvaluateStepUp).
//
// PRECEDENCE — standard claim first, enrichment map second:
//
//  1. top-level `acr`, the standard OIDC claim. When the provider states the
//     level itself, that statement is authoritative and nothing overrides it.
//  2. `kacho_acr` in the enrichment map — where OUR OWN token hook puts the
//     level (services/iam token enrichment, carried in
//     `session.access_token.ext_claims`).
//
// Step 2 is not a nicety. The provider promotes to the top level only the claim
// names whitelisted in `oauth2.allowed_top_level_claims`, and neither `acr` nor
// `kacho_acr` is on that list on any deployed profile — so without this fallback
// the level a human actually authenticated with is present in the signed token,
// arrives at the edge, and is discarded before the gate sees it. The gate then
// ranks every human at 0 and refuses every RPC that declares a floor. Machine
// principals never surfaced it: they are exempted from the floor before any
// comparison happens.
//
// FAIL-CLOSED, and it must stay that way: the value is read ONLY from the
// verified, signed claim set. No header, no metadata, no request-supplied input
// may ever feed it — a caller who could name their own assurance level would
// satisfy any floor by asking. A token asserting no level anywhere yields "",
// which ranks 0 and is denied.
//
// An empty top-level `acr` is treated as ABSENT rather than as an assertion of
// level 0: an empty string states nothing, and letting it shadow the enrichment
// map would resurrect exactly the drop this function exists to prevent.
func extractACR(claims jwt.MapClaims, ext map[string]any) string {
	if acr, ok := claims["acr"].(string); ok && acr != "" {
		return acr
	}
	if acr, ok := ext["kacho_acr"].(string); ok {
		return acr
	}
	return ""
}

func extractAudience(claims jwt.MapClaims) ([]string, error) {
	v, ok := claims["aud"]
	if !ok {
		return nil, nil
	}
	switch t := v.(type) {
	case string:
		return []string{t}, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("aud entry is not string: %T", e)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("aud has unexpected type %T", v)
	}
}

func audienceContains(auds []string, want string) bool {
	for _, a := range auds {
		if a == want {
			return true
		}
	}
	return false
}

func numericTime(v any) (time.Time, bool) {
	switch n := v.(type) {
	case float64:
		return time.Unix(int64(n), 0), true
	case int64:
		return time.Unix(n, 0), true
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return time.Unix(i, 0), true
		}
	}
	return time.Time{}, false
}

func stringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), t...)
	case string:
		return []string{t}
	}
	return nil
}

// unverifiedIssuer достаёт объявленный токеном `iss` ДО проверки подписи.
//
// Разбор до проверки подписи здесь неизбежен: выбрать ключ иначе нечем. Поэтому
// у значения ровно одна роль — ключ поиска в таблице объявленных записей, — и
// оно не участвует ни в построении адреса, ни в ключе кэша, ни в тексте,
// уходящем наружу. Ни одно другое утверждение отсюда не читается.
func unverifiedIssuer(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("jwt must have 3 parts, got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}
	if len(payload) > tokenpolicy.KeySetBodyCeiling {
		return "", errors.New("payload exceeds the declared ceiling")
	}
	var claims struct {
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("decode claims: %w", err)
	}
	return claims.Iss, nil
}
