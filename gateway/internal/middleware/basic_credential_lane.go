// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/lrucache"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
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
// числа запросов не зависит — ПОКА они помещаются в потолок кэша
// (basicCredentialCacheMaxEntries). Сверх потолка наименее свежие вердикты
// вытесняются, и вытесненное удостоверение оплачивает вызов заново: стоимость
// растёт, полоса не открывается. Достижение потолка объявляется оператору
// (noteCapacity) — иначе рост цены был бы неотличим от исправной работы.
//
// ОКНО ОТЗЫВА ОТ ЭТОГО НЕ РАСТЁТ: вытеснение только СОКРАЩАЕТ жизнь вердикта,
// поэтому граница «отозванное отвергается не позже окна» остаётся верной.
type BasicCredentialLane struct {
	authority basicCredentialAuthority
	now       func() time.Time
	logger    *slog.Logger

	// cache — ЕДИНЫЙ ограниченный примитив (internal/lrucache), тот же, что у
	// кэшей решения, интроспекции, повтора и сессии: логика вытеснения написана
	// и проверена ровно один раз.
	//
	// Здесь стояла голая карта, и у неё не было вытеснения НИ ПО РАЗМЕРУ, НИ ПО
	// ИСТЕЧЕНИЮ: чтение просроченной записи возвращало промах и оставляло её в
	// карте. Замер #1218 на ревизии e4da590cf: тысяча удостоверений, часы на сто
	// окон вперёд, ещё тысяча ДРУГИХ — в карте две тысячи записей. То есть рост
	// ограничивался не окном, а временем жизни процесса.
	cache *lrucache.Cache[string, BasicVerifiedCredential]

	// atCapacity — защёлка «потолок был достигнут». Отдельная величина, а не
	// вывод из числа записей: заполненный кэш и кэш, которого не спрашивали,
	// иначе читаются одинаково. Проверяется ТОЛЬКО пока не взведена, поэтому
	// перепись живых записей не ложится на путь запроса навсегда.
	atCapacity atomic.Bool
}

// basicCredentialCacheMaxEntries — потолок числа записей кэша вердиктов.
//
// # Откуда число, и почему это НЕ «с запасом»
//
// Величина отвечает на вопрос «сколько РАЗЛИЧНЫХ удостоверений занимают запись
// в пределах окна», а не «сколько запросов в секунду»: запись одна на
// удостоверение независимо от числа предъявлений.
//
// Взято не из ощущения, а у соседа ТОГО ЖЕ КЛАССА и с ТЕМ ЖЕ ОКНОМ: кэш
// интроспекции — тоже вердикт об одном предъявленном удостоверении, тоже с
// окном 5 с, — и этот профиль посадки объявляет его вместимость равной 10000
// (`KACHO_INTROSPECTION_CACHE_SIZE`). То есть 10000 — уже данный этим
// развёртыванием ответ на тот самый вопрос.
//
// Для ЭТОЙ полосы число является ВЕРХНЕЙ границей, а не нижней: базовый секрет
// предъявляют машины — ключи служебных учёток, токены, докерные клиенты, — а их
// заведомо меньше, чем интерактивных предъявителей несомых токенов.
//
// # Цена названа замером, а не оценкой
//
// 307 байт на запись (замер #1218: 100000 записей, прирост кучи 30695552 байта,
// строки значения различны — в проде они декодируются с провода, а не берутся
// из общего литерала). Потолок стоит 307 Б × 10000 ≈ 3.1 МБ.
//
// # Почему константа, а не ручка
//
// Ручку заводит наблюдение, а не предчувствие: величина, которую никто не
// двигал, добавляет профилю поверхность и второе место об одном предмете. Тот
// же выбор сделан у кэша сессии (kratosCacheMaxEntries). Появится посадка, где
// защёлка ниже взводится в норме, — число станет ручкой вместе с ней.
const basicCredentialCacheMaxEntries = 10000

// NewBasicCredentialLane конструирует полосу.
func NewBasicCredentialLane(a basicCredentialAuthority) *BasicCredentialLane {
	l := &BasicCredentialLane{authority: a, now: time.Now}
	// Часы читаются ЧЕРЕЗ ПОЛЕ: WithClock подменяет их после конструирования, а
	// снимок значения в этот момент оставил бы кэш на настоящих часах — и пробы
	// истечения окна утверждали бы о другом кэше.
	l.cache = lrucache.New[string, BasicVerifiedCredential](
		basicCredentialCacheMaxEntries,
		BasicCredentialVerdictWindow,
		func() time.Time { return l.now() },
	)
	return l
}

// WithLogger провязывает журнал. nil-безопасно: наблюдение не роняет сборку.
func (l *BasicCredentialLane) WithLogger(lg *slog.Logger) *BasicCredentialLane {
	l.logger = lg
	return l
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
	return l.cache.Get(key)
}

// store кладёт вердикт под окном полосы и отмечает достижение потолка.
//
// ВЫТЕСНЕНИЕ НЕ ОТКРЫВАЕТ ПОЛОСУ, и это свойство построения, а не обещания:
// вытесненный ключ даёт ПРОМАХ, а промах в Verify ведёт к вызову авторитета —
// единственная ветка, возвращающая положительный вердикт мимо кэша, требует
// ответа авторитета. Потерять запись значит заплатить вызовом, никогда — пройти.
func (l *BasicCredentialLane) store(key string, v BasicVerifiedCredential) {
	l.cache.Put(key, v)
	l.noteCapacity()
}

// noteCapacity взводит защёлку заполнения и говорит об этом ОДИН раз.
//
// Однократно потому, что запись на каждое вытеснение — это запись на каждый
// промах под нагрузкой, то есть шум, который перестают читать. И перепись живых
// записей после взведения больше не считается: путь запроса не платит за
// наблюдение, которое уже состоялось.
func (l *BasicCredentialLane) noteCapacity() {
	if l.atCapacity.Load() {
		return
	}
	if l.cache.Len() < basicCredentialCacheMaxEntries {
		return
	}
	if !l.atCapacity.CompareAndSwap(false, true) {
		return
	}
	if l.logger == nil {
		return
	}
	// Величины берутся у CacheStats, а не собираются здесь заново: иначе
	// оператор и проба читали бы два разных места об одном предмете, а
	// расходятся такие пары молча.
	st := l.CacheStats()
	l.logger.Warn("basic credential lane: verdict cache reached capacity",
		"entries", st.Entries,
		"capacity", st.Capacity,
		"window", BasicCredentialVerdictWindow.String(),
		// Число вытеснений НА МОМЕНТ ЗАЩЁЛКИВАНИЯ. Имя серии здесь намеренно НЕ
		// называется: оно жило бы вторым местом об одном предмете и разошлось бы
		// с коллектором молча. Скорость читается сериями, а не журналом, — на то
		// они и заведены (#1221).
		"evictions", st.Evictions,
		"consequence", "least-recently-used verdicts are evicted; an evicted credential is re-checked against the authority, never waved through")
}

// BasicCredentialCacheStats — ПРОЧИТАННЫЕ величины кэша вердиктов.
//
// Именно прочитанные: по молчанию «кэш пуст» и «кэша не спрашивали» неразличимы,
// а прочитанный ноль их различает (`security.md` §Hardening-инвариант 8(в)).
type BasicCredentialCacheStats struct {
	// Entries — ЖИВЫХ записей. Просроченные, ещё не вытесненные, не в счёт:
	// вердиктом они не являются.
	Entries int
	// Capacity — потолок числа записей.
	Capacity int
	// AtCapacity — потолок был достигнут хотя бы раз за жизнь процесса.
	// Отдельный факт: без него исчерпание неотличимо от исправной работы, ведь
	// заполненный кэш продолжает отвечать правильно — он лишь чаще спрашивает
	// авторитета.
	AtCapacity bool
	// Evictions — вердиктов, снятых ПОД ДАВЛЕНИЕМ ПОТОЛКА, за жизнь процесса.
	//
	// Третья величина заведена ради решения, которое по первым двум не
	// принимается (#1221): защёлка отвечает «дошли ли до потолка», занятость —
	// «сколько там сейчас», и ни одна не отвечает «насколько быстро растём».
	// Между тем потолок, достигнутый раз в сутки, и потолок, перемалывающий
	// сотню записей в минуту, требуют разного, а защёлка у них ОДНА И ТА ЖЕ.
	//
	// Величина монотонна, поэтому её производная по времени и есть искомая
	// скорость. Оборот окна сюда не считается — иначе она росла бы и на
	// незаполненном кэше (`lrucache.Cache.Evictions`).
	Evictions uint64
}

// CacheStats отдаёт величины кэша. Читатель — оператор: заполнение обязано быть
// видно ДО того, как рост станет предметом разбора.
func (l *BasicCredentialLane) CacheStats() BasicCredentialCacheStats {
	return BasicCredentialCacheStats{
		Entries:    l.cache.Len(),
		Capacity:   basicCredentialCacheMaxEntries,
		AtCapacity: l.atCapacity.Load(),
		Evictions:  l.cache.Evictions(),
	}
}

// CacheKeysForTest — перепись ЗАНЯТЫХ ключей, включая просроченные, ещё не
// вытесненные. Существует ради двух утверждений, которые иначе проверяются
// только чтением кода: СОСТАВ ключа («сырой строки в карте нет») и
// ОСВОБОЖДЕНИЕ («мёртвая запись не держит место»). Второе счётчиком живых
// записей не измеряется — он мёртвых не видит by construction.
func (l *BasicCredentialLane) CacheKeysForTest() []string {
	return l.cache.Keys()
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
