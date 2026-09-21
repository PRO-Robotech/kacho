// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// external_refusal_existence_test.go — на ВНЕШНЕМ слушателе отказ не смеет
// зависеть от того, СУЩЕСТВУЕТ ли внутренний предмет.
//
// ЧТО УЖЕ БЫЛО ПРИШПИЛЕНО РЯДОМ И ПОЧЕМУ НЕ УДЕРЖАЛО. Соседний файл
// `external_refusal_shape_test.go` держит ровно это свойство — и держит честно:
// укрытый административный путь отвечает тем же производителем, что и путь,
// которого нет. Но предмет той пробы — ДИСПЕТЧЕР В ОДИНОЧКУ (`NewMux(...)`,
// и запрос подаётся прямо ему). В собранном крае перед диспетчером стоит
// полоса прав (`cmd/api-gateway/main.go`: `inner = authzMW.HTTP(inner)` НАРУЖУ
// от `httpMux`, который держит `restHandler`). Она отвечает ПЕРВОЙ и до
// диспетчера не доходит вовсе — то есть доказанное свойство в собранном крае
// недостижимо, а его держатель об этом молчит, потому что собранного края не
// видит.
//
// Это и есть ответ на вопрос «чем починка держалась»: держателем, чья ОБЛАСТЬ
// уже меньше предмета. Поэтому проба здесь строит цепь в ПРОИЗВОДСТВЕННОМ
// порядке — полоса прав поверх диспетчера — и спрашивает её, а не диспетчер.
//
// ОБЛАСТЬ ЭТОЙ ПРОБЫ НАЗВАНА, а не подразумевается. Между потолком тела и
// полосой прав в собранном крае стоят ещё две полосы: `AuthInterceptor.HTTP` и
// `DPoPMiddleware.Wrap`. Ни одна из них запрос БЕЗ удостоверения не
// перехватывает на посаженном профиле:
//
//   - `AuthInterceptor.HTTP` при отсутствии сессии, базового секрета и
//     предъявителя доходит до `next.ServeHTTP` (auth.go);
//   - `DPoPMiddleware.Wrap` отвечает сам только при `RequireForAllRequests`, и
//     сама полоса монтируется лишь при `KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP`.
//     Умолчание ручки — `false` (config.go), и ни один профиль посадки её не
//     задаёт; это записано и в самом коде (config.go, шапка
//     `AuthNRequireMachineTokenBinding`: «DPoPMiddleware / CnfBindingInterceptor
//     … therefore unmounted on a default deployment»).
//
// Поэтому цепь из двух звеньев — полоса прав над диспетчером — и есть то, что
// видит запросчик без удостоверения.
//
// ЧТО УТВЕРЖДАЕТСЯ. Ответ внешнего слушателя запросчику БЕЗ удостоверения на
// существующий внутренний путь и на путь, которого нет, обязан совпадать
// ПОБАЙТОВО — код, заголовки, тело, — и ни один из них не смеет называть
// внутреннее имя метода.
//
// ЧЕГО ЭТИ ПРОБЫ НЕ ЗАКРЫВАЮТ — ВРЕМЯ ОТВЕТА. Ось названа, а не умолчана.
// Здесь сверяется то, что вызывающий ЧИТАЕТ; сколько он ЖДЁТ, не сверяет ничто,
// и по этой оси свойство НЕ доказано.
//
// ОСЬ НЕ ДВОИЧНАЯ, и описывать её короче, чем она есть, значит описывать её
// неверно. Разбор маршрута идёт по корзине метода и возвращается на ПЕРВОМ
// совпадении, поэтому время зависит от ПОЛОЖЕНИЯ маршрута в порядке разбора, а
// не от одного лишь факта его существования. Наружу просачивается грубое
// положение в таблице: у путей, совпадающих поздно, разница с промахом мала — у
// совпадающих рано она велика. Отношение, снятое на ОДНОМ пути, характеризует
// этот путь, а не ось; разброс между самими существующими путями того же метода
// того же порядка, что разница «есть» против «нет».
//
// Ось оставлена открытой осознанно, а не забыта. Снять её значит отказаться от
// раннего возврата на пути, который исполняется на КАЖДОМ запросе края, либо
// завести второй разборщик с постоянным временем: другой предмет, другая цена и
// другая полоса. Через сеть величина тонет в джиттере, а тому, кому её хватит,
// различимость и так законна там, где он стоит.
//
// ПРЕДИКАТ СНЯТИЯ — два условия разом, оба измеримые:
//
//  1. отношение медиан «существует» к «не существует» не выше 2 для КАЖДОГО
//     сравниваемого пути, а не для одного выбранного;
//  2. абсолютная разница медиан ниже 1 мс — нижней границы джиттера сети, ради
//     которой край выставлен наружу.
//
// Замер: по 20 000 запросов на ветвь после разогрева, по всем внутренним
// биндингам, которые проба ниже сверяет с близнецом. Величины замера в дереве
// не лежат намеренно.
//
// Пока предикат не выполнен, зелёное этих проб читается УЖЕ, чем слово
// «неразличимо»: оно про код, заголовки и тело, и только про них.

package restmux

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// ---- сборка цепи в производственном порядке ----

// countingChecker — модель прав, которую на полосе БЕЗ удостоверения обязаны не
// спрашивать вовсе. Счётчик здесь не украшение: он отличает «отказ выдан до
// вопроса к модели» от «модель ответила отказом».
type countingChecker struct{ calls atomic.Int64 }

func (c *countingChecker) Check(context.Context, middleware.AuthzCheckInput) (middleware.AuthzCheckResult, error) {
	c.calls.Add(1)
	return middleware.AuthzCheckResult{Allowed: false}, nil
}

// externalEdgeChain собирает цепь в ТОМ ЖЕ порядке, что композиционный корень:
// полоса прав СНАРУЖИ диспетчера. Каталог прав — настоящий, встроенный;
// таблица маршрутов — настоящая, сгенерённая.
func externalEdgeChain(t *testing.T) (http.Handler, *countingChecker) {
	t.Helper()
	dispatcher, err := NewMux(context.Background(), probeAddrs(t), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	checker := &countingChecker{}
	mw := buildExternalAuthzWithChecker(t, slog.New(slog.NewTextHandler(io.Discard, nil)), checker)
	return mw.HTTP(dispatcher), checker
}

// buildExternalAuthz — полоса прав с настоящим встроенным каталогом и настоящей
// таблицей маршрутов, пишущая в переданный журнал.
func buildExternalAuthz(t *testing.T, logger *slog.Logger) *middleware.AuthzMiddleware {
	t.Helper()
	return buildExternalAuthzWithChecker(t, logger, &countingChecker{})
}

func buildExternalAuthzWithChecker(t *testing.T, logger *slog.Logger, checker middleware.AuthorizeChecker) *middleware.AuthzMiddleware {
	t.Helper()
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	if err != nil {
		t.Fatalf("LoadEmbeddedPermissionCatalog: %v", err)
	}
	if catalog.Size() == 0 {
		t.Fatal("встроенный каталог прав пуст — предмета у пробы нет")
	}
	rr := middleware.NewRestRouter()
	mw, err := middleware.NewAuthzMiddleware(middleware.AuthzMiddlewareConfig{
		Enabled:         true,
		Catalog:         catalog,
		Subjects:        middleware.NewSubjectExtractor(true),
		Context:         middleware.NewContextExtractor(time.Now, true),
		Resources:       middleware.NewResourceExtractor(rr.PathTemplates()),
		Checker:         checker,
		RestRouter:      rr,
		Logger:          logger,
		CacheTTL:        5 * time.Second,
		CacheMaxEntries: 100,
		PublicAllowlist: middleware.DefaultPublicAllowlist(),
	})
	if err != nil {
		t.Fatalf("NewAuthzMiddleware: %v", err)
	}
	return mw
}

// ---- что видит запросчик ----

// edgeAnswer — ВЕСЬ ответ: код, заголовки, тело. Ничего не отбрасывается:
// различие, ради которого проба заведена, жило именно в теле.
type edgeAnswer struct {
	code    int
	headers string
	body    string
}

func (a edgeAnswer) String() string {
	return "code=" + itoaShape(a.code) + " headers=[" + a.headers + "] body=" + strings.TrimRight(a.body, "\n")
}

// askExternalNoCredential подаёт запрос БЕЗ единого удостоверения. Внешнее
// происхождение — умолчание fail-closed: маркера внутреннего слушателя нет.
func askExternalNoCredential(t *testing.T, h http.Handler, method, path string) edgeAnswer {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	keys := make([]string, 0, len(rec.Header()))
	for k := range rec.Header() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(k + ": " + strings.Join(rec.Header()[k], ","))
	}
	return edgeAnswer{code: rec.Code, headers: b.String(), body: rec.Body.String()}
}

// ---- предмет: внутренние биндинги и их близнецы «того же нет» ----

// absentTwin — путь, отличающийся от предмета РОВНО ОДНИМ фактом: маршрута по
// нему нет. Домен и версия сохранены, поэтому всё, что решается по префиксу,
// решается одинаково; меняется только существование предмета.
func absentTwin(path string) string {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(segs) < 2 {
		return "/kachoAbsentDomain/v1/kachoAbsentCollection"
	}
	return "/" + segs[0] + "/" + segs[1] + "/kachoAbsentCollection"
}

// internalSubjects — предмет, вычисленный из proto-дескрипторов: каждый
// REST-биндинг Internal*-службы, приведённый к конкретному пути. Обходятся ОБА
// объявленных корня, а не один: административная поверхность края состоит из
// служб обоих.
func internalSubjects() []publicBinding {
	var out []publicBinding
	for _, b := range loadedHTTPBindings() {
		if !b.internal {
			continue
		}
		out = append(out, publicBinding{method: b.method, path: probePath(b.template), fqn: b.fqn})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].fqn != out[j].fqn {
			return out[i].fqn < out[j].fqn
		}
		return out[i].path < out[j].path
	})
	return out
}

// sharesAPublicRoute — на этой паре (метод, путь) внешний слушатель обслуживает
// ПУБЛИЧНЫЙ глагол, и «маршрута нет» про неё просто неверно.
//
// Так устроена поверхность пулов адресов: `AddressPoolService` выставлен наружу
// НАМЕРЕННО и гейтится отношением `system_admin` @ `cluster`, а пути совпадают с
// внутренними, потому что второго адреса у ресурса быть не должно (см. решение
// в шапке регистрации, restmux/mux.go). Укрыть такой путь значило бы убрать
// публичный глагол; отвечать на нём «есть маршрут» — не оракул, а опубликованная
// таблица маршрутов.
//
// Признак берётся ТОЙ ЖЕ таблицей и ТЕМ ЖЕ предикатом, которыми пользуется сама
// полоса прав. Второй источник ответа на этот вопрос разошёлся бы с первым
// молча — и разошёлся бы ровно на тех путях, ради которых заводится.
func sharesAPublicRoute(rr *middleware.RestRouter, method, path string) bool {
	fqn, ok := rr.Resolve(method, path)
	return ok && !allowlist.HasInternalSuffix("/"+fqn)
}

// TestExternalListener_RefusalDoesNotDependOnExistence — условие 1:
// неразличимость доказывается СРАВНЕНИЕМ двух снятых целиком ответов.
func TestExternalListener_RefusalDoesNotDependOnExistence(t *testing.T) {
	chain, checker := externalEdgeChain(t)
	rr := middleware.NewRestRouter()

	subjects := internalSubjects()
	if len(subjects) < minInternalBindings {
		t.Fatalf("административных REST-биндингов в дескрипторах %d (< %d) — предмета у пробы нет",
			len(subjects), minInternalBindings)
	}

	// Премиса в обе стороны: предмет — НАСТОЯЩИЙ маршрут внутренней службы,
	// близнец — НАСТОЯЩЕЕ отсутствие. Оба проверяются той же таблицей, которой
	// пользуется полоса прав, а не предполагаются.
	subject := subjects[0]
	fqn, ok := rr.Resolve(subject.method, subject.path)
	if !ok {
		t.Fatalf("предмет %s %s не резолвится таблицей маршрутов — он не «существующий внутренний путь»",
			subject.method, subject.path)
	}
	twinPath := absentTwin(subject.path)
	if gotFQN, twinOK := rr.Resolve(subject.method, twinPath); twinOK {
		t.Fatalf("близнец %s %s резолвится в %q — он не «несуществующий путь», сравнивать нечего",
			subject.method, twinPath, gotFQN)
	}

	present := askExternalNoCredential(t, chain, subject.method, subject.path)
	absent := askExternalNoCredential(t, chain, subject.method, twinPath)

	t.Logf("существующий внутренний путь: %s %s (%s)\n  -> %s", subject.method, subject.path, fqn, present)
	t.Logf("несуществующий путь:          %s %s\n  -> %s", subject.method, twinPath, absent)
	t.Logf("модель прав спрошена раз: %d", checker.calls.Load())

	if present != absent {
		t.Errorf("на ВНЕШНЕМ слушателе запросчик БЕЗ удостоверения различает «есть» и «нет»:\n"+
			"  существующий  %s %s\n    %s\n"+
			"  которого нет  %s %s\n    %s\n"+
			"Различая их, он обходит всю внутреннюю поверхность и узнаёт её состав, не предъявив ничего.",
			subject.method, subject.path, present, subject.method, twinPath, absent)
	}
	if strings.Contains(present.body, fqn) {
		t.Errorf("тело отказа называет ПОЛНОЕ ВНУТРЕННЕЕ ИМЯ МЕТОДА %q:\n  %s", fqn, present)
	}
}

// existenceOracleCeiling — потолок УБЫВАЮЩИЙ: сколько внутренних биндингов
// сегодня отвечают запросчику без удостоверения не так, как отвечает путь,
// которого нет. Цель — 0; число снимается прогоном, а не назначается.
const existenceOracleCeiling = 0

// TestExternalListener_ExistenceOracleCensus — условие 3: КЛАСС, а не
// координата. Предмет вычисляется обходом дескрипторов, находки считаются
// числом, перепись печатается отдельно от находок.
func TestExternalListener_ExistenceOracleCensus(t *testing.T) {
	chain, _ := externalEdgeChain(t)
	rr := middleware.NewRestRouter()
	subjects := internalSubjects()
	if len(subjects) < minInternalBindings {
		t.Fatalf("административных REST-биндингов в дескрипторах %d (< %d) — пустой обход вердиктом не является",
			len(subjects), minInternalBindings)
	}

	var (
		distinguishable  []string
		namesTheMethod   int
		compared         int
		twinResolved     int
		sharedWithPublic int
	)
	shapes := map[string]int{}
	for _, b := range subjects {
		if sharesAPublicRoute(rr, b.method, b.path) {
			// Путь обслуживается публичным глаголом — «маршрута нет» про него
			// неверно, сравнивать не с чем. Считается ОТДЕЛЬНО, а не
			// выбрасывается молча: исключение, которого не видно в переписи,
			// однажды поглотит настоящую находку.
			sharedWithPublic++
			continue
		}
		twinPath := absentTwin(b.path)
		if _, twinOK := rr.Resolve(b.method, twinPath); twinOK {
			// Близнец оказался настоящим маршрутом — сравнивать нечего, и
			// молчание на этой строке ничего не значит. Считается отдельно.
			twinResolved++
			continue
		}
		compared++
		present := askExternalNoCredential(t, chain, b.method, b.path)
		absent := askExternalNoCredential(t, chain, b.method, twinPath)
		shapes[present.String()]++
		if fqn, ok := rr.Resolve(b.method, b.path); ok && strings.Contains(present.body, fqn) {
			namesTheMethod++
		}
		if present != absent {
			distinguishable = append(distinguishable,
				"  "+b.method+" "+b.path+" ("+b.fqn+")\n"+
					"      есть : "+present.String()+"\n"+
					"      нет  : "+b.method+" "+twinPath+" -> "+absent.String())
		}
	}

	t.Logf("перепись: внутренних биндингов %d · сверено с близнецом %d · делят путь с публичным глаголом %d · "+
		"близнец оказался маршрутом %d · различных форм ответа на «есть» %d · тело называет имя метода у %d",
		len(subjects), compared, sharedWithPublic, twinResolved, len(shapes), namesTheMethod)
	if compared == 0 {
		t.Fatal("ни один внутренний биндинг не получил близнеца — сверка выродилась, её молчание пусто")
	}

	if len(distinguishable) > existenceOracleCeiling {
		t.Errorf("мест на внешнем слушателе, отвечающих ПО-РАЗНОМУ на «есть» и «нет» запросчику без "+
			"удостоверения: %d (потолок %d). Каждое из них перечисляет внутреннюю поверхность.\n%s",
			len(distinguishable), existenceOracleCeiling, strings.Join(distinguishable, "\n"))
	}
	if namesTheMethod > 0 {
		t.Errorf("тело отказа называет ПОЛНОЕ ВНУТРЕННЕЕ ИМЯ МЕТОДА у %d из %d внутренних биндингов",
			namesTheMethod, compared)
	}
}

// TestExternalListener_HiddenRefusalMatchesTheDispatcherNotFound — укрытие
// обязано совпадать с ответом САМОГО ДИСПЕТЧЕРА на маршрут, которого у него
// нет, побайтово.
//
// ЗАЧЕМ ОТДЕЛЬНО. Предыдущая проба доказывает, что два ответа полосы прав равны
// ДРУГ ДРУГУ. Этого мало: они могли бы быть равны и при этом отличаться от того,
// что на том же слушателе отдаёт диспетчер на всяком обычном промахе, — и тогда
// «полоса прав ответила» снова стало бы отличимо от «маршрута нет», просто на
// другом срезе. Здесь берётся ответ диспетчера НАПРЯМУЮ (без полосы прав) и
// сверяется с укрытием.
//
// Эта же проба — СТОРОЖ РАСХОЖДЕНИЯ. Полоса прав не может позвать диспетчер за
// текстом: `middleware` не импортирует `restmux` и не должен. Поэтому литерал
// живёт в полосе прав, а равенство двух производителей держится здесь — и
// смена текста у grpc-gateway красит эту пробу, а не расщепляет свойство молча.
func TestExternalListener_HiddenRefusalMatchesTheDispatcherNotFound(t *testing.T) {
	dispatcher, err := NewMux(context.Background(), probeAddrs(t), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	chain, _ := externalEdgeChain(t)

	subjects := internalSubjects()
	if len(subjects) < minInternalBindings {
		t.Fatalf("административных REST-биндингов %d (< %d) — предмета нет", len(subjects), minInternalBindings)
	}
	subject := subjects[0]

	// Премиса: диспетчер сам по себе отвечает на этот путь отсутствием
	// маршрута. Без неё сверка шла бы с неизвестно чем.
	byDispatcher := askExternalNoCredential(t, dispatcher, subject.method, subject.path)
	if byDispatcher.code != http.StatusNotFound {
		t.Fatalf("диспетчер сам отвечает на %s %s кодом %d, а не отсутствием маршрута — "+
			"укрытие в нём больше не держится, и сверять с ним нечего",
			subject.method, subject.path, byDispatcher.code)
	}

	byChain := askExternalNoCredential(t, chain, subject.method, subject.path)
	t.Logf("диспетчер напрямую: %s", byDispatcher)
	t.Logf("собранный край:     %s", byChain)
	if byChain != byDispatcher {
		t.Errorf("укрытие в собранном крае не совпадает с ответом диспетчера на маршрут, которого нет:\n"+
			"  диспетчер : %s\n  край      : %s", byDispatcher, byChain)
	}
}

// publicGatedCollection — публичный биндинг-коллекция (без переменных в шаблоне),
// чья запись каталога гейтится отношением модели. Вычисляется обходом, а не
// выписывается: выписанный путь пережил бы свой предмет молча.
func publicGatedCollection(t *testing.T, catalog *middleware.PermissionCatalog) publicBinding {
	t.Helper()
	for _, b := range loadedHTTPBindings() {
		if b.internal || strings.ContainsRune(b.template, '{') {
			continue
		}
		entry, found := catalog.Lookup(b.fqn)
		if !found || entry.IsExempt() || entry.ScopeFiltered || entry.RequiredRelation == "" {
			continue
		}
		return publicBinding{method: b.method, path: b.template, fqn: b.fqn}
	}
	t.Fatal("в контракте нет ни одного публичного биндинга-коллекции с гейтящей записью каталога — " +
		"опыт «свой получает точный отказ» ставить не на чем")
	return publicBinding{}
}

// TestExternalListener_OwnCallerKeepsAPreciseRefusal — условие 2, и это ВТОРОЙ
// опыт пары.
//
// Укрытие адресовано ЧУЖОМУ — запросчику без удостоверения на пути, которого
// этот слушатель не обслуживает. Оно не смеет распространиться на СВОЕГО: тот,
// кто предъявил годное удостоверение и обратился к пути, который слушатель
// обслуживает, обязан по-прежнему получить отказ, по которому понятно, ЧТО не
// так. Без этого опыта «одинаковый ответ» достигался бы тем, что одинаковым
// стало всё.
func TestExternalListener_OwnCallerKeepsAPreciseRefusal(t *testing.T) {
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	if err != nil {
		t.Fatalf("LoadEmbeddedPermissionCatalog: %v", err)
	}
	chain, checker := externalEdgeChain(t)
	b := publicGatedCollection(t, catalog)

	req := httptest.NewRequest(b.method, b.path, nil)
	req.Header.Set("X-Kacho-Principal-Id", "usr_ownprobe")
	req.Header.Set("X-Kacho-Principal-Type", "user")
	req.Header.Set("X-Kacho-Token-Acr", "3")
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)
	body := rec.Body.String()

	t.Logf("свой на обслуживаемом пути: %s %s (%s) -> %d %s", b.method, b.path, b.fqn, rec.Code, strings.TrimRight(body, "\n"))
	t.Logf("модель прав спрошена раз: %d", checker.calls.Load())

	if rec.Code == http.StatusNotFound {
		t.Errorf("свой запросчик получил укрытие вместо отказа: %d %s — наблюдаемость сломана, "+
			"по такому ответу непонятно, что не так", rec.Code, strings.TrimRight(body, "\n"))
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("свой запросчик на гейтящемся пути ожидает отказ модели (403), получил %d: %s",
			rec.Code, strings.TrimRight(body, "\n"))
	}
	if checker.calls.Load() == 0 {
		t.Error("модель прав не была спрошена вовсе — отказ выдан не ею, и «точность» ему брать неоткуда")
	}
	if !strings.Contains(body, "PreconditionFailure") {
		t.Errorf("в отказе своему нет разбора причины (PreconditionFailure): %s", strings.TrimRight(body, "\n"))
	}
}

// TestInternalListener_KeepsTellingItsOwnCallersApart — внутренний слушатель
// НЕ предмет этой полосы, и проба сторожит именно это: на нём различимость
// законна, там ходят свои. Укрытие, расползшееся на внутренний слушатель,
// сделало бы административную поверхность недостижимой для тех, кому она
// адресована, — и показалось бы «ещё более безопасным».
func TestInternalListener_KeepsTellingItsOwnCallersApart(t *testing.T) {
	dispatcher, err := NewMux(context.Background(), probeAddrs(t), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	chain, _ := externalEdgeChain(t)
	reference := hiddenAnswer(t, dispatcher)
	subjects := internalSubjects()
	if len(subjects) < minInternalBindings {
		t.Fatalf("административных REST-биндингов %d (< %d) — предмета нет", len(subjects), minInternalBindings)
	}

	hidden := 0
	for _, b := range subjects {
		req := httptest.NewRequest(b.method, b.path, nil)
		req = req.WithContext(listenerorigin.WithInternal(req.Context()))
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code == reference.code && rec.Body.String() == reference.body {
			hidden++
		}
	}
	t.Logf("перепись: внутренних биндингов %d · укрыто на ВНУТРЕННЕМ слушателе %d", len(subjects), hidden)
	if hidden > 0 {
		t.Errorf("укрытие внешнего слушателя действует и на ВНУТРЕННЕМ: %d из %d биндингов "+
			"отвечают «маршрута нет» там, где ходят свои", hidden, len(subjects))
	}
}

// TestExternalListener_PublicSurfaceIsNotHidden — ОТРИЦАНИЕ В ПАРЕ С
// ПОЛОЖИТЕЛЬНЫМ, и без него соседняя перепись ничего не стоит.
//
// «Ответы неразличимы» достигается двумя способами: убрать различие — и убрать
// ответы. Второй дал бы зелёное на всех пробах выше и снял бы наружу весь
// публичный контракт. Поэтому здесь утверждается обратное: НИ ОДИН публичный
// биндинг контракта не смеет получить укрытие на внешнем слушателе.
//
// Проба сторожит и вторую, менее очевидную беду. Часть административных путей
// СОВПАДАЕТ с публичными, и какой глагол увидит вызывающий, решает порядок
// разбора таблицы маршрутов. Поменяйся он — укрытие накрыло бы публичную
// поверхность пулов адресов, и снаружи это выглядело бы как «стало безопаснее».
func TestExternalListener_PublicSurfaceIsNotHidden(t *testing.T) {
	dispatcher, err := NewMux(context.Background(), probeAddrs(t), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	chain, _ := externalEdgeChain(t)
	reference := hiddenAnswer(t, dispatcher)

	var subject []publicBinding
	for _, b := range loadedHTTPBindings() {
		if b.internal {
			continue
		}
		subject = append(subject, publicBinding{method: b.method, path: probePath(b.template), fqn: b.fqn})
	}
	if len(subject) == 0 {
		t.Fatal("публичных биндингов в дескрипторах ноль — пустой обход вердиктом не является")
	}

	var hidden []string
	for _, b := range subject {
		if askExternalNoCredential(t, chain, b.method, b.path) == reference {
			hidden = append(hidden, "  "+b.method+" "+b.path+" ("+b.fqn+")")
		}
	}
	t.Logf("перепись: публичных биндингов %d · укрыто %d", len(subject), len(hidden))
	if len(hidden) > 0 {
		t.Errorf("укрытие накрыло %d ПУБЛИЧНЫХ биндингов из %d — снаружи пропал контракт, "+
			"а «неразличимость» получена тем, что различать стало нечего:\n%s",
			len(hidden), len(subject), strings.Join(hidden, "\n"))
	}
}

// hiddenAnswer — ответ укрытия, взятый у САМОГО ДИСПЕТЧЕРА на заведомо
// отсутствующем маршруте, а не выписанный литералом.
//
// Литерал здесь не годится, и это измерено: `protojson` намеренно подмешивает в
// вывод пробелы и выбирает их ОДИН РАЗ ЗА ПРОЦЕСС. Выписанное тело совпадало бы
// с настоящим в одних запусках и расходилось бы в других — проба мигала бы, а
// мигающая проба вердикта не даёт.
func hiddenAnswer(t *testing.T, dispatcher http.Handler) edgeAnswer {
	t.Helper()
	a := askExternalNoCredential(t, dispatcher, "GET", "/kachoAbsentDomain/v1/kachoAbsentCollection")
	if a.code != http.StatusNotFound {
		t.Fatalf("диспетчер ответил на заведомо отсутствующий маршрут кодом %d, а не отсутствием — "+
			"эталон укрытия брать неоткуда", a.code)
	}
	return a
}

// ---- запись об укрытом отказе ----

// logSink — журнал процесса, собранный С ТЕМ ЖЕ ПОРОГОМ, что ставит корень
// (`slog.LevelInfo`, cmd/api-gateway/main.go). Порог здесь не выбран, а
// повторён; связь держит cmd/api-gateway/access_log_wiring_test.go,
// `TestProcessLogLevelIsTheOneTheProbesAssume` — сменится уровень в корне,
// покраснеет он, а не разойдётся молча смысл зелёного этих проб.
type logSink struct {
	buf    *bytes.Buffer
	logger *slog.Logger
}

func newLogSink() *logSink {
	buf := &bytes.Buffer{}
	return &logSink{
		buf: buf,
		logger: slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}
}

// records разбирает напечатанное в записи. Считаются СТРОКИ, которые процесс
// действительно напечатал бы: запись ниже порога сюда не попадает вовсе, и в
// этом весь предмет.
func (s *logSink) records() []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// TestHiddenRefusalLeavesARecord — укрытый отказ оставляет запись, и записей
// столько же, сколько запросов.
//
// ЧТО ЭТО ДЕРЖИТ И ЧЕГО НЕ ДЕРЖИТ. Здесь проверяется МЕХАНИЗМ: журнал доступа,
// стоящий снаружи полосы прав, видит укрытый отказ, и собственная запись фазы
// печатается. Сам ПОРЯДОК звеньев в собранном крае держит другой гейт —
// cmd/api-gateway/access_log_wiring_test.go,
// `TestHTTPAccessLogIsOutsideTheRightsLane`, — потому что порядок живёт в
// композиционном корне, а не здесь. Разделение намеренное: проба, которая сама
// собрала бы цепь в нужном порядке и на нём обрадовалась, проверяла бы себя.
func TestHiddenRefusalLeavesARecord(t *testing.T) {
	sink := newLogSink()
	dispatcher, err := NewMux(context.Background(), probeAddrs(t), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	mw := buildExternalAuthz(t, sink.logger)
	// Порядок, который держит гейт корня: журнал доступа СНАРУЖИ полосы прав.
	chain := middleware.HTTPAccessLog(sink.logger)(mw.HTTP(dispatcher))

	subjects := internalSubjects()
	var asked []publicBinding
	rr := middleware.NewRestRouter()
	for _, b := range subjects {
		if sharesAPublicRoute(rr, b.method, b.path) {
			continue
		}
		asked = append(asked, b)
	}
	if len(asked) < minInternalBindings {
		t.Fatalf("необслуживаемых путей для опроса %d (< %d) — пустой обход вердиктом не является",
			len(asked), minInternalBindings)
	}

	for _, b := range asked {
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, httptest.NewRequest(b.method, b.path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s ответил %d — это не укрытый отказ, и проба меряет не тот предмет",
				b.method, b.path, rec.Code)
		}
	}

	var access, phase int
	pathsSeen := map[string]bool{}
	for _, r := range sink.records() {
		switch r["msg"] {
		case "access":
			access++
			if m, ok := r["method"].(string); ok {
				pathsSeen[m] = true
			}
		default:
			phase++
		}
	}
	t.Logf("перепись: запросов %d · строк журнала доступа %d · прочих строк %d · различных путей в журнале %d",
		len(asked), access, phase, len(pathsSeen))

	if access != len(asked) {
		t.Errorf("строк журнала доступа %d при %d запросах к необслуживаемым путям. "+
			"Анонимный перебор внешней поверхности не оставляет следа, по которому его можно было бы "+
			"разобрать: ровно этот класс событий и ищут в разборе происшествия.", access, len(asked))
	}
	if len(pathsSeen) != len(asked) {
		t.Errorf("различных путей в журнале %d при %d опрошенных — по записи нельзя сказать, ЧТО спрашивали",
			len(pathsSeen), len(asked))
	}
	if phase == 0 {
		t.Errorf("собственная запись фазы укрытия не напечатана НИ РАЗУ при %d укрытых отказах: "+
			"она идёт уровнем ниже порога процесса, то есть не существует для оператора. "+
			"Без неё в журнале 404 полосы прав неотличим от 404 диспетчера.", len(asked))
	}
}

// ---- объём записи не управляется запросчиком ----

// loggedBytesForPathOfLength — сколько байт журнала оставляет ОДИН
// незасвидетельствованный запрос к необслуживаемому пути указанной длины.
func loggedBytesForPathOfLength(t *testing.T, dispatcher http.Handler, n int) (bytes int, lines int, body string) {
	t.Helper()
	sink := newLogSink()
	mw := buildExternalAuthz(t, sink.logger)
	chain := middleware.HTTPAccessLog(sink.logger)(mw.HTTP(dispatcher))
	path := "/vpc/v1/" + strings.Repeat("a", n)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("путь длиной %d ответил %d, а не укрытием — проба меряет не тот предмет", n, rec.Code)
	}
	return sink.buf.Len(), len(sink.records()), sink.buf.String()
}

// TestRecordSizeIsNotDrivenByTheRequester — объём записи ограничен КОНСТАНТОЙ,
// а не длиной пути запросчика.
//
// # Предмет
//
// Наблюдаемость, которой можно залить диск, наблюдаемостью не остаётся. Запись
// об укрытом отказе несёт путь, путь приходит от незасвидетельствованного
// запросчика, и предел длины строки запроса у сервера края — умолчание
// библиотеки в мегабайт. Без усечения один запрос писал бы в журнал кратно
// своему размеру, и ротация вымывала бы историю ровно во время события, ради
// разбора которого запись и заводилась.
//
// Ограничителя темпа на этой полосе нет ни одного, поэтому объём на ОДИН запрос
// обязан быть конечным сам по себе, а не в среднем.
func TestRecordSizeIsNotDrivenByTheRequester(t *testing.T) {
	dispatcher, err := NewMux(context.Background(), probeAddrs(t), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	small, smallLines, _ := loggedBytesForPathOfLength(t, dispatcher, 1_000)
	large, largeLines, largeBody := loggedBytesForPathOfLength(t, dispatcher, 900_000)

	t.Logf("перепись: путь 1 000 байт -> строк %d, журнала %d байт · "+
		"путь 900 000 байт -> строк %d, журнала %d байт · прирост %d",
		smallLines, small, largeLines, large, large-small)

	// Премиса: запись вообще ведётся. Ноль строк сделал бы «объём не растёт»
	// истинным даром — и это ровно то состояние, которое чинили до этого.
	if smallLines == 0 || largeLines == 0 {
		t.Fatal("записей не ведётся вовсе — «объём не растёт» получено тем, что писать нечего")
	}

	// ГЛАВНОЕ: прирост объёма на рост пути в 900 раз обязан быть ограничен
	// константой. Допуск покрывает саму пометку об усечении и длину в ней.
	const growthAllowance = 512
	if large-small > growthAllowance {
		t.Errorf("объём записи вырос на %d байт при росте пути на %d байт — записью управляет "+
			"ЗАПРОСЧИК. Один незасвидетельствованный запрос под предел строки запроса пишет в "+
			"журнал кратно своему размеру; ротация вымоет историю ровно во время события, ради "+
			"которого запись и заводилась.", large-small, 900_000-1_000)
	}

	// Усечение обязано быть ЯВНЫМ: молча укороченный путь читается как настоящий
	// и уводит разбор происшествия по ложному следу.
	if !strings.Contains(largeBody, middleware.LogTruncationMark) {
		t.Errorf("в записи о длинном пути нет признака усечения %q — укороченный путь неотличим "+
			"от настоящего", middleware.LogTruncationMark)
	}
	// И обязано называть ИСХОДНУЮ длину: без неё по записи не отличить запрос в
	// сто байт сверх предела от запроса в мегабайт.
	if !strings.Contains(largeBody, "900008") {
		t.Errorf("в записи не названа исходная длина пути — запрос чуть длиннее предела " +
			"неотличим от запроса под мегабайт")
	}
}

// TestShortPathIsLoggedWhole — законный близнец: путь в пределах разумного
// пишется ЦЕЛИКОМ. Без него «объём ограничен» достигалось бы тем, что в журнал
// не попадает ничего полезного.
func TestShortPathIsLoggedWhole(t *testing.T) {
	dispatcher, err := NewMux(context.Background(), probeAddrs(t), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	subjects := internalSubjects()
	if len(subjects) == 0 {
		t.Fatal("предмета нет")
	}
	sink := newLogSink()
	mw := buildExternalAuthz(t, sink.logger)
	chain := middleware.HTTPAccessLog(sink.logger)(mw.HTTP(dispatcher))

	b := subjects[0]
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, httptest.NewRequest(b.method, b.path, nil))
	body := sink.buf.String()

	if !strings.Contains(body, b.path) {
		t.Errorf("путь обычной длины %q не попал в журнал целиком — усечение съело предмет записи:\n%s",
			b.path, body)
	}
	if strings.Contains(body, middleware.LogTruncationMark) {
		t.Errorf("путь обычной длины помечен усечённым — предел стоит слишком низко:\n%s", body)
	}
}
