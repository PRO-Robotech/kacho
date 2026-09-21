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
package restmux

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	if err != nil {
		t.Fatalf("LoadEmbeddedPermissionCatalog: %v", err)
	}
	if catalog.Size() == 0 {
		t.Fatal("встроенный каталог прав пуст — предмета у пробы нет")
	}
	rr := middleware.NewRestRouter()
	checker := &countingChecker{}
	mw, err := middleware.NewAuthzMiddleware(middleware.AuthzMiddlewareConfig{
		Enabled:         true,
		Catalog:         catalog,
		Subjects:        middleware.NewSubjectExtractor(true),
		Context:         middleware.NewContextExtractor(time.Now, true),
		Resources:       middleware.NewResourceExtractor(rr.PathTemplates()),
		Checker:         checker,
		RestRouter:      rr,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		CacheTTL:        5 * time.Second,
		CacheMaxEntries: 100,
		PublicAllowlist: middleware.DefaultPublicAllowlist(),
	})
	if err != nil {
		t.Fatalf("NewAuthzMiddleware: %v", err)
	}
	return mw.HTTP(dispatcher), checker
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
		distinguishable []string
		namesTheMethod  int
		compared        int
		twinResolved    int
	)
	shapes := map[string]int{}
	for _, b := range subjects {
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

	t.Logf("перепись: внутренних биндингов %d · сверено с близнецом %d · близнец оказался маршрутом %d · "+
		"различных форм ответа на «есть» %d · тело называет имя метода у %d",
		len(subjects), compared, twinResolved, len(shapes), namesTheMethod)
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
	chain, _ := externalEdgeChain(t)
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
		if rec.Code == http.StatusNotFound && strings.Contains(rec.Body.String(), `"message":"Not Found"`) {
			hidden++
		}
	}
	t.Logf("перепись: внутренних биндингов %d · укрыто на ВНУТРЕННЕМ слушателе %d", len(subjects), hidden)
	if hidden > 0 {
		t.Errorf("укрытие внешнего слушателя действует и на ВНУТРЕННЕМ: %d из %d биндингов "+
			"отвечают «маршрута нет» там, где ходят свои", hidden, len(subjects))
	}
}
