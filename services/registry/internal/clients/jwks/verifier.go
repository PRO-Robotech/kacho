// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package jwks — проверяющий identity-JWT плоскости данных реестра.
//
// # Что он проверяет
//
// Состав обязательных проверок объявлен ОДИН раз — в `pkg/tokenpolicy`, — и эта
// реализация ОБЪЯВЛЯЕТ, какие из них исполняет (`DeclaredChecks`). Пока состав
// живёт у каждой поверхности свой, различие между поверхностями не выражено и
// потому не может покраснеть; объявление делает его предметом гейта по дереву.
//
// # Издатель — МНОЖЕСТВО, и у каждого СВОЯ запись источника ключей
//
// Платформа чеканит свои токены сама, а прежний издатель на переходе остаётся.
// Принять двух издателей, имея один набор ключей, значило бы разрешить ключу
// одного проверять токен другого — то есть отменить ту самую защиту, ради
// которой развязка и делается. Поэтому у каждого принимаемого издателя своя
// объявленная запись: свой адрес набора, свой снимок, свой срок годности.
//
// Адрес записи ОБЪЯВЛЯЕТСЯ перечислением и НИКОГДА не выводится из издателя.
// Издатель приходит от предъявителя — это недоверенный вход; кроме прямого
// вреда (значение от предъявителя управляет тем, куда мы ходим), производный
// адрес получался бы у ВСЯКОГО издателя, и состояние «записи нет» не наступало
// бы никогда: страж старта остался бы в тексте, не имея возможности упасть.
//
// # Отзыв читается НА ПРЕДЪЯВЛЕНИИ
//
// Контроль, действующий только в местах выдачи удостоверения, отзывом не
// является: он лишь не выдаёт нового. Поэтому запись нашей чеканки несёт
// читателя авторитета отзыва на пути запроса; любой неопознанный исход
// авторитета — отказ (см. revocation.go).
//
// # Отказ при сомнении
//
// Слишком строгая проверка даёт отказ, видимый сразу. Слишком слабая даёт
// принимаемый чужой токен, не видимый никогда: успешная проверка выглядит
// одинаково независимо от того, что именно она проверила. Поэтому на каждой
// развилке выбран отказ.
//
// Реализует порт dataplane.TokenVerifier (структурно: Verify(ctx,string)(string,error)).
package jwks

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

// ErrInvalidToken — обобщённая ошибка проверки. Плоскость данных отвечает на неё
// 401 error="invalid_token", не раскрывая причину предъявителю.
//
// Текст ошибки НЕ несёт значений, пришедших от предъявителя (идентификатор
// ключа, объявленный издатель, алгоритм): он уходит в журнал и в диагностику,
// то есть покидает процесс.
var ErrInvalidToken = errors.New("jwks: invalid token")

// defaultTTL — срок годности снимка набора, когда источник его не назвал.
const defaultTTL = 5 * time.Minute

// maxTTL — потолок срока годности снимка. Он НЕ объявляется здесь: это второе
// слагаемое арифметики отсрочки снятия ключа, и пока каждая сторона объявляет
// своё число, отсрочка не вычисляется, а угадывается. Источник один —
// `pkg/tokenpolicy`.
const maxTTL = tokenpolicy.ConsumerKeySetCacheCeiling

// defaultMinRefresh — собственный минимальный интервал ВЫНУЖДЕННОГО перезапроса
// набора по неизвестному идентификатору ключа. Значение — из объявленной
// политики: идентификатор читается из заголовка ДО проверки подписи, поэтому без
// интервала поток выдуманных идентификаторов превращается в поток обращений к
// публикатору.
const defaultMinRefresh = tokenpolicy.UnknownKeyIDRefetchInterval

// staleServeAttempts — во сколько окон интервала укладывается отсрочка
// обслуживания по ПРОТУХШЕМУ снимку (граница = staleServeAttempts × minRefresh
// сверх срока годности).
//
// Отсрочка нужна: перезапрос ограничен интервалом, поэтому один сетевой сбой
// иначе превратился бы в окно полного отказа проверки для законных токенов. Но
// окно интервала возобновляется на каждой попытке, поэтому «отдаём из снимка,
// пока окно активно» без АБСОЛЮТНОЙ границы означает: при постоянно недоступном
// источнике снятый ключ принимается бесконечно. Граница отсчитывается от
// последнего УСПЕШНОГО обращения — единственного момента, когда ключ был
// подтверждён источником, — и потому неудачами не продлевается.
const staleServeAttempts = 3

// maxKeyIDLen — потолок длины идентификатора ключа. Значение приходит от
// предъявителя, поэтому его форма ограничивается ДО использования: иначе
// произвольная строка доходит до поиска, до журнала и до счётчиков.
//
// Число ВЫВОДИТСЯ из политики платформы, а не выписывается здесь: мест
// исполнения этой формы три — чеканка и две конфигурации приёма, — и разойтись
// им можно только молча (Ф1б, задача #926).
const maxKeyIDLen = tokenpolicy.KeyIDMaxLen

// KeySetSource — ОБЪЯВЛЕННАЯ запись «издатель → источник его набора ключей».
//
// Записи задаются перечислением. Издателя, для которого записи нет, не бывает:
// он даёт отказ, а не перебор записей подряд и не адрес, выведенный из него
// самого.
type KeySetSource struct {
	// Issuer — точное значение `iss`, которое обязан объявить токен. Служит
	// ТОЛЬКО ключом поиска в этой таблице: ни частью адреса, ни частью имени,
	// ни частью ключа кэша.
	Issuer string
	// URL — объявленный адрес набора проверочных ключей этого издателя.
	URL string
	// TokenType — ожидаемое значение `typ`. Тип обязателен: разбор, не
	// встретив типа, сам не возразит, а один подписант, обслуживающий два
	// контура, делает путаницу типов настоящей возможностью.
	TokenType string
	// TolerateAbsentTokenType — принимать токен ЭТОЙ записи, если заголовок
	// типа не несёт вовсе. НЕСОВПАДАЮЩИЙ тип отвергается всё равно.
	//
	// Послабление выдано ровно одной полосе — прежнего издателя — и по
	// названной причине: его токены чеканим не мы, форму заголовка диктует он,
	// и потребовать от неё того, чего мы у него не проверяли, значило бы
	// поставить работу живого контура на непроверенное допущение о третьей
	// стороне. Цена ошибки здесь несимметрична обычной: лишняя строгость на
	// ЭТОЙ полосе — не видимый отказ одного запроса, а отказ КАЖДОГО, и
	// защиты она не добавляет — подпись, издатель, адресат и привязка ключа
	// уже отвергли бы чужой токен.
	//
	// На НАШЕЙ полосе послабления нет и быть не может: производитель типа —
	// мы сами, и отсутствие типа означало бы, что мы не выпускаем того, что
	// требуем.
	//
	// ПРЕДИКАТ СНЯТИЯ: послабление уходит вместе с записью прежнего издателя.
	// Запись, которой больше нечего зеркалить, — находка, и вместе с ней
	// находкой становится это поле.
	TolerateAbsentTokenType bool
	// ReadRevocation — спрашивать авторитет отзыва на предъявлении токена
	// этого издателя. Полоса прежнего издателя своего поведения не меняет.
	ReadRevocation bool
}

// Option — настройка проверяющего сверх обязательных записей.
type Option func(*Verifier)

// WithRevocationReader задаёт читателя авторитета отзыва. Обязателен, если хотя
// бы одна запись объявила чтение отзыва: объявленный контроль без читателя —
// мёртвый контроль, и он не отказал бы ни разу за всю свою жизнь.
func WithRevocationReader(r RevocationReader) Option {
	return func(v *Verifier) { v.revoke = r }
}

// WithHTTPClient подменяет клиента обращений к источникам наборов.
func WithHTTPClient(c *http.Client) Option {
	return func(v *Verifier) {
		if c != nil {
			v.http = c
		}
	}
}

// WithClock подменяет источник времени. Часы — вход, а не окружение: без этого
// пробы допуска на расхождение часов недетерминированы, то есть не могут упасть
// предсказуемо.
func WithClock(now func() time.Time) Option {
	return func(v *Verifier) {
		if now != nil {
			v.now = now
		}
	}
}

// issuerRecord — состояние ОДНОЙ записи источника: свой снимок, свой срок
// годности, своё окно перезапроса. Раздельность здесь не про производительность:
// общий снимок означал бы, что ключ одного издателя проверяет токен другого.
type issuerRecord struct {
	issuer         string
	url            string
	tokenType      string
	tolerateNoTyp  bool
	readRevocation bool

	mu          sync.Mutex
	keys        map[string]keyRecord
	ttl         time.Duration
	fetched     time.Time // последнее УСПЕШНОЕ обращение (база срока годности)
	lastRefresh time.Time // последняя ПОПЫТКА (база интервала, включая неудачные)
}

// keyRecord — проверочный ключ вместе с алгоритмом, ЗАКРЕПЛЁННЫМ за ним
// источником. Заголовок токена алгоритм не выбирает: иначе предъявитель сам
// назначает, как проверять его подпись.
type keyRecord struct {
	pub crypto.PublicKey
	alg string // пусто → источник алгоритм не объявил, остаётся сверка по виду ключа
}

// Verifier — потокобезопасный проверяющий identity-JWT.
type Verifier struct {
	aud     string
	records map[string]*issuerRecord
	revoke  RevocationReader
	skew    time.Duration
	http    *http.Client
	now     func() time.Time
}

// New строит проверяющего по ОБЪЯВЛЕННЫМ записям источников.
//
// Отказ вместо старта, если: записей нет · у записи пуст издатель, адрес или тип
// токена · издатель объявлен дважды · не задан ожидаемый адресат · запись
// объявила чтение отзыва, а читателя нет. Каждое из этих состояний при пустом
// значении означает «не сужаем», а «не сужаем» на проверке подлинности — это
// «принимаем любого».
func New(sources []KeySetSource, aud string, opts ...Option) (*Verifier, error) {
	if strings.TrimSpace(aud) == "" {
		return nil, errors.New("jwks: expected audience is required (an unset audience means «any audience»)")
	}
	if len(sources) == 0 {
		return nil, errors.New("jwks: at least one declared issuer key-set source is required")
	}

	v := &Verifier{
		aud:     aud,
		records: make(map[string]*issuerRecord, len(sources)),
		skew:    tokenpolicy.ClockSkew,
		http:    &http.Client{Timeout: 10 * time.Second},
		now:     time.Now,
	}
	for _, s := range sources {
		issuer := strings.TrimSpace(s.Issuer)
		if issuer == "" {
			return nil, errors.New("jwks: key-set source with an empty issuer")
		}
		if strings.TrimSpace(s.URL) == "" {
			return nil, fmt.Errorf("jwks: issuer %q has a key-set source with an empty URL", issuer)
		}
		if strings.TrimSpace(s.TokenType) == "" {
			return nil, fmt.Errorf("jwks: issuer %q declares no expected token type "+
				"(an unset type means «any type»)", issuer)
		}
		if _, dup := v.records[issuer]; dup {
			return nil, fmt.Errorf("jwks: issuer %q is declared twice — one issuer, one key-set record", issuer)
		}
		v.records[issuer] = &issuerRecord{
			issuer:         issuer,
			url:            strings.TrimSpace(s.URL),
			tokenType:      strings.TrimSpace(s.TokenType),
			tolerateNoTyp:  s.TolerateAbsentTokenType,
			readRevocation: s.ReadRevocation,
			keys:           map[string]keyRecord{},
			ttl:            defaultTTL,
		}
	}
	for _, o := range opts {
		o(v)
	}
	for _, rec := range v.records {
		if rec.readRevocation && v.revoke == nil {
			return nil, fmt.Errorf("jwks: issuer %q declares revocation on presentation but no revocation "+
				"reader is wired — a declared control without a reader never refuses", rec.issuer)
		}
	}
	return v, nil
}

// jwtHeader — разбираемая часть заголовка JOSE.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
	// Crit — параметры, которые ОТПРАВИТЕЛЬ пометил обязательными к пониманию
	// (RFC 7515 §4.1.11). Не читая его, принимающий исполняет условие, которого
	// не понял, — то есть принимает токен на основании, которого не проверил.
	Crit []string `json:"crit"`
}

// jwtClaims — энфорсимые утверждения токена. `aud` допускает строку или массив
// (RFC 7519) — разбирается собственным типом audience.
type jwtClaims struct {
	Sub string   `json:"sub"`
	Iss string   `json:"iss"`
	Aud audience `json:"aud"`
	Iat int64    `json:"iat"`
	Nbf int64    `json:"nbf"`
	Exp int64    `json:"exp"`
	// Ext — обёртка обогащения прежнего издателя: там `sub` несёт
	// идентификатор клиента, а принципал Kachō штампуется отдельным
	// утверждением.
	Ext struct {
		ExtClaims struct {
			KachoPrincipalID string `json:"kaname_principal_id"`
		} `json:"ext_claims"`
	} `json:"ext"`
}

// Verify проверяет предъявленный компактный JWS и возвращает принципала.
//
// Порядок не косметический:
//
//  1. алгоритм сверяется с закрытым словарём ДО разрешения ключа — иначе «без
//     подписи» доходит до поиска ключа и оплачивается обращением к источнику;
//  2. форма идентификатора ключа ограничивается ДО использования;
//  3. запись источника выбирается по объявленному издателю ТОЛЬКО поиском в
//     таблице — ни перебора, ни адреса, выведенного из самого издателя;
//  4. подпись — до утверждений: срок и адресат не значат ничего, пока не
//     доказано, кто их написал;
//  5. отзыв — последним, потому что спрашивать авторитет о токене, который и
//     так негоден, незачем.
func (v *Verifier) Verify(ctx context.Context, raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("%w: not a compact JWS", ErrInvalidToken)
	}

	var hdr jwtHeader
	if err := decodeSegment(parts[0], &hdr); err != nil {
		return "", fmt.Errorf("%w: bad header", ErrInvalidToken)
	}
	// (1) Закрытый словарь алгоритмов — общий для всей платформы. Пустое
	// значение в него не входит: «алгоритм не назван» означало бы «любой».
	if !tokenpolicy.AlgorithmAllowed(hdr.Alg) {
		return "", fmt.Errorf("%w: algorithm outside the declared dictionary", ErrInvalidToken)
	}
	// (2) Идентификатор ключа — недоверенный вход.
	if !keyIDWellFormed(hdr.Kid) {
		return "", fmt.Errorf("%w: malformed key id", ErrInvalidToken)
	}
	// (2а) Параметр, помеченный отправителем обязательным к пониманию, мы
	// обязаны либо исполнить, либо отвергнуть токен целиком (RFC 7515 §4.1.11).
	// Обратная сторона того же требования — НЕ помеченное неизвестное
	// игнорируется (RFC 7519, EID 8060): именно на этом держится совместимость,
	// поэтому неизвестные поля заголовка и утверждений разбор молча пропускает.
	if ok, name := tokenpolicy.CriticalHeadersUnderstood(hdr.Crit); !ok {
		return "", fmt.Errorf("%w: critical header %q is not understood", ErrInvalidToken, name)
	}

	var claims jwtClaims
	if err := decodeSegment(parts[1], &claims); err != nil {
		return "", fmt.Errorf("%w: bad claims", ErrInvalidToken)
	}
	// (3) Объявленный издатель — тоже недоверенный вход, и здесь он служит
	// ИСКЛЮЧИТЕЛЬНО ключом поиска.
	rec, ok := v.records[claims.Iss]
	if !ok {
		return "", fmt.Errorf("%w: issuer has no declared key-set record", ErrInvalidToken)
	}
	// Тип равен ожидаемому для ЭТОЙ записи. Сравнение регистронезависимо:
	// `typ` — медиа-тип, а они регистронезависимы по RFC.
	//
	// ОТСУТСТВИЕ типа и НЕСОВПАДЕНИЕ типа — разные вещи, и различает их только
	// эта ветка. Несовпадение отвергается всегда; отсутствие — всегда, кроме
	// полосы, которой послабление выдано явно и с предикатом снятия.
	if hdr.Typ == "" {
		if !rec.tolerateNoTyp {
			return "", fmt.Errorf("%w: token declares no type", ErrInvalidToken)
		}
	} else if !strings.EqualFold(hdr.Typ, rec.tokenType) {
		return "", fmt.Errorf("%w: unexpected token type", ErrInvalidToken)
	}

	key, err := v.keyFor(ctx, rec, hdr.Kid)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	// Алгоритм, ЗАКРЕПЛЁННЫЙ за ключом источником. Заголовок его не выбирает.
	if key.alg != "" && key.alg != hdr.Alg {
		return "", fmt.Errorf("%w: header algorithm differs from the algorithm bound to the key", ErrInvalidToken)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("%w: bad signature encoding", ErrInvalidToken)
	}
	// (4)
	if err := verifySignature(hdr.Alg, key.pub, []byte(parts[0]+"."+parts[1]), sig); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if err := v.checkTime(claims); err != nil {
		return "", err
	}
	if !claims.Aud.contains(v.aud) {
		return "", fmt.Errorf("%w: audience mismatch", ErrInvalidToken)
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("%w: empty subject", ErrInvalidToken)
	}

	// (5) Отзыв — на предъявлении. Любой неопознанный исход авторитета
	// закрывает доступ: «не дозвонился» не означает «разрешено».
	if rec.readRevocation {
		active, rerr := v.revoke.Active(ctx, raw)
		if rerr != nil {
			return "", fmt.Errorf("%w: revocation authority did not answer", ErrInvalidToken)
		}
		if !active {
			return "", fmt.Errorf("%w: revoked", ErrInvalidToken)
		}
	}

	// Принципал Kachō — источник истины субъекта авторизации. У прежнего
	// издателя он приезжает обогащением; пусто → падаем обратно на `sub`.
	if pid := claims.Ext.ExtClaims.KachoPrincipalID; pid != "" {
		return pid, nil
	}
	return claims.Sub, nil
}

// checkTime проверяет срок и момент вступления в силу с ОБЪЯВЛЕННЫМ допуском на
// расхождение часов.
//
// Срок ОБЯЗАТЕЛЕН, и это включено явно: разбор, встретив срок, его проверит, а
// не встретив — не возразит. Токен без срока живёт вечно, и заметить это на
// положительном пути нельзя.
func (v *Verifier) checkTime(c jwtClaims) error {
	now := v.now()
	if c.Exp == 0 {
		return fmt.Errorf("%w: expiry is required", ErrInvalidToken)
	}
	if now.Add(-v.skew).After(time.Unix(c.Exp, 0)) {
		return fmt.Errorf("%w: expired", ErrInvalidToken)
	}
	horizon := now.Add(v.skew)
	if c.Nbf != 0 && time.Unix(c.Nbf, 0).After(horizon) {
		return fmt.Errorf("%w: not valid yet", ErrInvalidToken)
	}
	if c.Iat != 0 && time.Unix(c.Iat, 0).After(horizon) {
		return fmt.Errorf("%w: issued in the future", ErrInvalidToken)
	}
	return nil
}

// keyIDWellFormed ограничивает форму идентификатора ключа ДО его использования.
//
// Значение приходит от предъявителя. Разрешены только знаки, из которых
// составляют идентификаторы ключей (base64url плюс точка, тильда и двоеточие):
// ни разделителей пути, ни управляющих символов, ни разметки — и с потолком
// длины, чтобы четырёхкилобайтная строка не доходила ни до поиска, ни до
// журнала.
//
// Пустое значение негодно тем же правилом: токен без идентификатора ключа
// отвергается одинаково до и после ротации.
func keyIDWellFormed(kid string) bool {
	if kid == "" || len(kid) > maxKeyIDLen {
		return false
	}
	for i := 0; i < len(kid); i++ {
		c := kid[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == '~', c == ':':
		default:
			return false
		}
	}
	return true
}

// keyFor возвращает ключ записи по идентификатору. Свежий снимок с этим
// идентификатором — отдаём сразу; иначе РОВНО ОДИН вынужденный перезапрос.
//
// Вынужденный перезапрос намеренно игнорирует срок годности: снимок бывает
// свежим и уже неполным, и именно этот повод поглощает ротацию, случившуюся в
// середине отсрочки. Его цена ограничена собственным интервалом — иначе поток
// выдуманных идентификаторов превращается в поток обращений к публикатору.
//
// Обслуживание по протухшему снимку ОГРАНИЧЕНО во времени: окно интервала
// возобновляется каждой попыткой, поэтому без абсолютной границы постоянно
// недоступный источник оставлял бы снятый ключ валидным вечно.
func (v *Verifier) keyFor(ctx context.Context, rec *issuerRecord, kid string) (keyRecord, error) {
	rec.mu.Lock()
	now := v.now()
	key, ok := rec.keys[kid]
	age := now.Sub(rec.fetched)
	if ok && age < rec.ttl {
		rec.mu.Unlock()
		return key, nil
	}
	// Слот перезапроса захватывается ПОД замком до отпускания его на обращение
	// к источнику: конкурентные промахи схлопываются в одно обращение, а не
	// веерятся в N исходящих соединений.
	if now.Sub(rec.lastRefresh) < defaultMinRefresh {
		servable := ok && age < rec.ttl+staleServeAttempts*defaultMinRefresh
		rec.mu.Unlock()
		if servable {
			return key, nil
		}
		if ok {
			return keyRecord{}, fmt.Errorf("key unconfirmed by its source for %s", age.Truncate(time.Second))
		}
		return keyRecord{}, errors.New("unknown key id")
	}
	rec.lastRefresh = now
	rec.mu.Unlock()

	// Обращение отвязано от контекста вызывающего: слот уже захвачен, поэтому
	// обрыв соединения победителем слота не должен ни срывать общее обновление
	// ключей, ни сжигать слот, блокируя подхват ротации для остальных.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), v.http.Timeout)
	defer cancel()
	if err := v.refresh(fetchCtx, rec); err != nil {
		return keyRecord{}, err
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if k, ok := rec.keys[kid]; ok {
		return k, nil
	}
	return keyRecord{}, errors.New("unknown key id")
}

// refresh тянет набор ОДНОЙ записи и перестраивает её снимок.
//
// Тип содержимого проверяется ДО разбора, а чтение прекращается на объявленном
// потолке: ответ, по форме не являющийся набором ключей, — это признак того, что
// по адресу не тот эндпоинт, и разбирать его незачем.
func (v *Verifier) refresh(ctx context.Context, rec *issuerRecord) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rec.url, nil)
	if err != nil {
		return errors.New("key set request could not be built")
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return errors.New("key set source did not answer")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("key set source answered %d", resp.StatusCode)
	}
	if err := keySetContentTypeOK(resp.Header.Get("Content-Type")); err != nil {
		return err
	}

	// Потолок тела: читаем на один байт больше и отвергаем превышение целиком.
	// Обрезанное тело нельзя разбирать «сколько влезло» — неполный набор
	// неотличим от полного, и потребитель принял бы его за истину.
	body, err := io.ReadAll(io.LimitReader(resp.Body, tokenpolicy.KeySetBodyCeiling+1))
	if err != nil {
		return errors.New("key set body could not be read")
	}
	if len(body) > tokenpolicy.KeySetBodyCeiling {
		return fmt.Errorf("key set body exceeds the declared ceiling of %d bytes", tokenpolicy.KeySetBodyCeiling)
	}

	var doc struct {
		Keys []jsonWebKey `json:"keys"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&doc); err != nil {
		return errors.New("key set body is not a key set document")
	}

	fresh := make(map[string]keyRecord, len(doc.Keys))
	for _, k := range doc.Keys {
		if !keyIDWellFormed(k.Kid) {
			continue
		}
		pub, perr := k.toKey()
		if perr != nil {
			// ПОТРЕБИТЕЛЬ пропускает ключ, которого не понимает, и принимает
			// набор. Это НАМЕРЕННО противоположно правилу публикатора («не
			// можешь отдать целиком — не отдавай ничего»), и предметы у них
			// разные: там неполнота выдаётся за полноту, здесь один незнакомый
			// ключ (новый вид, будущий алгоритм) обвалил бы проверку ВСЕХ
			// токенов сразу.
			continue
		}
		fresh[k.Kid] = keyRecord{pub: pub, alg: k.Alg}
	}

	ttl := parseMaxAge(resp.Header.Get("Cache-Control"))
	if ttl > maxTTL {
		ttl = maxTTL // потолок: непомерный срок годности не растягивает окно ротации
	}
	rec.mu.Lock()
	rec.keys = fresh
	rec.fetched = v.now()
	if ttl > 0 {
		rec.ttl = ttl
	}
	rec.mu.Unlock()
	return nil
}

// keySetContentTypeOK отвергает ответ, который набором ключей не является.
//
// Ответ не того типа — признак НАСТРОЙКИ (по адресу стоит не тот эндпоинт), а
// не сбоя: повтором он не лечится. Проверка стоит до разбора, потому что разбор
// страницы с ошибкой может случайно дать пустой набор, а пустой набор читается
// как факт «ключей нет».
func keySetContentTypeOK(header string) error {
	if strings.TrimSpace(header) == "" {
		return errors.New("key set answer carries no content type")
	}
	mt, _, err := mime.ParseMediaType(header)
	if err != nil {
		return errors.New("key set answer carries an unparsable content type")
	}
	switch strings.ToLower(mt) {
	case "application/json", "application/jwk-set+json":
		return nil
	default:
		return errors.New("key set answer is not a key set media type")
	}
}

// verifySignature проверяет подпись по алгоритму. Вид ключа обязан
// соответствовать алгоритму — несоответствие отвергается (иначе ключ одного вида
// подставляется в проверку другого).
func verifySignature(alg string, key crypto.PublicKey, signingInput, sig []byte) error {
	switch alg {
	case tokenpolicy.AlgRS256:
		pub, ok := key.(*rsa.PublicKey)
		if !ok {
			return errors.New("key type mismatch for RS256")
		}
		sum := sha256.Sum256(signingInput)
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
			return errors.New("signature mismatch")
		}
		return nil
	case tokenpolicy.AlgES256:
		pub, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return errors.New("key type mismatch for ES256")
		}
		// ES256 в JWS: сырая подпись r||s, по 32 байта (P-256).
		if len(sig) != 64 {
			return errors.New("bad ES256 signature length")
		}
		sum := sha256.Sum256(signingInput)
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(pub, sum[:], r, s) {
			return errors.New("signature mismatch")
		}
		return nil
	case tokenpolicy.AlgEdDSA:
		pub, ok := key.(ed25519.PublicKey)
		if !ok {
			return errors.New("key type mismatch for EdDSA")
		}
		// Ed25519 подписывает СООБЩЕНИЕ, а не его свёртку: подставить сюда
		// заранее посчитанный SHA-256 значило бы проверять не то.
		if len(sig) != ed25519.SignatureSize {
			return errors.New("bad EdDSA signature length")
		}
		if !ed25519.Verify(pub, signingInput, sig) {
			return errors.New("signature mismatch")
		}
		return nil
	default:
		return errors.New("algorithm outside the declared dictionary")
	}
}

// jsonWebKey — ключ набора: RSA (n/e), EC/P-256 (crv/x/y) либо OKP/Ed25519
// (crv/x) — все в base64url, big-endian.
type jsonWebKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Crv string `json:"crv"`
	N   string `json:"n"`
	E   string `json:"e"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// toKey собирает проверочный ключ по виду. Поддержаны ровно те виды, которые
// отвечают закрытому словарю алгоритмов; остальные — ошибка (и пропуск набором).
func (k jsonWebKey) toKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		return k.toRSA()
	case "EC":
		return k.toECDSA()
	case "OKP":
		return k.toOKP()
	default:
		return nil, fmt.Errorf("unsupported kty %q", k.Kty)
	}
}

// toRSA собирает *rsa.PublicKey из base64url n/e.
func (k jsonWebKey) toRSA() (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}
	if len(nb) == 0 || len(eb) == 0 {
		return nil, errors.New("empty modulus/exponent")
	}
	e := new(big.Int).SetBytes(eb)
	if !e.IsInt64() || e.Int64() < 2 {
		return nil, errors.New("invalid exponent")
	}
	n := new(big.Int).SetBytes(nb)
	// Короткий модуль факторизуется, а значит подпись подделывается. Такой ключ
	// не попадает в снимок вовсе.
	if n.BitLen() < tokenpolicy.MinRSAModulusBits {
		return nil, fmt.Errorf("RSA modulus too small: %d bits (min %d)",
			n.BitLen(), tokenpolicy.MinRSAModulusBits)
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

// toECDSA собирает *ecdsa.PublicKey из base64url x/y для кривой P-256. Точка
// проверяется как лежащая на кривой — иначе подложный ключ попадёт в снимок.
func (k jsonWebKey) toECDSA() (*ecdsa.PublicKey, error) {
	if k.Crv != "P-256" {
		return nil, fmt.Errorf("unsupported EC curve %q", k.Crv)
	}
	xb, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, err
	}
	yb, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, err
	}
	if len(xb) == 0 || len(xb) > 32 || len(yb) == 0 || len(yb) > 32 {
		return nil, errors.New("invalid EC coordinate length")
	}
	// Несжатая точка SEC1: 0x04 || X(32) || Y(32); NewPublicKey проверяет,
	// что она на кривой.
	uncompressed := make([]byte, 1+32+32)
	uncompressed[0] = 4
	copy(uncompressed[1+(32-len(xb)):33], xb)
	copy(uncompressed[33+(32-len(yb)):], yb)
	if _, perr := ecdh.P256().NewPublicKey(uncompressed); perr != nil {
		return nil, fmt.Errorf("invalid EC point: %w", perr)
	}
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xb),
		Y:     new(big.Int).SetBytes(yb),
	}, nil
}

// toOKP собирает ed25519.PublicKey из base64url x. Кривая — только Ed25519:
// закрытый словарь алгоритмов называет EdDSA, и других кривых у него нет.
func (k jsonWebKey) toOKP() (ed25519.PublicKey, error) {
	if k.Crv != "Ed25519" {
		return nil, fmt.Errorf("unsupported OKP curve %q", k.Crv)
	}
	xb, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, err
	}
	if len(xb) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid Ed25519 public key length %d", len(xb))
	}
	return ed25519.PublicKey(xb), nil
}

// audience — `aud`, допускающий строку ИЛИ массив строк (RFC 7519).
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		*a = audience{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a audience) contains(want string) bool {
	for _, v := range a {
		if v == want {
			return true
		}
	}
	return false
}

// decodeSegment раскодирует сегмент JOSE и разбирает его как JSON.
func decodeSegment(seg string, out any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// parseMaxAge извлекает max-age (секунды) из Cache-Control; 0 — не задан либо негоден.
func parseMaxAge(cacheControl string) time.Duration {
	for _, part := range strings.Split(cacheControl, ",") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "max-age="); ok {
			var secs int
			if _, err := fmt.Sscanf(v, "%d", &secs); err == nil && secs > 0 {
				return time.Duration(secs) * time.Second
			}
		}
	}
	return 0
}
