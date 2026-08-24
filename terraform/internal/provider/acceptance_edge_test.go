// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Поддельный край Kachō — общий каркас приёмочных проб провайдера.
//
// # Зачем он существует
//
// До него единственным доказательством работоспособности ресурса был ручной прогон на
// живом стенде: он не воспроизводится, не идёт в конвейере, требует секрета и упирается в
// одного писателя. Семнадцать из двадцати пяти ресурсов такого прогона не имели вовсе.
// Каркас закрывает не эти четыре ресурса, а способ проверять любой: новый вид
// регистрируется одной структурой edgeKind рядом со своими пробами.
//
// # ЧТО он воспроизводит — и почему именно это
//
// Каждое свойство ниже уже стоило отладки на живом крае (см. terraform/doc.go). Подделка,
// в которой их нет, зеленела бы ровно на том, что в жизни падает.
//
//  1. МУТАЦИИ АСИНХРОННЫ. POST/PATCH/DELETE отвечают операцией; идентификатор ресурса
//     лежит в её МЕТАДАННЫХ; операция становится завершённой НЕ на первом опросе
//     (edgeOpPollsBeforeDone). Идентификатор присутствует в метаданных и у операции,
//     завершившейся ОТКАЗОМ, — иначе провайдер, читающий метаданные до проверки отказа,
//     выглядел бы исправным.
//  2. «НЕ НАЙДЕНО» ПРИ ОТКАЗЕ В ДОСТУПЕ ПОБАЙТОВО РАВНО НАСТОЯЩЕМУ ОТСУТСТВИЮ. Тело того
//     и другого производит ОДНА функция (edgeNotFoundBody) — две копии разошлись бы, и
//     проба на неразличимость стала бы формой без содержания.
//  3. КЛЮЧ ИДЕМПОТЕНТНОСТИ СОБЛЮДАЕТСЯ: тот же ключ отдаёт ТУ ЖЕ операцию и второго
//     объекта не создаёт; другой ключ и его отсутствие идут обычным путём.
//  4. НУЛИ И 64-РАЗРЯДНЫЕ ЦЕЛЫЕ КАК У protojson: незаданное поле приезжает нулём
//     (пустая строка, пустая карта, пустой массив), 64-разрядное целое — СТРОКОЙ,
//     32-разрядное — числом. Одна структура ответа несёт оба вида.
//  5. ПОРЯДОК ЭЛЕМЕНТОВ НЕ СОХРАНЯЕТСЯ. Поля-наборы край отдаёт в своём порядке —
//     выдачу вид объявляет сам (edgeKind.ReadView), и настройка не вправе на порядок
//     закладываться.
//  6. СЛЕДСТВИЕ МУТАЦИИ ВИДНО НЕ СРАЗУ. Завершённость операции означает долговечность
//     предмета мутации, а не видимость её следствия: состав группы целей сходится через
//     несколько чтений (edgeRow.staleReads). Это контракт платформы, а не дефект.
//  7. ОТКАЗ ПРИЕЗЖАЕТ КОНВЕРТОМ google.rpc.Status с блоком details — с ИМЕНЕМ поля,
//     которое край отверг. Без блока отказ не называет ни одного поля, и проверять
//     нечего.
//
// # Чего он НАМЕРЕННО не воспроизводит
//
//  1. АВТОРИЗАЦИЮ. Токен не проверяется вовсе. Решение о доступе принимает модель прав
//     края; подделка «да/нет» доказывала бы только саму подделку, а не право. Отказ в
//     доступе здесь ЗАКАЗЫВАЕТСЯ пробой (denyList / hideRead / rejectCreate /
//     rejectDelete) как СОБЫТИЕ, потому что предмет проверки — поведение ПРОВАЙДЕРА
//     на таком ответе.
//  2. ВАЛИДАЦИЮ ТЕЛА ПО СУЩЕСТВУ (форма блока адресов, диапазоны портов, совпадение
//     зон). Край-подделка, повторяющая её, стала бы второй реализацией контракта и
//     разошлась бы с настоящей молча — ровно там, где расхождение не видно. Отказ по
//     существу проба ЗАКАЗЫВАЕТ (rejectCreate), называя поле и текст.
//  3. TLS и mTLS: httptest поднимается по HTTP. Проверка доверия к краю живёт в
//     internal/client и проверяется там.
//  4. КОНКУРЕНТНОСТЬ КРАЯ: весь сервер под одной блокировкой, гонок в нём нет by
//     construction. Гонки — предмет проб самого края, а не провайдера.
//  5. НАСТОЯЩИЕ СРОКИ: операция завершается на втором опросе, а не через минуты.
//  6. ПОСТРАНИЧНЫЙ ОБХОД: список отдаёт одну страницу и пустой курсор. Провайдер обхода
//     и не делает — его контрольный вопрос к списку звучит «есть ли в области хоть
//     что-нибудь этого вида».
//  7. ПОЛНОТУ КАТАЛОГА: зарегистрировано столько видов, сколько нужно пробам.
//     Незарегистрированный путь отвечает ОТКАЗОМ, НАЗЫВАЮЩИМ путь, а не пустым успехом:
//     иначе первая же опечатка в адресе давала бы зелёную пробу по несуществующему краю.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// edgeOpPollsBeforeDone — на каком по счёту опросе операция объявляется завершённой.
//
// Двойка, а не единица: провайдер обязан УМЕТЬ ждать. При завершении на первом же опросе
// проба зеленела бы и на провайдере, который читает метаданные прямо из ответа мутации, —
// то есть не проверяла бы асинхронность вовсе.
const edgeOpPollsBeforeDone = 2

// edgeLagTargetComposition — лаг видимости состава группы целей, в чтениях.
//
// Замер на живом крае: операция снятия цели объявляется завершённой через ~0,2 с, а сама
// цель исчезает из чтения через ~1,3 с. Двух чтений при паузе ожидания 500 мс достаточно,
// чтобы отличить провайдера, который ждёт сходимости, от того, который сдаётся на первом
// же ответе. Значение живёт здесь, а не у вида, чтобы замер и его следствие стояли рядом.
const edgeLagTargetComposition = 2

// edgeObject — тело ресурса в том виде, в каком край его отдаёт.
type edgeObject = map[string]any

// edgeStatus — конверт google.rpc.Status. Поле Field заполняется, когда край называет
// негодное поле: без него отказ не даёт ни одного основания для правки настройки.
type edgeStatus struct {
	HTTP     int
	Code     int
	Message  string
	Field    string
	FieldWhy string
}

func (s edgeStatus) body() []byte {
	env := map[string]any{"code": s.Code, "message": s.Message}
	if s.Field != "" {
		env["details"] = []any{map[string]any{
			"@type": "type.googleapis.com/google.rpc.BadRequest",
			"fieldViolations": []any{map[string]any{
				"field": s.Field, "description": s.FieldWhy,
			}},
		}}
	}
	raw, err := json.Marshal(env)
	if err != nil {
		// Недостижимо: карта строк и чисел всегда сериализуется. Паника здесь честнее
		// молчаливого пустого тела — оно превратилось бы в «ответ не края» и увело
		// разбор в сторону настройки адреса.
		panic("поддельный край: конверт отказа не сериализован: " + err.Error())
	}
	return raw
}

// edgeKind — как поддельный край обслуживает ОДИН вид ресурса.
//
// Здесь и проходит шов расширения: двадцать один оставшийся ресурс добавляется такой же
// структурой рядом со своими пробами, а каркас не меняется.
type edgeKind struct {
	// Path — путь коллекции. Сегмент «{}» означает значение, которое назначает
	// вызывающий: у ключей служебной учётки коллекция вложена в владельца.
	Path string
	// Name — как край называет вид в тексте «<Вид> <id> not found». Часть контракта:
	// провайдер сверяет отсутствие по этому тону.
	Name string
	// IDPrefix — префикс идентификатора; форму проверяет общий каталог платформы, и
	// импорт без неё не пройдёт.
	IDPrefix string
	// Hyphen — идентификатор в дефисной форме («ins-…»), а не в слитной.
	Hyphen bool
	// MetadataKey — имя поля идентификатора в МЕТАДАННЫХ операции создания.
	MetadataKey string
	// ListKey — имя массива в списочном ответе.
	ListKey string
	// Scope — имя параметра области в списочном запросе («projectId», «accountId»).
	// Пусто у вложенной коллекции: её область задана самим путём.
	Scope string

	// Create строит хранимое представление из тела запроса. Ошибка означает отказ края.
	Create func(e *fakeEdge, id string, req edgeObject) (edgeObject, error)
	// Update применяет ОДНО поле маски изменения. Неизвестное имя — отказ края.
	Update func(e *fakeEdge, obj, req edgeObject, field string) error
	// Verbs — суффикс-действия края, ключ БЕЗ двоеточия («add-cidr-blocks», «addTargets»).
	Verbs map[string]func(e *fakeEdge, row *edgeRow, req edgeObject) error
	// LagAfterVerb — сколько чтений подряд после суффикс-действия отдают ПРЕЖНЕЕ
	// следствие. Ноль означает «следствие видно сразу».
	//
	// Задаётся ПОВИДОВО, а не общим умолчанием, и это не осторожность: лаг измерен на
	// составе группы целей (операция снятия завершается через ~0,2 с, цель исчезает из
	// чтения через ~1,3 с), а у супернета сети та же пара действий видна сразу. Общий
	// лаг был бы выдумкой, и провайдер сети, работающий верно, краснел бы на свойстве,
	// которого у его края нет.
	LagAfterVerb int
	// Delete — предусловие удаления. Ошибка означает отказ края (непустая группа целей).
	Delete func(e *fakeEdge, row *edgeRow) error
	// OpResponse — полезная нагрузка УСПЕШНОЙ операции создания. Нужна там, где мутация
	// возвращает то, что больше нигде не прочитать (выпуск ключа служебной учётки).
	OpResponse func(row *edgeRow) edgeObject
	// ReadView — что отдать чтением поверх хранимого. Пусто означает «сам объект».
	// Существует ради свойств, которые край добавляет на выдаче: порядок элементов
	// набора он не сохраняет.
	ReadView func(obj edgeObject) edgeObject
	// Seed — строки, существующие у края до первой пробы (каталог типов машин).
	Seed []edgeObject
}

// edgeRow — одна строка у поддельного края.
type edgeRow struct {
	kind *edgeKind
	// base — путь коллекции с подставленными сегментами. У вложенной коллекции он и есть
	// область: два владельца не видят ключи друг друга.
	base string
	id   string
	seq  int
	obj  edgeObject

	// stale — прежнее следствие суффикс-действия и сколько чтений его ещё отдавать.
	stale      edgeObject
	staleReads int
}

type edgeOp struct {
	id       string
	polls    int
	metadata edgeObject
	response edgeObject
	failure  *edgeStatus
}

// fakeEdge — поддельный край. Создаётся newFakeEdge, закрывается t.Cleanup.
type fakeEdge struct {
	t   *testing.T
	srv *httptest.Server

	mu    sync.Mutex
	kinds []*edgeKind
	rows  map[string]*edgeRow // id → строка
	ops   map[string]*edgeOp
	idem  map[string]string // ключ идемпотентности → идентификатор операции
	seq   int

	// Записи о том, ЧТО провайдер делал. Проба утверждает по ним порядок и состав
	// действий: без записи «отправлено» неотличимо от «не отправлено».
	verbLog      []string
	methodLog    []string
	createsSeen  int
	createsKeyed int
	readsByID    map[string]int
	idemReplayed int

	// Заказанные пробой события края.
	hideRead     map[string]bool
	hideList     map[string]bool
	denyList     map[string]bool // ключ — путь коллекции
	rejectCreate map[string]*edgeStatus
	rejectDelete map[string]*edgeStatus
}

func newFakeEdge(t *testing.T, kinds ...*edgeKind) *fakeEdge {
	t.Helper()
	e := &fakeEdge{
		t:            t,
		kinds:        kinds,
		rows:         map[string]*edgeRow{},
		ops:          map[string]*edgeOp{},
		idem:         map[string]string{},
		readsByID:    map[string]int{},
		hideRead:     map[string]bool{},
		hideList:     map[string]bool{},
		denyList:     map[string]bool{},
		rejectCreate: map[string]*edgeStatus{},
		rejectDelete: map[string]*edgeStatus{},
	}
	for _, k := range kinds {
		for _, seed := range k.Seed {
			id, _ := seed["id"].(string)
			if id == "" {
				t.Fatalf("поддельный край: посев вида %s без идентификатора", k.Path)
			}
			e.seq++
			e.rows[id] = &edgeRow{kind: k, base: k.Path, id: id, seq: e.seq, obj: seed}
		}
	}
	e.srv = httptest.NewServer(http.HandlerFunc(e.handle))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *fakeEdge) URL() string { return e.srv.URL }

// ---- заказ событий края ---------------------------------------------------------------
//
// Каждый заказ — ОТДЕЛЬНОЕ событие, а не общий «сломайся». Их различие и есть предмет
// проверки: провайдер обязан по-разному отвечать на «ресурса нет», «доступ к области
// отозван» и «различить нечем».

// HideFromRead — пообъектное чтение отвечает «не найдено». Список объект по-прежнему
// показывает: это ОКНО ПРАВ, а не удаление.
func (e *fakeEdge) HideFromRead(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hideRead[id] = true
}

// Reveal снимает сокрытие пообъектного чтения: доступ вернулся.
//
// Нужен, чтобы проба могла получить ОБА ответа про ОДИН И ТОТ ЖЕ идентификатор — сперва
// отказ, затем настоящее отсутствие. Сравнивать ответы про разные идентификаторы
// бессмысленно: они и должны различаться, потому что называют разные строки.
func (e *fakeEdge) Reveal(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.hideRead, id)
}

// HideFromList — объекта не видно и в списке. Вместе с HideFromRead это НАСТОЯЩЕЕ
// удаление — при условии, что в области остался хоть кто-то ещё.
func (e *fakeEdge) HideFromList(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hideList[id] = true
}

// DenyList — список коллекции отвечает отказом: доступ к области утрачен.
func (e *fakeEdge) DenyList(collection string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.denyList[collection] = true
}

// AllowList — доступ к списку коллекции вернулся.
//
// Нужен, чтобы проба могла утверждать не только отказ, но и ЕГО ПОСЛЕДСТВИЕ: пережил ли
// ресурс событие прав. Без возврата доступа состояние после отказа нечем прочитать, и
// проба поневоле останавливалась бы на тексте ошибки — то есть утверждала бы меньше, чем
// обещает её имя.
func (e *fakeEdge) AllowList(collection string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.denyList, collection)
}

// RejectCreate — следующее и всякое создание в коллекции отвергается названным отказом.
func (e *fakeEdge) RejectCreate(collection string, s edgeStatus) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rejectCreate[collection] = &s
}

// RejectDelete — всякое удаление в коллекции отвергается названным отказом.
//
// Отдельный заказ, а не Forget: Forget УБИРАЕТ строку, и провайдер, читающий список,
// снимет ресурс из состояния ещё на обновлении — удаление не отправится вовсе. Здесь
// строка ЖИВА и видна чтением, а отказ приходит именно на отзыве. Различие не
// педантское: у этих двух событий разные тексты у провайдера, и предмет пробы —
// именно тот, что приходит на отказе.
func (e *fakeEdge) RejectDelete(collection string, s edgeStatus) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rejectDelete[collection] = &s
}

// AllowDelete — удаление в коллекции снова доезжает.
//
// Нужен ровно затем, чтобы проба, утверждавшая отказ, могла ЗА СОБОЙ УБРАТЬ: уборка
// набора идёт тем же удалением, и невзятый обратно отказ уронил бы пробу на её
// собственной уборке — то есть по причине, к предмету не относящейся.
func (e *fakeEdge) AllowDelete(collection string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.rejectDelete, collection)
}

// Forget — строки у края больше НЕТ: её удалили мимо Terraform.
//
// Отличается от HideFromRead+HideFromList тем, что здесь нечего показывать и списку: это
// настоящее удаление, а не сокрытие. Разница — предмет отдельных проб, и подменять одно
// другим нельзя: провайдер обязан отвечать на них по-разному.
func (e *fakeEdge) Forget(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.rows, id)
}

// Insert кладёт строку НАПРЯМУЮ, минуя HTTP: так пробе достаётся ресурс, существовавший
// «до Terraform» — для импорта, для контрольной страницы области, для чужого соседа.
// base пуст означает путь коллекции самого вида.
//
// Вид РАЗРЕШАЕТСЯ среди зарегистрированных, а не берётся переданным: списочная выдача
// сличает вид по тождеству, и строка, положенная с одноимённой, но ДРУГОЙ структурой, в
// список не попала бы. Внешне это выглядит как «край потерял ресурс», а на деле — два
// объекта об одном виде. Незарегистрированный вид роняет пробу здесь и сразу.
func (e *fakeEdge) Insert(k *edgeKind, base string, obj edgeObject) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	resolved := (*edgeKind)(nil)
	for _, cand := range e.kinds {
		if cand.Path == k.Path {
			resolved = cand
			break
		}
	}
	if resolved == nil {
		e.t.Fatalf("поддельный край: посев вида %s, который не зарегистрирован в newFakeEdge", k.Path)
	}
	k = resolved
	if base == "" {
		base = k.Path
	}
	id, _ := obj["id"].(string)
	if id == "" {
		id = edgeNewID(k)
		obj["id"] = id
	}
	e.seq++
	e.rows[id] = &edgeRow{kind: k, base: base, id: id, seq: e.seq, obj: obj}
	return id
}

// ---- наблюдение -----------------------------------------------------------------------

func (e *fakeEdge) Verbs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.verbLog...)
}

func (e *fakeEdge) Methods() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.methodLog...)
}

func (e *fakeEdge) ReadsOf(id string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.readsByID[id]
}

// Row отдаёт хранимую строку — для проверок, которым нужно то, что у края НА САМОМ ДЕЛЕ,
// а не то, что записано в состояние Terraform.
func (e *fakeEdge) Row(id string) edgeObject {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.rows[id]
	if !ok {
		return nil
	}
	return r.obj
}

// CountOf — сколько строк вида живёт у края.
func (e *fakeEdge) CountOf(collection string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, r := range e.rows {
		if r.kind.Path == collection {
			n++
		}
	}
	return n
}

// AssertEveryCreateCarriedIdempotencyKey — все ли создания несли ключ повторной подачи.
//
// Утверждается ОТДЕЛЬНО от самого свойства идемпотентности: край её соблюдает, но
// провайдер, переставший слать заголовок, остался бы зелёным по всем остальным пробам —
// защита от дубля исчезла бы молча.
func (e *fakeEdge) AssertEveryCreateCarriedIdempotencyKey(t *testing.T) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.createsSeen == 0 {
		t.Fatalf("создание не отправлялось ни разу — утверждать про ключ нечего")
	}
	if e.createsKeyed != e.createsSeen {
		t.Errorf("созданий %d, с ключом повторной подачи %d: защита от дубля работает не на всех",
			e.createsSeen, e.createsKeyed)
	}
}

// ---- маршрутизация ----------------------------------------------------------------------

func (e *fakeEdge) handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := readAllBody(r)

	e.mu.Lock()
	defer e.mu.Unlock()

	path := r.URL.Path
	if strings.HasPrefix(path, "/operations/") {
		e.pollOperation(w, strings.TrimPrefix(path, "/operations/"))
		return
	}

	kind, base, rest := e.route(path)
	if kind == nil {
		// Незарегистрированный путь — ОТКАЗ, называющий путь. Пустой успех здесь означал
		// бы зелёную пробу по краю, которого нет: опечатка в адресе выглядела бы как
		// «ресурсов нет».
		e.write(w, edgeStatus{HTTP: http.StatusNotImplemented, Code: 12,
			Message: "поддельный край не обслуживает путь " + path +
				": зарегистрируйте вид (edgeKind) рядом с пробой этого ресурса"})
		return
	}

	e.methodLog = append(e.methodLog, r.Method+" "+path)

	var req edgeObject
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			e.write(w, edgeStatus{HTTP: http.StatusBadRequest, Code: 3,
				Message: "тело запроса не разбирается как JSON: " + err.Error()})
			return
		}
	}

	id, verb := splitIDVerb(rest)

	switch {
	case id == "" && r.Method == http.MethodGet:
		e.list(w, kind, base, r.URL.Query())
	case id == "" && r.Method == http.MethodPost:
		e.create(w, r, kind, base, req)
	case verb != "":
		e.verb(w, kind, id, verb, req)
	case r.Method == http.MethodGet:
		e.read(w, kind, id)
	case r.Method == http.MethodPatch:
		e.update(w, kind, id, req)
	case r.Method == http.MethodDelete:
		e.delete(w, kind, id)
	default:
		e.write(w, edgeStatus{HTTP: http.StatusMethodNotAllowed, Code: 12,
			Message: "поддельный край не обслуживает " + r.Method + " " + path})
	}
}

// route находит вид по пути. Сегмент «{}» в объявлении вида принимает любое значение.
func (e *fakeEdge) route(path string) (*edgeKind, string, string) {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for _, k := range e.kinds {
		pat := strings.Split(strings.Trim(k.Path, "/"), "/")
		if len(segs) < len(pat) {
			continue
		}
		ok := true
		for i, p := range pat {
			if p != "{}" && p != segs[i] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		base := "/" + strings.Join(segs[:len(pat)], "/")
		rest := ""
		if len(segs) > len(pat) {
			rest = strings.Join(segs[len(pat):], "/")
		}
		return k, base, rest
	}
	return nil, "", ""
}

// splitIDVerb разбирает остаток пути: «», «<id>» либо «<id>:<действие>».
func splitIDVerb(rest string) (id, verb string) {
	if rest == "" {
		return "", ""
	}
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		return rest[:i], rest[i+1:]
	}
	return rest, ""
}

// ---- операции ---------------------------------------------------------------------------

// newOperation заводит операцию. Метаданные кладутся ВСЕГДА, в том числе у операции,
// которой предстоит завершиться отказом: так и делает настоящий край — идентификатор
// чеканится при приёме, до работы. Провайдер, читающий метаданные до проверки отказа,
// обязан на этом попасться, а не выглядеть исправным.
func (e *fakeEdge) newOperation(metadata edgeObject) *edgeOp {
	e.seq++
	op := &edgeOp{id: fmt.Sprintf("enp%017d", e.seq), metadata: metadata}
	e.ops[op.id] = op
	return op
}

func (e *fakeEdge) writeOperation(w http.ResponseWriter, op *edgeOp) {
	e.writeJSON(w, http.StatusOK, map[string]any{"id": op.id, "done": false, "metadata": op.metadata})
}

func (e *fakeEdge) pollOperation(w http.ResponseWriter, opID string) {
	op, ok := e.ops[opID]
	if !ok {
		e.write(w, edgeStatus{HTTP: http.StatusNotFound, Code: 5,
			Message: "Operation " + opID + " not found"})
		return
	}
	op.polls++
	if op.polls < edgeOpPollsBeforeDone {
		e.writeJSON(w, http.StatusOK, map[string]any{"id": op.id, "done": false, "metadata": op.metadata})
		return
	}
	out := map[string]any{"id": op.id, "done": true, "metadata": op.metadata}
	if op.failure != nil {
		var env map[string]any
		_ = json.Unmarshal(op.failure.body(), &env)
		out["error"] = env
	} else if op.response != nil {
		out["response"] = op.response
	}
	e.writeJSON(w, http.StatusOK, out)
}

// ---- действия над ресурсом ---------------------------------------------------------------

func (e *fakeEdge) create(w http.ResponseWriter, r *http.Request, k *edgeKind, base string, req edgeObject) {
	e.createsSeen++
	key := r.Header.Get("Idempotency-Key")
	if key != "" {
		e.createsKeyed++
		if opID, seen := e.idem[key]; seen {
			// Тот же ключ — ТА ЖЕ операция и НИ ОДНОГО нового объекта. Это и есть
			// соблюдение ключа: повтор после потерянного ответа не рождает дубль.
			e.idemReplayed++
			e.writeOperation(w, e.ops[opID])
			return
		}
	}

	if s, deny := e.rejectCreate[k.Path]; deny {
		e.write(w, *s)
		return
	}

	id := edgeNewID(k)
	obj, err := k.Create(e, id, req)
	if err != nil {
		e.write(w, edgeStatus{HTTP: http.StatusBadRequest, Code: 3, Message: err.Error()})
		return
	}
	e.seq++
	row := &edgeRow{kind: k, base: base, id: id, seq: e.seq, obj: obj}
	e.rows[id] = row

	op := e.newOperation(edgeObject{k.MetadataKey: id})
	if k.OpResponse != nil {
		op.response = k.OpResponse(row)
	}
	if key != "" {
		e.idem[key] = op.id
	}
	e.writeOperation(w, op)
}

func (e *fakeEdge) read(w http.ResponseWriter, k *edgeKind, id string) {
	e.readsByID[id]++
	row, ok := e.rows[id]
	if !ok || e.hideRead[id] {
		// ОДИН производитель тела на два разных события: настоящее отсутствие и отказ в
		// доступе. Различить их по ответу нельзя — в этом и состоит сокрытие
		// существования, и провайдер обязан работать с этим, а не вопреки этому.
		e.writeRaw(w, http.StatusNotFound, edgeNotFoundBody(k.Name, id))
		return
	}
	e.writeJSON(w, http.StatusOK, e.viewOf(row))
}

// viewOf — что отдать чтением. Пока держится лаг видимости, отдаётся ПРЕЖНЕЕ следствие
// суффикс-действия: завершённость операции означает долговечность предмета мутации, а не
// видимость её следствия.
func (e *fakeEdge) viewOf(row *edgeRow) edgeObject {
	obj := row.obj
	if row.staleReads > 0 {
		row.staleReads--
		obj = row.stale
	}
	// Выдача применяется и к отстающему снимку тоже: иначе прежнее следствие приезжало бы
	// в другой форме, чем нынешнее, и различие читалось бы как изменение ресурса.
	if row.kind.ReadView != nil {
		return row.kind.ReadView(obj)
	}
	return obj
}

func (e *fakeEdge) update(w http.ResponseWriter, k *edgeKind, id string, req edgeObject) {
	row, ok := e.rows[id]
	if !ok {
		e.writeRaw(w, http.StatusNotFound, edgeNotFoundBody(k.Name, id))
		return
	}
	// Маска приезжает СТРОКОЙ: protojson кодирует google.protobuf.FieldMask
	// перечислением путей через запятую и переводит их в нижний верблюжий регистр.
	//
	// Пустая маска обрабатывается КАК У НАСТОЯЩЕГО КРАЯ — полнообъектной записью, при
	// которой незаполненные поля тела становятся нулями. Отвергать её было бы удобнее для
	// пробы и неверно: тогда провайдер, приславший пустую маску, получал бы явный отказ, а
	// на живом крае молча стирал бы чужую настройку. Здесь он её сотрёт — и следующая же
	// проверка пустого плана это назовёт.
	mask, _ := req["updateMask"].(string)
	fields := []string{}
	if mask == "" {
		fields = k.fullMask()
	} else {
		for _, f := range strings.Split(mask, ",") {
			if f = strings.TrimSpace(f); f != "" {
				fields = append(fields, f)
			}
		}
	}
	for _, f := range fields {
		if err := k.Update(e, row.obj, req, f); err != nil {
			e.write(w, edgeStatus{HTTP: http.StatusBadRequest, Code: 3, Message: err.Error()})
			return
		}
	}
	e.writeOperation(w, e.newOperation(edgeObject{k.MetadataKey: id}))
}

// fullMask — поля, которые край перепишет при ПУСТОЙ маске. Перечень объявляет сам вид
// через свою функцию Update: имена, которые она принимает, и есть изменяемые поля.
// Здесь их не выписать, поэтому берётся общий для всех ресурсов минимум — тот, что
// пострадает заметнее прочего и попадётся первой же проверкой пустого плана.
func (k *edgeKind) fullMask() []string { return []string{"name", "description", "labels"} }

func (e *fakeEdge) verb(w http.ResponseWriter, k *edgeKind, id, verb string, req edgeObject) {
	e.verbLog = append(e.verbLog, verb)
	row, ok := e.rows[id]
	if !ok {
		e.writeRaw(w, http.StatusNotFound, edgeNotFoundBody(k.Name, id))
		return
	}
	fn, ok := k.Verbs[verb]
	if !ok {
		e.write(w, edgeStatus{HTTP: http.StatusNotImplemented, Code: 12,
			Message: "поддельный край не знает действия :" + verb + " у " + k.Path})
		return
	}
	// Прежнее следствие снимается ДО применения — иначе лаг видимости отдавал бы уже
	// новое состояние и проба на ожидание сходимости зеленела бы без ожидания.
	row.stale = edgeCopy(row.obj)
	if err := fn(e, row, req); err != nil {
		e.write(w, edgeStatus{HTTP: http.StatusBadRequest, Code: 3, Message: err.Error()})
		return
	}
	row.staleReads = k.LagAfterVerb
	e.writeOperation(w, e.newOperation(edgeObject{k.MetadataKey: id}))
}

func (e *fakeEdge) delete(w http.ResponseWriter, k *edgeKind, id string) {
	if s, deny := e.rejectDelete[k.Path]; deny {
		e.write(w, *s)
		return
	}
	row, ok := e.rows[id]
	if !ok {
		e.writeRaw(w, http.StatusNotFound, edgeNotFoundBody(k.Name, id))
		return
	}
	if k.Delete != nil {
		if err := k.Delete(e, row); err != nil {
			e.write(w, edgeStatus{HTTP: http.StatusBadRequest, Code: 9, Message: err.Error()})
			return
		}
	}
	delete(e.rows, id)
	e.writeOperation(w, e.newOperation(edgeObject{k.MetadataKey: id}))
}

// edgeFilterName — разбор фильтра списка: край принимает `name="значение"`.
var edgeFilterName = regexp.MustCompile(`^name="(.*)"$`)

func (e *fakeEdge) list(w http.ResponseWriter, k *edgeKind, base string, q url.Values) {
	if e.denyList[k.Path] {
		e.write(w, edgeStatus{HTTP: http.StatusForbidden, Code: 7,
			Message: "permission denied on " + k.Path})
		return
	}
	wantName := ""
	if m := edgeFilterName.FindStringSubmatch(q.Get("filter")); m != nil {
		wantName = m[1]
	}
	scopeWant := ""
	if k.Scope != "" {
		scopeWant = q.Get(k.Scope)
	}

	rows := make([]*edgeRow, 0, len(e.rows))
	for _, r := range e.rows {
		if r.kind != k || r.base != base || e.hideList[r.id] {
			continue
		}
		if scopeWant != "" {
			if got, _ := r.obj[k.Scope].(string); got != scopeWant {
				continue
			}
		}
		if wantName != "" {
			if got, _ := r.obj["name"].(string); got != wantName {
				continue
			}
		}
		rows = append(rows, r)
	}
	// Порядок — по времени создания, как у настоящего края (курсор `(created_at, id)`).
	sort.Slice(rows, func(i, j int) bool { return rows[i].seq < rows[j].seq })

	items := make([]edgeObject, 0, len(rows))
	for _, r := range rows {
		items = append(items, e.viewOf(r))
	}
	e.writeJSON(w, http.StatusOK, map[string]any{k.ListKey: items, "nextPageToken": ""})
}

// ---- ответы ------------------------------------------------------------------------------

// edgeNotFoundBody — тело ответа «не найдено».
//
// ЕДИНСТВЕННЫЙ производитель на два разных события: настоящее отсутствие ресурса и отказ в
// доступе к нему. Побайтовое равенство — намеренное свойство края (сокрытие
// существования), и держится оно тем, что копия здесь одна. Заведись вторая — проба на
// неразличимость осталась бы зелёной, а различие появилось бы.
func edgeNotFoundBody(kindName, id string) []byte {
	return []byte(fmt.Sprintf(`{"code":5,"message":%q}`, kindName+" "+id+" not found"))
}

func (e *fakeEdge) write(w http.ResponseWriter, s edgeStatus) {
	e.writeRaw(w, s.HTTP, s.body())
}

func (e *fakeEdge) writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (e *fakeEdge) writeJSON(w http.ResponseWriter, status int, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		e.t.Errorf("поддельный край: ответ не сериализован: %v", err)
		raw = []byte(`{"code":13,"message":"поддельный край: ответ не сериализован"}`)
		status = http.StatusInternalServerError
	}
	e.writeRaw(w, status, raw)
}

// ---- вспомогательное ------------------------------------------------------------------------

// edgeNewID — идентификатор в той же форме, в какой его чеканит платформа.
//
// Берётся НАСТОЯЩИЙ генератор (pkg/ids), а не своя строка: импорт ресурса проверяет форму
// общим каталогом префиксов, и подделка «похожего» идентификатора провалила бы импорт по
// причине, не имеющей отношения к предмету пробы.
func edgeNewID(k *edgeKind) string {
	if k.Hyphen {
		return ids.NewHyphenID(k.IDPrefix)
	}
	return ids.NewID(k.IDPrefix)
}

// edgeCopy — поверхностная копия объекта с копированием вложенных карт и массивов.
//
// Нужна лагу видимости: прежнее следствие обязано пережить изменение самого объекта.
// Ссылка вместо копии дала бы «прежнее» состояние, равное новому, — то есть лаг был бы
// объявлен и не воспроизведён.
func edgeCopy(in edgeObject) edgeObject {
	raw, err := json.Marshal(in)
	if err != nil {
		panic("поддельный край: снимок объекта не сериализован: " + err.Error())
	}
	var out edgeObject
	if err := json.Unmarshal(raw, &out); err != nil {
		panic("поддельный край: снимок объекта не разобран: " + err.Error())
	}
	return out
}

// edgeStrings — массив строк из тела запроса. Отсутствие и пустой массив дают nil:
// protojson опускает пустое поле, и различать их краю нечем.
func edgeStrings(req edgeObject, field string) []string {
	list, ok := req[field].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// edgeAnyStrings — то же для хранимого значения: край держит массивы как []any.
func edgeAnyStrings(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func edgeToAny(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

func edgeStr(req edgeObject, field string) string {
	s, _ := req[field].(string)
	return s
}

// edgeInt — целое из тела запроса. protojson кодирует 64-разрядные строкой, а
// 32-разрядные числом, поэтому принимаются оба вида: одна структура запроса несёт оба.
func edgeInt(req edgeObject, field string) int64 {
	switch v := req[field].(type) {
	case float64:
		return int64(v)
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

func edgeMap(req edgeObject, field string) map[string]any {
	m, ok := req[field].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

// edgeNow — момент создания в том виде, в каком его отдаёт край: секундная точность.
func edgeNow() string { return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339) }

func readAllBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer func() { _ = r.Body.Close() }()
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}
