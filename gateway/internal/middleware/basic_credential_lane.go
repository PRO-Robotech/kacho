// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/credsecret"
)

// BasicCredentialVerdictWindow — окно вердикта о предъявленном (приёмка BAT-1
// §1.4, §2.2). ТО ЖЕ число, что уже объявлено для прочих полос, и второй ручки
// под него не заводится: «отзыв действует не позже N» обязано означать одно и
// то же на всех полосах.
//
// Оно же — ГАРАНТИРОВАННАЯ ГРАНИЦА ОТЗЫВА: отозванное удостоверение
// отвергается не позже этого срока после того, как отзыв закоммичен. Типичная
// задержка меньше, но утверждать надо гарантию: проба, утверждающая типичное,
// зеленеет на сломанной гарантии.
const BasicCredentialVerdictWindow = 5 * time.Second

// BasicCredentialLevel — уровень аутентификации базового секрета.
//
// «1», а не «2», и следствие названо: человек, предъявивший базовый секрет, НЕ
// МОЖЕТ ни выпустить новое удостоверение, ни отозвать существующее — эти
// действия остаются за интерактивным входом. Долгоживущий предъявительский
// секрет не должен уметь чеканить себе смену.
const BasicCredentialLevel = "1"

// ErrCredentialRefused — ЕДИНЫЙ отказ в самом удостоверении. Неизвестный
// идентификатор, неверный секрет, истёкший срок, отозванное, неактивный
// владелец — один исход: различимый был бы оракулом.
var ErrCredentialRefused = errors.New("credential refused")

// ErrCredentialStateUnknown — авторитет не ответил либо ответил не тем.
//
// ОТДЕЛЬНЫЙ исход, и это решение, а не недосмотр: предлагать вызывающему
// переаутентифицироваться на неисправность, которую не исправит ни одно его
// удостоверение, значит вводить его в заблуждение. Наружу он тоже отличим —
// UNAVAILABLE, не UNAUTHENTICATED.
var ErrCredentialStateUnknown = errors.New("credential state could not be established")

// basicCredentialAuthority — порт авторитета. Край зависит от него, а не от
// сгенерированного клиента: подставить детерминированный дублёр в пробе иначе
// нельзя, а проба стоимости обязана СЧИТАТЬ вызовы.
type basicCredentialAuthority interface {
	Resolve(ctx context.Context, presented string) (*iamv1.ResolveBasicCredentialResponse, error)
}

// BasicVerifiedCredential — то, что полоса кладёт вниз по потоку.
//
// У КАЖДОГО поля назван источник (приёмка BAT-1 §5.2). Поле, которое полоса не
// заполнила, делает нижележащий контроль ПРОЙДЕННЫМ МИМО, а не успешно, — и это
// неотличимо от исправной работы.
type BasicVerifiedCredential struct {
	// PrincipalType / PrincipalID / DisplayName — из ответа авторитета.
	PrincipalType string
	PrincipalID   string
	DisplayName   string
	// CredentialID — идентификатор удостоверения; им адресуется отзыв.
	CredentialID string
	// AuthenticationLevel — константа «1» (см. BasicCredentialLevel).
	AuthenticationLevel string
	// ExpiresAt — срок строки удостоверения. Заполнен всегда.
	ExpiresAt time.Time
	// Confirmation — признак привязки к предъявителю. ОТСУТСТВУЕТ ВСЕГДА: вид
	// предъявительский by construction, и заполнять его нечем — докерная полоса
	// шлёт `Basic` и доказать владение не может ничем.
	Confirmation string
}

// BasicCredentialLane — полоса приёма базового секрета на крае.
//
// ТРИ УРОВНЯ ОТСЕВА, и только третий стоит вызова к соседу:
//  1. марка — сравнение префикса, обращения нет;
//  2. форма и контрольная сумма — одно хеширование, обращения НЕТ;
//  3. вердикт авторитета — один вызов НА УДОСТОВЕРЕНИЕ ЗА ОКНО, с кэшем.
//
// Стоимость равна числу РАЗЛИЧНЫХ ПРЕДЪЯВЛЯЕМЫХ удостоверений за окно и от
// числа запросов не зависит.
type BasicCredentialLane struct {
	authority basicCredentialAuthority
	now       func() time.Time

	mu    sync.Mutex
	cache map[string]cachedVerdict
}

type cachedVerdict struct {
	value   BasicVerifiedCredential
	expires time.Time
}

// NewBasicCredentialLane конструирует полосу.
func NewBasicCredentialLane(a basicCredentialAuthority) *BasicCredentialLane {
	return &BasicCredentialLane{
		authority: a,
		now:       time.Now,
		cache:     make(map[string]cachedVerdict),
	}
}

// WithClock подменяет часы. Существует ради проб истечения окна: пауза длиной в
// окно закрепила бы конкретную задержку вместо границы и сделала бы прогон
// медленным ради ничего.
func (l *BasicCredentialLane) WithClock(now func() time.Time) *BasicCredentialLane {
	l.now = now
	return l
}

// Owns отвечает, НАША ли это полоса. Решает МАРКА и только она.
//
// Запрещено «не разобралось как подписанный токен — попробуем как секрет»:
// запасной путь, срабатывающий на неудаче, превращает всякую негодную строку во
// вход второй полосы и делает диагностику невозможной. И обратно: строка,
// несущая нашу марку, ОСТАЁТСЯ нашей даже будучи негодной — вердикт по ней
// выносим мы, а не отдаём дальше как «удостоверения нет вовсе».
func (l *BasicCredentialLane) Owns(presented string) bool {
	return credsecret.HasMark(presented)
}

// Verify выносит вердикт. Полоса ТЕРМИНАЛЬНА: вызывающий обязан вернуть отказ,
// а не продолжить другими полосами.
func (l *BasicCredentialLane) Verify(ctx context.Context, presented string) (BasicVerifiedCredential, error) {
	// Уровень 2. Обращения к соседу нет — обрезанный, опечатанный и
	// подделанный наугад вход не оплачивается вызовом.
	p, err := credsecret.Parse(presented)
	if err != nil {
		return BasicVerifiedCredential{}, ErrCredentialRefused
	}

	// КЛЮЧ КЭША — идентификатор удостоверения И отпечаток предъявленной строки,
	// НИКОГДА сама строка. Отпечаток обязателен: без него неверный секрет при
	// верном идентификаторе получил бы положительный вердикт по кэшу — то есть
	// кэш стал бы обходом проверки.
	key := verdictCacheKey(p.CredentialID, presented)
	if v, ok := l.lookup(key); ok {
		return v, nil
	}

	callCtx, cancel := context.WithTimeout(ctx, BasicCredentialCallBudget)
	defer cancel()

	resp, rerr := l.authority.Resolve(callCtx, presented)
	if rerr != nil {
		// Различить «негодное удостоверение» и «авторитет не отвечает» —
		// обязанность вызывающего авторитета, а не наша догадка: вердикт об
		// удостоверении он объявляет отказом аутентификации, всё прочее — это
		// неспособность установить состояние.
		if isCredentialRefusal(rerr) {
			return BasicVerifiedCredential{}, ErrCredentialRefused
		}
		// МОЛЧАНИЕ АВТОРИТЕТА — ОТКАЗ. Мягкий проход означал бы: выдаём,
		// отзываем и свой же отзыв не исполняем.
		return BasicVerifiedCredential{}, ErrCredentialStateUnknown
	}

	v := BasicVerifiedCredential{
		PrincipalType:       resp.GetPrincipalType(),
		PrincipalID:         resp.GetPrincipalId(),
		DisplayName:         resp.GetDisplayName(),
		CredentialID:        resp.GetCredentialId(),
		AuthenticationLevel: BasicCredentialLevel,
		// Confirmation остаётся пустым — и это не пропуск, а объявление.
	}
	if ts := resp.GetExpiresAt(); ts != nil {
		v.ExpiresAt = ts.AsTime()
	}
	// В кэш кладётся ТОЛЬКО положительный вердикт. Отрицательный кэшировать
	// нельзя по обратной причине: он не сходится сам, и ошибочно закэшированный
	// отказ пережил бы починку.
	l.store(key, v)
	return v, nil
}

// isCredentialRefusal отличает вердикт ОБ УДОСТОВЕРЕНИИ от неспособности его
// установить. Классификация идёт по коду ответа авторитета, а не по тексту:
// текст — контракт для человека, код — для машины.
//
// Корзины «прочее» здесь нет намеренно: ВСЁ, что не является объявленным
// отказом в удостоверении, считается неустановленным состоянием и ведёт к
// fail-closed. Обратный порядок («всё непонятное — отказ в удостоверении»)
// предлагал бы вызывающему исправлять чужую поломку сменой своего секрета.
func isCredentialRefusal(err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	return st.Code() == codes.Unauthenticated
}

// BasicCredentialCallBudget — бюджет одного вызова к авторитету. Названный
// бюджет обязателен: неотвечающий сосед без него вешает горутину навсегда.
const BasicCredentialCallBudget = time.Second

func (l *BasicCredentialLane) lookup(key string) (BasicVerifiedCredential, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.cache[key]
	if !ok || !c.expires.After(l.now()) {
		return BasicVerifiedCredential{}, false
	}
	return c.value, true
}

func (l *BasicCredentialLane) store(key string, v BasicVerifiedCredential) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache[key] = cachedVerdict{value: v, expires: l.now().Add(BasicCredentialVerdictWindow)}
}

// CacheKeysForTest — перепись ключей кэша. Существует ради утверждения СОСТАВА
// ключа: «сырой строки в карте нет» иначе проверяется только чтением кода, а
// чтение кода свойства не измеряет.
func (l *BasicCredentialLane) CacheKeysForTest() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.cache))
	for k := range l.cache {
		out = append(out, k)
	}
	return out
}

// verdictCacheKey — идентификатор плюс отпечаток строки. Сырой секрет в карту
// не попадает: правило уже действует на соседней полосе и здесь не ослабляется.
func verdictCacheKey(credentialID, presented string) string {
	sum := sha256.Sum256([]byte("kacho.edge.verdict.v1\x00" + credentialID + "\x00" + presented))
	return credentialID + ":" + hex.EncodeToString(sum[:16])
}

// basicAuthorityAdapter оборачивает сгенерированный клиент в порт полосы.
type basicAuthorityAdapter struct {
	stub interface {
		ResolveBasicCredential(context.Context, *iamv1.ResolveBasicCredentialRequest, ...grpc.CallOption) (*iamv1.ResolveBasicCredentialResponse, error)
	}
}

// NewBasicAuthorityFromStub оборачивает `iamv1.InternalIAMServiceClient`.
func NewBasicAuthorityFromStub(stub interface {
	ResolveBasicCredential(context.Context, *iamv1.ResolveBasicCredentialRequest, ...grpc.CallOption) (*iamv1.ResolveBasicCredentialResponse, error)
}) *basicAuthorityAdapter {
	return &basicAuthorityAdapter{stub: stub}
}

func (a *basicAuthorityAdapter) Resolve(ctx context.Context, presented string) (*iamv1.ResolveBasicCredentialResponse, error) {
	return a.stub.ResolveBasicCredential(ctx, &iamv1.ResolveBasicCredentialRequest{Presented: presented})
}
