// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package middleware: HTTPIdempotency — HTTP middleware для Idempotency-Key.
//
// На каждом mutating-запросе (POST/PATCH/PUT/DELETE) если задан header
// `Idempotency-Key: <uuid>` — сохраняется ответ (status + body), и при
// повторном запросе с тем же ключом возвращается сохраненный ответ без вызова
// downstream: тот же Idempotency-Key → тот же Operation.id.
//
// Кэш-ключ привязан к (principal, method, path, Idempotency-Key, sha256 тела),
// поэтому запись одного caller'а не может быть отдана другому principal'у или на
// другом маршруте.
//
// # ДОМЕН ПАРАЛЛЕЛИЗМА ЗАЩИТЫ = ФЛОТ, А НЕ ПРОЦЕСС (#694)
//
// Однократность — инвариант, и держать его обязан слой, охватывающий ВЕСЬ домен
// параллелизма, в котором её обходят (правило #10). Домен здесь — флот подов
// края, а не один процесс: посадка объявляет автомасштабирование, и повтор,
// попавший в соседнюю реплику, записи в чужой памяти не находит. Поэтому
// хранилище здесь — ИНТЕРФЕЙС с ровно одной атомарной точкой допуска
// (`Reserve`), а не структура: реализация в памяти процесса законна ровно для
// флота из одной реплики, для большего нужна общая (`internal/idempotencypg`).
// Пару «хранилище ↔ объявленный размер флота» сводит воедино отказ в старте
// (`gateway/cmd/api-gateway/idempotency_validation.go`); чарт рендерит размер
// флота из того же значения, что питает автомасштабирование, поэтому два
// объявления об одном предмете перестали существовать.
package middleware

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"google.golang.org/grpc/codes"
)

const (
	// IdempotencyTTL — время жизни записи. Погашение ключа живёт ровно столько,
	// после чего запись убирается сборщиком — иначе хранилище растёт без границы.
	IdempotencyTTL = 24 * time.Hour
	// idempotencyMaxEntries — потолок числа записей (FIFO-вытеснение) у
	// хранилища В ПАМЯТИ. Защищает от роста памяти, если caller шлет
	// mutating-запросы с уникальным ключом.
	idempotencyMaxEntries = 10000
	// idempotencyMaxBodyBytes — ответы крупнее не кэшируются (control-plane
	// ответы — это маленький Operation/ресурс; крупное тело кэшировать незачем,
	// и это убирает amplification-вектор).
	idempotencyMaxBodyBytes = 256 * 1024
	// IdempotencyWaitBudget — сколько ждать держателя брони, прежде чем ответить
	// вызывающему «ключ в работе». Ожидание ограничено намеренно: бесконечное
	// держало бы соединение столько, сколько живёт чужой запрос.
	IdempotencyWaitBudget = 5 * time.Second
	// IdempotencyLeaseTTL — срок брони. Держатель, умерший, не оставив исхода
	// (упавший под), освобождает ключ по истечении этого срока, а не навсегда.
	IdempotencyLeaseTTL = 2 * time.Minute
)

// IdempotencyRecord — сохранённый ответ. Content-Type — единственный заголовок,
// который восстанавливается при повторе: он один и захватывается.
type IdempotencyRecord struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

// IdempotencyOutcome — исход атомарного допуска по ключу. Их РОВНО ТРИ, и
// четвёртого нет: либо ответ уже есть, либо исполняем мы, либо исполняет другой.
type IdempotencyOutcome int

const (
	// IdempotencyReplay — по ключу есть законченная запись; её и отдаём.
	IdempotencyReplay IdempotencyOutcome = iota
	// IdempotencyOwn — бронь наша: downstream исполняем МЫ, ровно один раз.
	// Держатель ОБЯЗАН ровно один раз вызвать Commit либо Release.
	IdempotencyOwn
	// IdempotencyWait — бронь у другого предъявителя (в этом процессе или в
	// соседней реплике): ждём его исхода, downstream не зовём.
	IdempotencyWait
)

// IdempotencyReservation — результат допуска. Lease непрозрачен для середины:
// хранилище выдаёт его и требует обратно, чтобы Commit/Release не мог применить
// тот, чью бронь уже перехватили по истечении срока.
type IdempotencyReservation struct {
	Key     string
	Outcome IdempotencyOutcome
	Record  IdempotencyRecord
	Lease   any
}

// IdempotencyAwaitOutcome — чем кончилось ожидание держателя брони.
type IdempotencyAwaitOutcome int

const (
	// IdempotencyAwaitReplay — держатель оставил исход; отдаём его.
	IdempotencyAwaitReplay IdempotencyAwaitOutcome = iota
	// IdempotencyAwaitVacant — держатель ушёл, НЕ оставив исхода (паника, 5xx,
	// смерть пода): ключ свободен, вызывающий исполняет downstream сам.
	IdempotencyAwaitVacant
	// IdempotencyAwaitBusy — держатель всё ещё работает, бюджет ожидания исчерпан.
	// Исполнять downstream НЕЛЬЗЯ — это и было бы вторым исполнением.
	IdempotencyAwaitBusy
)

// IdempotencyAwait — исход ожидания.
type IdempotencyAwait struct {
	Outcome IdempotencyAwaitOutcome
	Record  IdempotencyRecord
}

// IdempotencyStore — хранилище однократности.
//
// Контракт: Reserve — ЕДИНСТВЕННАЯ точка допуска, и она атомарна. Раздельные
// «посмотреть» и «записать» здесь запрещены (правило #10): под конкуренцией они
// пропускают обоих предъявителей, и это ровно тот дефект, ради которого
// хранилище существует.
type IdempotencyStore interface {
	// Reserve атомарно разрешает ключ ровно в один из трёх исходов.
	Reserve(ctx context.Context, key string) (IdempotencyReservation, error)
	// Commit — держатель записывает исход и снимает бронь. keep=false означает
	// «исход есть, но хранить его нельзя» (5xx / слишком большое тело): ждущие
	// его получат, а следующий предъявитель — нет.
	Commit(ctx context.Context, res IdempotencyReservation, rec IdempotencyRecord, keep bool)
	// Release — держатель снимает бронь, не оставив исхода.
	Release(ctx context.Context, res IdempotencyReservation)
	// Await — ждущий дожидается исхода держателя, не дольше ctx.
	Await(ctx context.Context, res IdempotencyReservation) IdempotencyAwait
}

// idempotencyEntry хранит сохраненный response вместе со сроком годности.
type idempotencyEntry struct {
	record    IdempotencyRecord
	expiresAt time.Time
}

// idempotencyItem — значение элемента FIFO-списка: ключ + запись.
type idempotencyItem struct {
	key   string
	entry idempotencyEntry
}

// idempotencyFlight — бронь ключа в этом процессе. Первый (держатель)
// регистрирует бронь, остальные ждут `done` и повторяют захваченный им ответ,
// поэтому мутирующий downstream исполняется ровно один раз на пачку
// одновременных предъявлений (закрывает check-then-act TOCTOU, CWE-362).
type idempotencyFlight struct {
	done      chan struct{}
	hasResult bool
	record    IdempotencyRecord
}

// MemoryIdempotencyStore — хранилище В ПАМЯТИ ПРОЦЕССА: TTL, потолок ёмкости,
// фоновый сборщик.
//
// ЗАКОННО РОВНО ДЛЯ ФЛОТА ИЗ ОДНОЙ РЕПЛИКИ. Второй под этих записей не видит,
// поэтому повтор, попавший в него, проходит к downstream. Пару «этот store ↔
// объявленный размер флота» держит отказ в старте
// (validateIdempotencyFleetPairing), а не комментарий: комментарий здесь уже
// стоял, был верен и не был связан ни с чем (#694).
type MemoryIdempotencyStore struct {
	mu         sync.Mutex
	elems      map[string]*list.Element      // key → *list.Element{Value: *idempotencyItem}
	order      *list.List                    // FIFO insertion order для вытеснения
	inflight   map[string]*idempotencyFlight // key → бронь
	ttl        time.Duration
	maxEntries int
}

// NewIdempotencyStore создает in-memory store с фоновым GC и стандартной емкостью.
func NewIdempotencyStore(ttl time.Duration) *MemoryIdempotencyStore {
	return newIdempotencyStoreWithCap(ttl, idempotencyMaxEntries)
}

// newIdempotencyStoreWithCap — конструктор с явной емкостью (для тестов).
func newIdempotencyStoreWithCap(ttl time.Duration, maxEntries int) *MemoryIdempotencyStore {
	if maxEntries <= 0 {
		maxEntries = idempotencyMaxEntries
	}
	s := &MemoryIdempotencyStore{
		elems:      make(map[string]*list.Element),
		order:      list.New(),
		inflight:   make(map[string]*idempotencyFlight),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
	go s.gcLoop()
	return s
}

// Reserve — атомарная точка допуска под одним удержанием замка.
func (s *MemoryIdempotencyStore) Reserve(_ context.Context, key string) (IdempotencyReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.elems[key]; ok {
		it := e.Value.(*idempotencyItem)
		if !time.Now().After(it.entry.expiresAt) {
			return IdempotencyReservation{Key: key, Outcome: IdempotencyReplay, Record: it.entry.record}, nil
		}
		s.removeElem(e)
	}
	if fl, ok := s.inflight[key]; ok {
		return IdempotencyReservation{Key: key, Outcome: IdempotencyWait, Lease: fl}, nil
	}
	fl := &idempotencyFlight{done: make(chan struct{})}
	s.inflight[key] = fl
	return IdempotencyReservation{Key: key, Outcome: IdempotencyOwn, Lease: fl}, nil
}

// Commit записывает исход держателя на бронь (чтобы ждущие его повторили),
// при keep — кладёт в долговременное хранилище, снимает бронь и будит ждущих.
func (s *MemoryIdempotencyStore) Commit(_ context.Context, res IdempotencyReservation, rec IdempotencyRecord, keep bool) {
	fl, ok := res.Lease.(*idempotencyFlight)
	if !ok {
		return
	}
	s.mu.Lock()
	fl.record = rec
	fl.hasResult = true
	if keep {
		s.putLocked(res.Key, idempotencyEntry{record: rec, expiresAt: time.Now().Add(s.ttl)})
	}
	if cur, exists := s.inflight[res.Key]; exists && cur == fl {
		delete(s.inflight, res.Key)
	}
	s.mu.Unlock()
	close(fl.done)
}

// Release снимает бронь без исхода. Ждущие просыпаются и исполняют downstream
// сами.
func (s *MemoryIdempotencyStore) Release(_ context.Context, res IdempotencyReservation) {
	fl, ok := res.Lease.(*idempotencyFlight)
	if !ok {
		return
	}
	s.mu.Lock()
	if cur, exists := s.inflight[res.Key]; exists && cur == fl {
		delete(s.inflight, res.Key)
		s.mu.Unlock()
		close(fl.done)
		return
	}
	s.mu.Unlock()
}

// Await ждёт держателя брони этого процесса.
func (s *MemoryIdempotencyStore) Await(ctx context.Context, res IdempotencyReservation) IdempotencyAwait {
	fl, ok := res.Lease.(*idempotencyFlight)
	if !ok {
		return IdempotencyAwait{Outcome: IdempotencyAwaitVacant}
	}
	select {
	case <-fl.done:
		if fl.hasResult {
			return IdempotencyAwait{Outcome: IdempotencyAwaitReplay, Record: fl.record}
		}
		return IdempotencyAwait{Outcome: IdempotencyAwaitVacant}
	case <-ctx.Done():
		return IdempotencyAwait{Outcome: IdempotencyAwaitBusy}
	}
}

// gcLoop удаляет expired entries раз в ttl/24 (но не реже минуты).
func (s *MemoryIdempotencyStore) gcLoop() {
	tick := s.ttl / 24
	if tick < time.Minute {
		tick = time.Minute
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		s.mu.Lock()
		for e := s.order.Front(); e != nil; {
			next := e.Next()
			if now.After(e.Value.(*idempotencyItem).entry.expiresAt) {
				s.removeElem(e)
			}
			e = next
		}
		s.mu.Unlock()
	}
}

// removeElem снимает элемент из списка и map. Caller держит s.mu.
func (s *MemoryIdempotencyStore) removeElem(e *list.Element) {
	it := e.Value.(*idempotencyItem)
	s.order.Remove(e)
	delete(s.elems, it.key)
}

// Len возвращает текущее число записей.
func (s *MemoryIdempotencyStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.elems)
}

// get возвращает сохраненный entry или (zero, false) если ключа нет/expired.
func (s *MemoryIdempotencyStore) get(key string) (idempotencyEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.elems[key]
	if !ok {
		return idempotencyEntry{}, false
	}
	it := e.Value.(*idempotencyItem)
	if time.Now().After(it.entry.expiresAt) {
		s.removeElem(e)
		return idempotencyEntry{}, false
	}
	return it.entry, true
}

// put сохраняет entry с TTL, вытесняя самую старую запись при достижении лимита.
func (s *MemoryIdempotencyStore) put(key string, entry idempotencyEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putLocked(key, entry)
}

// putLocked — общий insert-путь. Caller держит s.mu.
func (s *MemoryIdempotencyStore) putLocked(key string, entry idempotencyEntry) {
	if e, ok := s.elems[key]; ok {
		e.Value.(*idempotencyItem).entry = entry
		s.order.MoveToBack(e)
		return
	}
	for len(s.elems) >= s.maxEntries {
		if front := s.order.Front(); front != nil {
			s.removeElem(front)
		} else {
			break
		}
	}
	s.elems[key] = s.order.PushBack(&idempotencyItem{key: key, entry: entry})
}

// HTTPIdempotency — HTTP middleware: при наличии Idempotency-Key на mutating
// request кэширует ответ или возвращает сохраненный. GET и запросы без ключа
// проходят насквозь. Ответ кэшируется при status < 500 (5xx не кэшируем —
// retry-safety) и теле не больше idempotencyMaxBodyBytes.
//
// Ключ кэша — fingerprint запроса (principal, method, path, Idempotency-Key,
// sha256 тела): middleware смонтирован после authN/authZ, поэтому
// principal-заголовки уже проставлены, и запись одного caller'а не может быть
// отдана другому.
//
// ОТКАЗ ХРАНИЛИЩА — FAIL-CLOSED. Вызывающий, приславший ключ, ПОПРОСИЛ
// однократность. Если хранилище недоступно, дать её нечем — и тихо исполнить
// мутацию значило бы ответить успехом на просьбу, которую мы не выполнили
// (запрещённый класс «принято-и-проигнорировано»). Отвечаем 503 и называем
// причину; повтор осмыслен. Запросы БЕЗ ключа этим не задеты вовсе.
func HTTPIdempotency(store IdempotencyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isMutating(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			idemKey := r.Header.Get("Idempotency-Key")
			if idemKey == "" {
				next.ServeHTTP(w, r)
				return
			}
			key := idempotencyCacheKey(r, idemKey)

			// Единственная точка допуска, и она атомарна. Здесь закрывается
			// check-then-act, при котором два одновременных предъявления одного
			// ключа оба промахиваются мимо записи и оба мутируют (CWE-362).
			res, err := store.Reserve(r.Context(), key)
			if err != nil {
				writeIdempotencyStoreUnavailable(w)
				return
			}
			switch res.Outcome {
			case IdempotencyReplay:
				replayIdempotent(w, res.Record)
				return
			case IdempotencyWait:
				waitCtx, cancel := context.WithTimeout(r.Context(), IdempotencyWaitBudget)
				got := store.Await(waitCtx, res)
				cancel()
				switch got.Outcome {
				case IdempotencyAwaitReplay:
					replayIdempotent(w, got.Record)
				case IdempotencyAwaitVacant:
					// Держатель ушёл, не оставив исхода — ключ свободен,
					// исполняем сами (best-effort, как и было).
					next.ServeHTTP(w, r)
				default:
					writeIdempotencyInFlight(w)
				}
				return
			}

			// Полоса держателя. Бронь снимается ВСЕГДА, даже если downstream
			// паникует: иначе ждущие висят до истечения срока брони.
			finished := false
			defer func() {
				if !finished {
					store.Release(context.WithoutCancel(r.Context()), res)
				}
			}()
			rec := &responseRecorder{ResponseWriter: w, body: &bytes.Buffer{}, statusCode: 200}
			next.ServeHTTP(rec, r)
			answer := IdempotencyRecord{
				StatusCode:  rec.statusCode,
				ContentType: w.Header().Get("Content-Type"),
				Body:        rec.body.Bytes(),
			}
			// Долговременно храним только не-5xx в пределах потолка (5xx
			// retry-safe; огромные тела держали бы память). Ждущие этой пачки
			// повторяют захваченный исход в любом случае, поэтому пачка делит
			// одно исполнение downstream.
			keep := rec.statusCode < 500 && rec.body.Len() <= idempotencyMaxBodyBytes
			// Запись исхода не должна отменяться вместе с запросом вызывающего:
			// он мог отсоединиться, но downstream уже исполнен, и следующий
			// предъявитель обязан получить именно этот ответ.
			store.Commit(context.WithoutCancel(r.Context()), res, answer, keep)
			finished = true
		})
	}
}

// replayIdempotent writes a stored/captured response to w with the
// X-Idempotent-Replayed marker.
func replayIdempotent(w http.ResponseWriter, rec IdempotencyRecord) {
	if rec.ContentType != "" {
		w.Header().Set("Content-Type", rec.ContentType)
	}
	w.Header().Set("X-Idempotent-Replayed", "true")
	w.WriteHeader(rec.StatusCode)
	_, _ = w.Write(rec.Body)
}

// writeIdempotencyInFlight — ответ ждущему, чей держатель не уложился в бюджет.
// 409/ABORTED, а не исполнение: исполнить значило бы сделать мутацию дважды —
// ровно то, что заголовок и запрещает. Повтор осмыслен: держатель закончит и
// следующее предъявление получит его ответ.
func writeIdempotencyInFlight(w http.ResponseWriter) {
	writeIdempotencyStatus(w, http.StatusConflict, codes.Aborted,
		"a request with this Idempotency-Key is already in flight")
}

// writeIdempotencyStoreUnavailable — ответ, когда общее хранилище недоступно.
// 503/UNAVAILABLE: однократность обещана и не может быть обеспечена, значит
// мутация не исполняется. Fail-closed для мутаций.
func writeIdempotencyStoreUnavailable(w http.ResponseWriter) {
	writeIdempotencyStatus(w, http.StatusServiceUnavailable, codes.Unavailable,
		"idempotency store is unavailable; the exactly-once guarantee cannot be honoured")
}

// writeIdempotencyStatus печатает тело в форме grpc-gateway ({code,message,details}),
// чтобы клиент разбирал отказ края тем же кодом, что и отказ сервиса.
func writeIdempotencyStatus(w http.ResponseWriter, httpStatus int, code codes.Code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(struct {
		Code    int      `json:"code"`
		Message string   `json:"message"`
		Details []string `json:"details"`
	}{Code: int(code), Message: msg, Details: []string{}})
}

// idempotencyCacheKey строит fingerprint запроса и СВОРАЧИВАЕТ его в sha256.
//
// Прообраз — (principal, метод, путь, Idempotency-Key, sha256 тела) через
// NUL-разделитель: NUL исключает коллизии склейки между сегментами и не может
// прийти из значения заголовка, потому что заголовок его не переносит. В ключ
// входит sha256 тела запроса: повтор того же Idempotency-Key с ДРУГИМ payload'ом
// становится cache-miss (выполняется downstream), а не молчаливым replay'ем
// первого ответа (masked lost-update, CWE-694). Тело читается capped и
// восстанавливается для downstream.
//
// СВОРАЧИВАНИЕ В ХЭШ — не украшение, и держится тремя доводами:
//
//  1. ключ уезжает в ОБЩЕЕ хранилище флота, а прообраз содержит идентификатор
//     вызывающего и путь ресурса — при свёртке в базе не лежит ни того, ни
//     другого, и утечка снимка хранилища не говорит, кто что создавал;
//  2. длина ключа перестаёт зависеть от вызывающего: `Idempotency-Key` — его
//     значение, и без свёртки размер строки в хранилище задаёт он;
//  3. NUL в прообразе делал ключ непредставимым для текстовой колонки Postgres
//     (`invalid byte sequence 0x00`). Свёртка снимает это by construction, не
//     ослабляя разделитель.
//
// Различительная способность не меняется: разные прообразы дают разные ключи с
// точностью до стойкости sha256.
func idempotencyCacheKey(r *http.Request, idemKey string) string {
	principal := r.Header.Get(principalmeta.HeaderPrincipalID)
	preimage := principal + "\x00" + r.Method + "\x00" + r.URL.Path + "\x00" + idemKey +
		"\x00" + hashRequestBody(r)
	sum := sha256.Sum256([]byte(preimage))
	return hex.EncodeToString(sum[:])
}

// hashRequestBody возвращает hex(sha256) первых idempotencyMaxBodyBytes тела
// запроса и ВОССТАНАВЛИВАЕТ r.Body так, чтобы downstream прочитал полное тело.
// Cap совпадает с cap кэшируемого ответа: control-plane тела маленькие, а
// ограничение убирает amplification при огромном payload'е. Разные тела,
// совпадающие в пределах cap, коллизируют по хэшу — приемлемо (та же семантика,
// что и для размера кэшируемого ответа).
func hashRequestBody(r *http.Request) string {
	if r.Body == nil || r.Body == http.NoBody {
		return ""
	}
	orig := r.Body
	head, _ := io.ReadAll(io.LimitReader(orig, idempotencyMaxBodyBytes))
	// Восстановить поток: буфер прочитанной головы + возможный непрочитанный хвост.
	r.Body = &restoredBody{Reader: io.MultiReader(bytes.NewReader(head), orig), closer: orig}
	sum := sha256.Sum256(head)
	return hex.EncodeToString(sum[:])
}

// restoredBody — io.ReadCloser: читает из восстановленного MultiReader, но Close
// делегирует оригинальному телу (закрытие исходного соединения/reader'а).
type restoredBody struct {
	io.Reader
	closer io.Closer
}

func (b *restoredBody) Close() error { return b.closer.Close() }

// isMutating — true если HTTP метод изменяет state.
func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

// responseRecorder перехватывает status + body для кеширования.
type responseRecorder struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
	wroteHdr   bool
}

func (r *responseRecorder) WriteHeader(code int) {
	if r.wroteHdr {
		return
	}
	r.wroteHdr = true
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHdr {
		r.WriteHeader(http.StatusOK)
	}
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
