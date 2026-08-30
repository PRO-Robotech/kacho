// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/safeconv"
)

// OwnerConn — то, чем край дозванивается до владельца журнала.
//
// Это СГЕНЕРЁННЫЙ клиент общего глагола, а не свой интерфейс поверх него.
// Свой был бы вторым объявлением одного предмета и разошёлся бы с контрактом
// в день, когда контракт поправят; сгенерённый разойтись не может by
// construction — он и есть контракт.
type OwnerConn = subscriptionv1.InternalSubscriptionServiceClient

// Config — что приносит композиционный корень края.
//
// Здесь стоят величины ПОСАДКИ. Умолчаний у них нет: величина, которую никто не
// выбирал, не обсуждаема и не сужаема, а обнаруживается первым отказом в бою.
type Config struct {
	// Owners — закрытый словарь владельцев журналов. Пустой законен и означает
	// «владелец не объявлен»: ручка отвечает `501`, а не открывает поток, в
	// который никогда ничего не придёт.
	Owners Owners

	// StreamBudget — срок жизни одного потока.
	//
	// Обязан быть МЕНЬШЕ предела чтения посредника перед краем: иначе поток
	// рвёт посредник, и клиент видит сетевой сбой там, где было чистое
	// закрытие по сроку.
	StreamBudget time.Duration

	// Heartbeat — период служебного кадра поддержания связи. Молчащая подписка
	// — обычный её режим, и без кадра посредник закрыл бы соединение по своему
	// пределу чтения.
	Heartbeat time.Duration

	// MaxStreams — потолок ОДНОВРЕМЕННЫХ потоков этой реплики края.
	//
	// Каждый поток занимает горутину здесь и слот у владельца. Превышение
	// отвечает ОТКАЗОМ, а не молчаливой очередью: очередь превратила бы
	// исчерпание в неограниченное ожидание, неотличимое для клиента от
	// «событий нет».
	MaxStreams int

	// MaxStreamsPerSubject — потолок потоков ОДНОГО субъекта на этой реплике.
	//
	// Потолок реплики защищает процесс, этот — арендаторов друг от друга: без
	// него один субъект занимает [Config.MaxStreams] целиком, и остальные
	// получают отказ, не имея ни одного собственного потока. Консоль открывает
	// поток на вкладку, поэтому это не умозрительный случай.
	MaxStreamsPerSubject int

	// Logger — журнал процесса. Ноль резолвится в [slog.Default].
	Logger *slog.Logger
}

// Stats — снимок счётчиков ручки для диагностической поверхности.
//
// «Ноль отказов за всю жизнь контроля» обязано быть заметно: потолок потоков,
// который ни разу не сработал, и потолок, который не подключён, выглядят
// одинаково — если их не считать.
type Stats struct {
	Open   int64
	Opened uint64
	// RefusedInput — отказ по ВИНЕ ВЫЗЫВАЮЩЕГО: негодная форма запроса.
	RefusedInput uint64
	// RefusedNoOwner — отказ по СОСТОЯНИЮ ПОСАДКИ: владелец не объявлен.
	//
	// Считается отдельно от предыдущего намеренно: смешай их — и «клиенты
	// массово шлют мусор» стало бы неотличимо от «мы не объявили владельца», то
	// есть от собственной ошибки развёртывания.
	RefusedNoOwner uint64
	RefusedAuthN   uint64
	// RefusedSubjectKind — вызывающий НАЗВАН, но названным субъектом модели прав
	// не является (служебный принципал, тип вне закрытого словаря, псевдоним,
	// идентификатор с разделителями модели).
	//
	// Считается отдельно от [Stats.RefusedAuthN] намеренно: смешай их — и
	// «пришли без удостоверения» стало бы неотличимо от «пришли с удостоверением
	// вида, которому подписка не полагается». Первое лечит вызывающий, второе —
	// решение о том, кому подписка положена.
	RefusedSubjectKind uint64
	RefusedSlot        uint64
	// RefusedSubjectQuota — субъект исчерпал СВОЙ предел, а не предел реплики.
	RefusedSubjectQuota uint64
	RefusedOwner        uint64
	EventsSent          uint64
	ClosedByOwner       uint64
}

// Handler — единственная проекция потока. Один экземпляр на процесс края.
type Handler struct {
	cfg      Config
	log      *slog.Logger
	slots    chan struct{}
	registry *registry

	open                atomic.Int64
	opened              atomic.Uint64
	refusedInput        atomic.Uint64
	refusedNoOwner      atomic.Uint64
	refusedAuthN        atomic.Uint64
	refusedSubjectKind  atomic.Uint64
	refusedSlot         atomic.Uint64
	refusedSubjectQuota atomic.Uint64
	refusedOwner        atomic.Uint64
	eventsSent          atomic.Uint64
	closedByOwner       atomic.Uint64
}

// NewHandler собирает ручку и судит объявление посадки.
func NewHandler(cfg Config) (*Handler, error) {
	if cfg.MaxStreams <= 0 {
		return nil, fmt.Errorf("subscriptionstream: MaxStreams = %d — потолок одновременных потоков есть арифметика "+
			"горутин края и слотов владельца, а не вкус; умолчания у него нет", cfg.MaxStreams)
	}
	if cfg.MaxStreamsPerSubject <= 0 {
		return nil, fmt.Errorf("subscriptionstream: MaxStreamsPerSubject = %d — без предела на субъекта "+
			"один арендатор занимает потолок реплики целиком, и остальные получают отказ, "+
			"не имея ни одного собственного потока", cfg.MaxStreamsPerSubject)
	}
	if cfg.MaxStreamsPerSubject > cfg.MaxStreams {
		return nil, fmt.Errorf("subscriptionstream: MaxStreamsPerSubject %d превосходит MaxStreams %d — "+
			"предел, который не может быть достигнут, предела не ставит",
			cfg.MaxStreamsPerSubject, cfg.MaxStreams)
	}
	if cfg.Heartbeat <= 0 {
		return nil, fmt.Errorf("subscriptionstream: Heartbeat не объявлен — молчащая подписка обычный режим, " +
			"и без служебного кадра посредник закроет соединение по своему пределу чтения")
	}
	if cfg.StreamBudget <= cfg.Heartbeat {
		return nil, fmt.Errorf("subscriptionstream: StreamBudget %v не превосходит Heartbeat %v — поток, "+
			"истекающий раньше первого кадра поддержания связи, не поток", cfg.StreamBudget, cfg.Heartbeat)
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Handler{
		cfg:      cfg,
		log:      log,
		slots:    make(chan struct{}, cfg.MaxStreams),
		registry: newRegistry(),
	}, nil
}

// Stats отдаёт снимок счётчиков.
func (h *Handler) Stats() Stats {
	return Stats{
		Open:                h.open.Load(),
		Opened:              h.opened.Load(),
		RefusedInput:        h.refusedInput.Load(),
		RefusedNoOwner:      h.refusedNoOwner.Load(),
		RefusedAuthN:        h.refusedAuthN.Load(),
		RefusedSubjectKind:  h.refusedSubjectKind.Load(),
		RefusedSlot:         h.refusedSlot.Load(),
		RefusedSubjectQuota: h.refusedSubjectQuota.Load(),
		RefusedOwner:        h.refusedOwner.Load(),
		EventsSent:          h.eventsSent.Load(),
		ClosedByOwner:       h.closedByOwner.Load(),
	}
}

// CloseSubject закрывает все открытые потоки субъекта и возвращает их число.
//
// Читателя у отзыва двое, и оба живут на крае, а не здесь: толчок iam на
// внутреннем слушателе (`InternalAuthzCacheService.InvalidateSubject`) и
// перепрос изменений субъекта, которым узнают об отзыве ОСТАЛЬНЫЕ реплики —
// толчок доходит до одной. Ручка отдаёт им ОДНУ дверь и обходит один ключ
// реестра, а не ищет по горутинам.
//
// Ключ — субъект модели прав ([authz.TenantSubject]), тот же, которым отзыв
// называет субъекта. Совпадение обеспечено кодеком, а не написанием.
func (h *Handler) CloseSubject(subject string) int {
	return h.registry.closeSubject(subject)
}

// OpenStreams — открытые потоки этой реплики вместе с удостоверением каждого.
//
// Читателя у него один — перепрос состояния удостоверения на открытых
// соединениях (`gateway/internal/streamrevocation`, kacho#1410). Отдаётся снимок
// значениями, а не сам реестр: решение о закрытии принимает читатель, а
// устройство учёта остаётся здесь.
func (h *Handler) OpenStreams() []OpenStream {
	return h.registry.snapshot()
}

// CloseAll закрывает ВСЕ открытые потоки и возвращает их число.
//
// FAIL-CLOSED, и радиус у него намеренно широкий. Зовётся, когда край потерял
// читателя отзыва дольше объявленного срока: неполученный ответ авторитета не
// есть «прав ни у кого не отзывали», а кого именно закрывать, реплика в этом
// состоянии знать не может — имена приезжали как раз тем чтением, которого нет.
//
// Дешевле держать потоки нельзя: поток пережил бы отзыв ровно на время аварии
// соседа, то есть контроль отключался бы тем самым событием, ради которого он
// заведён.
func (h *Handler) CloseAll() int {
	return h.registry.closeAll()
}

// ServeHTTP — единственный вход проекции.
//
// # Порядок отказов, и он ЕСТЬ ПРЕДМЕТ
//
//  1. ОБЪЯВЛЕН ЛИ ВЛАДЕЛЕЦ ВООБЩЕ. Пустой словарь отвечает `501` с названной
//     причиной, а не «неизвестный владелец»: вызывающий не виноват в том, что
//     посадка владельца не объявила;
//  2. ФОРМА ЗАПРОСА. Терминальные отказы наступают ДО дозвона: иначе вызывающий
//     получает повторяемый код на ввод, который валидным не станет никогда;
//  3. ЛИЧНОСТЬ. Безымянному отвечает `401` — и ответ не зависит от посадки;
//  4. СЛОТ — расход ограниченного ресурса;
//  5. ДОЗВОН и ПЕРВОЕ СООБЩЕНИЕ владельца.
//
// # Заголовки пишутся ПОСЛЕ первого сообщения
//
// Порядок несущий: отказ владельца обязан стать кодом ответа, а код после
// отправки заголовков уже не изменить. Контракт гарантирует, что служебное
// сообщение открытия приходит первым и всегда, — поэтому дождаться его можно,
// ничего не отдав.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeRefusal(w, refusal{status: http.StatusMethodNotAllowed, code: 12,
			msg: "subscription stream is read-only; use GET"})
		return
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		// Ответ без сброса накопится в буфере и приедет одним куском в конце,
		// то есть перестанет быть потоком, ничем себя не выдав.
		h.log.Error("subscription stream refused: response writer cannot flush")
		writeRefusal(w, refusal{status: http.StatusInternalServerError, code: 13, msg: "internal error"})
		return
	}

	if len(h.cfg.Owners) == 0 {
		h.refusedNoOwner.Add(1)
		writeRefusal(w, refusal{status: http.StatusNotImplemented, code: 12,
			msg: "no journal owner is declared for this edge"})
		return
	}

	req, err := parseRequest(r, h.cfg.Owners)
	if err != nil {
		h.refusedInput.Add(1)
		writeRefusal(w, asRefusal(err))
		return
	}

	subject, callCtx, named := h.callerContext(r)
	if !named {
		// ДВА разных отказа, и различие обязано быть видно вызывающему.
		//
		// Безымянный получает `401`: личность здесь уже проверена полосой прав
		// (запись каталога объявлена `scope_filtered`, а это ТРЕБУЕТ названного
		// принципала), и проверка повторена потому, что страж у одной двери
		// переживает появление второго вызывающего — без личности владелец сузил
		// бы поток по правам КРАЯ, и арендатор увидел бы чужое.
		//
		// Названный, но не тенантный, получает `403`: он аутентифицирован, и
		// `401` посылал бы его аутентифицироваться заново — то есть отказ не
		// восстанавливал бы следующий шаг. Тот же код отдаёт на этот вход
		// владелец потока, и расходиться с ним крайю незачем.
		if r.Header.Get(principalmeta.HeaderPrincipalID) == "" &&
			r.Header.Get(principalmeta.HeaderGRPCMetaPrincipalID) == "" {
			h.refusedAuthN.Add(1)
			writeRefusal(w, refusal{status: http.StatusUnauthorized, code: 16,
				msg: "subscription requires an authenticated caller"})
			return
		}
		h.refusedSubjectKind.Add(1)
		writeRefusal(w, refusal{status: http.StatusForbidden, code: 7,
			msg: "subscription is available to user and service-account principals only"})
		return
	}

	// Удостоверение снимается ЗДЕСЬ и запоминается вместе с потоком.
	//
	// Не «на всякий случай»: без него отзыв удостоверения до открытого
	// соединения не доезжает НИ ПРИ КАКИХ УСЛОВИЯХ — спрашивать авторитет не о
	// чем (kacho#1410). Субъекта для этого недостаточно: отзыв прав называет
	// субъекта, а отзыв удостоверения — предъявленное, и два потока одного
	// человека могут быть открыты разными удостоверениями.
	//
	// Снимается ОДИН раз, при постановке на учёт: длинное соединение второго
	// запроса не делает, поэтому подменить своё удостоверение на живое
	// вызывающий не может by construction.
	cred := principalmeta.CredentialFromRequest(r)

	// Предел субъекта — ДО общего слота: место в очереди за общим ресурсом не
	// достаётся тому, кто своё уже выбрал. Обратный порядок дал бы субъекту
	// возможность занимать и отпускать общий слот на каждом отказе.
	entry, release, admitted := h.registry.tryAdd(subject, cred, h.cfg.MaxStreamsPerSubject)
	if !admitted {
		h.refusedSubjectQuota.Add(1)
		h.log.Warn("subscription stream refused: per-subject stream limit reached",
			"limit", h.cfg.MaxStreamsPerSubject)
		writeRefusal(w, exhausted(reasonSubjectLimit,
			"too many concurrent subscription streams for this caller (limit reached)"))
		return
	}
	defer release()

	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		h.refusedSlot.Add(1)
		h.log.Warn("subscription stream refused: concurrent stream limit reached",
			"limit", h.cfg.MaxStreams)
		writeRefusal(w, exhausted(reasonReplicaLimit,
			"too many concurrent subscription streams (limit reached)"))
		return
	}

	streamCtx, cancel := context.WithTimeout(callCtx, h.cfg.StreamBudget)
	defer cancel()
	entry.arm(cancel)

	h.stream(streamCtx, sse, req, r)
}

// stream ведёт один поток от дозвона до закрытия.
func (h *Handler) stream(ctx context.Context, sse *sseWriter, req parsed, r *http.Request) {
	client := h.cfg.Owners[req.owner]
	owned, err := client.Subscribe(ctx, req.req)
	if err != nil {
		h.refusedOwner.Add(1)
		writeRefusal(sse.w, h.ownerRefusal(err, req.owner))
		return
	}

	// ПЕРВОЕ сообщение — до заголовков: только здесь отказ владельца ещё может
	// стать кодом ответа.
	first, err := owned.Recv()
	if err != nil {
		h.refusedOwner.Add(1)
		writeRefusal(sse.w, h.ownerRefusal(err, req.owner))
		return
	}
	opened := first.GetOpened()
	if opened == nil {
		// Владелец нарушил собственный контракт: служебное сообщение открытия
		// приходит ПЕРВЫМ и ВСЕГДА. Отдавать поток дальше нельзя — клиенту не с
		// чего начинать позицию.
		h.refusedOwner.Add(1)
		h.log.Error("subscription stream refused: owner sent an event before the opened message",
			"owner", req.owner)
		writeRefusal(sse.w, refusal{status: http.StatusBadGateway, code: 13, msg: "internal error"})
		return
	}

	body, err := marshalMessage(first)
	if err != nil {
		h.refusedOwner.Add(1)
		h.log.Error("subscription stream refused: opened message is not serialisable",
			"owner", req.owner, "err", err)
		writeRefusal(sse.w, refusal{status: http.StatusBadGateway, code: 13, msg: "internal error"})
		return
	}

	sse.writeHead()
	h.open.Add(1)
	h.opened.Add(1)
	defer h.open.Add(-1)

	if err := sse.frame(eventOpened, opened.GetPosition(), body); err != nil {
		return
	}

	h.pump(ctx, sse, owned, req.owner, r)
}

// pump перекладывает сообщения владельца в кадры и держит связь живой.
//
// РЕПЛИКИ: запрос — петля живёт ровно столько, сколько открытый поток ОДНОГО
// вызывающего, и заводится его запросом. Реплика края, которую этот клиент не
// выбрал, петли не исполняет вовсе: дубля нет не потому, что он безвреден, а
// потому, что второго экземпляра не существует. Потолок одновременных петель на
// реплику — [Config.MaxStreams].
//
// Чтение владельца живёт в своей горутине, потому что `Recv` блокирует, а кадр
// поддержания связи обязан уходить и тогда, когда владелец молчит, — а молчание
// и есть обычный режим подписки.
func (h *Handler) pump(
	ctx context.Context,
	sse *sseWriter,
	owned subscriptionv1.InternalSubscriptionService_SubscribeClient,
	owner string,
	r *http.Request,
) {
	type received struct {
		msg *subscriptionv1.SubscriptionMessage
		err error
	}
	incoming := make(chan received, 1)
	go func() {
		for {
			msg, err := owned.Recv()
			select {
			case incoming <- received{msg: msg, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(h.cfg.Heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Срок потока истёк либо клиент ушёл. Закрываем ЧИСТО: клиент
			// возобновится со своей позиции, а обрыв ошибкой он записал бы как
			// сетевой сбой, которым это не является.
			return

		case <-r.Context().Done():
			return

		case <-ticker.C:
			if err := sse.heartbeat(); err != nil {
				return
			}

		case in := <-incoming:
			if in.err != nil {
				h.closedByOwner.Add(1)
				// Заголовки уже отданы, кода не изменить. Причина остаётся в
				// журнале процесса, а клиент видит закрытие потока и
				// возобновляется со своей позиции — тот же исход, что у
				// закрытия по сроку.
				if s, isStatus := status.FromError(in.err); !isStatus || s.Code() != codes.OK {
					h.log.Info("subscription stream closed by owner",
						"owner", owner, "code", status.Code(in.err).String())
				}
				return
			}
			event := in.msg.GetEvent()
			if event == nil {
				// Второе служебное сообщение открытия контрактом не
				// предусмотрено: оно приходит РОВНО ОДИН РАЗ за поток.
				h.log.Error("subscription stream closed: owner repeated the opened message", "owner", owner)
				return
			}
			body, err := marshalMessage(in.msg)
			if err != nil {
				// Состояние не сериализуется — у контракта на это есть СВОЁ
				// значение, и подставляется именно оно, а не выдуманное краем:
				// поток продолжается, а подписчик узнаёт, что по этому событию
				// состояния не будет.
				h.log.Warn("subscription event state is not serialisable; reported as unavailable",
					"owner", owner, "kind", event.GetKind(), "err", err)
				body, err = marshalMessage(withUnavailableState(in.msg))
				if err != nil {
					return
				}
			}
			if err := sse.frame(eventEvent, event.GetPosition(), body); err != nil {
				return
			}
			h.eventsSent.Add(1)
		}
	}
}

// callerContext достаёт личность вызывающего и собирает исходящие метаданные.
//
// Личность едет ЗАГОЛОВКАМИ, выставленными полосой аутентификации; клиентские
// заголовки того же пространства она вычищает до того, как запрос доходит сюда.
// Пустой принципал субъектом НЕ является: под ним владелец сузил бы поток по
// правам края.
//
// # Субъектом признаётся ТОЛЬКО тот, кого способен назвать отзыв
//
// Ключ учёта потока — субъект модели прав, и строит его тот же кодек, что
// спрашивает право у соседа ([authz.TenantSubject]). Это не аккуратность: поток,
// учтённый под строкой, которой в словаре модели не существует
// (`«:usr-x»`, `«workload:wid-x»`, `«sva:sva-x»`), закрыть по отзыву НЕЛЬЗЯ НИ
// ПРИ КАКИХ УСЛОВИЯХ — iam говорит о субъектах, а не о том, что край собрал из
// заголовков. Отсекается это ДО постановки на учёт и безусловно: отзываемость
// потока не вправе быть свойством того, что в этой посадке провязано.
//
// Ни одна работающая полоса от этого не теряется: владелец потока принимает тот
// же закрытый словарь и всякий иной субъект отвергает первым же условием — то
// есть такой поток и сегодня умирает, только тремя шагами позже и с чужим
// объяснением.
func (h *Handler) callerContext(r *http.Request) (subject string, ctx context.Context, ok bool) {
	md := principalmeta.MetadataFromRequest(r)
	ids := md.Get(principalmeta.MetaPrincipalID)
	types := md.Get(principalmeta.MetaPrincipalType)
	if len(ids) == 0 || len(types) == 0 {
		return "", nil, false
	}
	subject, named := authz.TenantSubject(types[0], ids[0])
	if !named {
		return "", nil, false
	}
	return subject, metadata.NewOutgoingContext(r.Context(), md), true
}

// ownerRefusal переводит отказ владельца в отказ края.
//
// # Что переносится дословно, а что нет
//
// Терминальные коды несут ТОН КОНТРАКТА владельца, и он часть договора: форма
// отсутствия проекта обязана совпасть дословно с обычным чтением, иначе
// различимый текст выдаёт существование чужого проекта. Поэтому такие отказы
// едут как есть, вместе с деталями, — по ним клиент ключуется машинно
// (`SUBSCRIPTION_POSITION_LOST` несёт возобновимую позицию).
//
// Нутряные коды текст НЕ несут: он приносит имена схемы, драйвера и адреса.
// Им подставляется фиксированный текст.
func (h *Handler) ownerRefusal(err error, owner string) refusal {
	st := status.Convert(err)
	httpStatus := runtime.HTTPStatusFromCode(st.Code())

	switch st.Code() {
	case codes.Internal, codes.Unknown, codes.DataLoss:
		h.log.Error("subscription owner refused the stream",
			"owner", owner, "code", st.Code().String())
		return refusal{status: httpStatus, code: statusCodeNumber(st.Code()), msg: "internal error"}
	case codes.Unavailable:
		h.log.Warn("subscription owner is unavailable", "owner", owner)
		return refusal{status: httpStatus, code: statusCodeNumber(st.Code()), msg: "subscription backend unavailable"}
	case codes.Unimplemented:
		// Владелец объявлен посадкой, но глагола не служит. Это состояние
		// посадки, а не вина вызывающего, — и оно обязано быть громким, иначе
		// «поток не работает» неотличимо от «событий нет».
		h.log.Error("subscription owner does not serve the platform subscription verb", "owner", owner)
		return refusal{status: httpStatus, code: statusCodeNumber(st.Code()),
			msg: "this owner does not serve the platform subscription verb"}
	default:
		return refusal{status: httpStatus, code: statusCodeNumber(st.Code()), msg: st.Message(), details: st.Proto().GetDetails()}
	}
}

// statusCodeNumber переводит код состояния в число тела отказа.
//
// Через приведение с зажимом, а не прямым `int32(...)`: код объявлен беззнаковым
// и по типу шире знакового, поэтому прямое приведение — переполнение, которое
// сборщик не запрещает, а проверка кода безопасности справедливо считает
// находкой. Зажим не меняет ни одного действительного кода (их шестнадцать) и
// снимает целый класс, вместо того чтобы объявлять его невозможным.
func statusCodeNumber(c codes.Code) int32 {
	return safeconv.ClampNonNegInt32(int64(c))
}

// marshalMessage сериализует сообщение потока в тело кадра.
//
// Регистр ключей — camelCase, тот же, что у всякого тела REST этого продукта:
// одна форма имени на обе поверхности. Незаполненные поля отдаются, как и на
// публичном mux'е, — подписчик, ведущий состояние, обязан видеть, что поле
// пусто, а не гадать, отдал его сервер или нет.
func marshalMessage(msg *subscriptionv1.SubscriptionMessage) (string, error) {
	body, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// withUnavailableState заменяет несериализуемое состояние признаком, который
// контракт для этого случая и завёл.
func withUnavailableState(msg *subscriptionv1.SubscriptionMessage) *subscriptionv1.SubscriptionMessage {
	event := msg.GetEvent()
	return &subscriptionv1.SubscriptionMessage{
		Message: &subscriptionv1.SubscriptionMessage_Event{
			Event: &subscriptionv1.SubscriptionEvent{
				Position:   event.GetPosition(),
				Kind:       event.GetKind(),
				ResourceId: event.GetResourceId(),
				ProjectId:  event.GetProjectId(),
				Change:     event.GetChange(),
				Carrier: &subscriptionv1.SubscriptionEvent_StateUnavailable_{
					StateUnavailable: &subscriptionv1.SubscriptionEvent_StateUnavailable{
						Reason: subscriptionv1.SubscriptionEvent_StateUnavailable_NOT_SERIALIZABLE,
					},
				},
			},
		},
	}
}

// Признаки полос исчерпания — то, на что клиенту РАЗРЕШЕНО ключеваться.
//
// Оба потолка отвечают `429` и кодом 8 (`RESOURCE_EXHAUSTED`), и различить их по
// коду нельзя by construction: код называет РОД отказа, а не его полосу. Между
// тем действие клиента у них противоположное — «подожди, место освободится»
// против «ты сам держишь свои потоки, закрой лишние», — и клиент, не умеющий их
// отличить, выберет неверное. Проза сообщения для этого не годится: тон текстов
// стабилен и является частью контракта, но разбирать его клиент не вправе.
//
// Та же дисциплина, что у полос резолва идентификаторов и у уже действующего
// признака утраченной позиции: полоса называется токеном в `google.rpc.ErrorInfo`.
const (
	// reasonReplicaLimit — исчерпан потолок ОДНОЙ реплики края. Состояние общего
	// ресурса: вызывающий не виноват, и повтор осмыслен.
	reasonReplicaLimit = "SUBSCRIPTION_REPLICA_STREAM_LIMIT"

	// reasonSubjectLimit — вызывающий выбрал СВОЙ предел. Повтор без закрытия
	// собственных потоков не пройдёт никогда, сколько бы ни ждать.
	reasonSubjectLimit = "SUBSCRIPTION_SUBJECT_STREAM_LIMIT"
)

// errorDomain — источник отказов этой поверхности.
//
// Тот же, что у отказов владельца, доезжающих сюда дословно
// (`SUBSCRIPTION_POSITION_LOST`), и это выбор, а не совпадение: у ручки один
// клиент, и заводить ему второй домен ради того, чтобы он различал край и
// владельца, значило бы просить различать то, на что он всё равно не реагирует.
// Полосу называет ТОКЕН; домен называет поверхность.
const errorDomain = "subscription.kacho.cloud"

// exhausted собирает отказ исчерпания с машинным признаком полосы.
//
// Признак ставится ЗДЕСЬ, а не у вызывающего: две полосы собираются в двух
// местах, и признак, проставляемый вручную, разошёлся бы с сообщением молча —
// ровно в том случае, когда полос станет три.
func exhausted(reason, msg string) refusal {
	r := refusal{status: http.StatusTooManyRequests, code: 8, msg: msg}
	detail, err := anypb.New(&errdetails.ErrorInfo{Reason: reason, Domain: errorDomain})
	if err != nil {
		// Признак не собрался — код и текст важнее детали, отдаём отказ без неё.
		return r
	}
	r.details = []*anypb.Any{detail}
	return r
}

// writeRefusal отдаёт отказ в той же форме, что и всякий другой отказ края:
// `{code, message, details}` из `google.rpc.Status`. Вторая форма ошибки
// заставила бы клиента разбирать две.
func writeRefusal(w http.ResponseWriter, r refusal) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	body := map[string]any{"code": r.code, "message": r.msg}
	if len(r.details) > 0 {
		encoded := make([]json.RawMessage, 0, len(r.details))
		for _, d := range r.details {
			raw, err := protojson.Marshal(d)
			if err != nil {
				continue
			}
			encoded = append(encoded, raw)
		}
		if len(encoded) > 0 {
			body["details"] = encoded
		}
	}
	_ = json.NewEncoder(w).Encode(body)
}
