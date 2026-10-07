// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// external_route_gate_test.go — на внешнем слушателе внутреннее пространство
// путей отвечает «маршрута нет» ДО слоя аутентификации и прав (kacho#3053).
//
// ПРИЗНАК. Решение «внутреннее на внешнем → publicMux» стояло в диспетчере, то
// есть ПОСЛЕДНИМ звеном цепочки края. Всё, что перед ним (аутентификация,
// ступень подтверждения, проверка прав), отвечало раньше: аноним получал 401 на
// внутреннем пути так же, как на публичном, а вызывающий с действующей сессией —
// 403, чьи подробности называли внутренний метод и его право. Запрет ban06
// снаружи не доказывался ничем, кроме гейта монтирования по дереву.
//
// ЧТО УТВЕРЖДАЕТСЯ. Перед цепочкой стоит сторож маршрута; цепочка подменена
// здесь сторожем-счётчиком, который за ней отдаёт запрос настоящему диспетчеру.
// Для каждого внутреннего REST-биндинга, ВЫВЕДЕННОГО из proto-дескрипторов
// (не выписанного), и для каждого внутреннего метода без `google.api.http`
// (путь по умолчанию `/<служба>/<метод>`):
//
//   - снаружи запрос до цепочки не доходит, и ответ побайтно равен ответу этого
//     же слушателя на путь, которого нет нигде (404), а на пути, публичном под
//     другим методом, — ответу на метод, которого нет ни у одного биндинга (501);
//   - внутри (близнец) тот же запрос доходит до цепочки и получает не 404.
//
// Исключаются ВЫВОДОМ, с печатью числа: внутренние пары, которые тот же
// сопоставитель находит среди публичных биндингов под тем же методом (публичный
// глагол на том же адресе, ADM-1 S1, и форма `{id}:internal`, которую поглощает
// публичный шаблон). Там «маршрута нет» неверно: маршрут есть и публичный.
//
// Пустой перечень — красный; число опрошенных печатается.
package restmux

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	geopb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// chainSentinel — на месте цепочки аутентификации и прав. Считает, сколько
// запросов до неё дошло, и отдаёт их настоящему диспетчеру: так близнец на
// внутреннем слушателе получает настоящий ответ внутреннего mux'а.
type chainSentinel struct {
	reached atomic.Int64
	next    http.Handler
}

func (s *chainSentinel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.reached.Add(1)
	s.next.ServeHTTP(w, r)
}

// gateAsk — один запрос через сторож: что видит вызывающий и дошёл ли запрос
// до цепочки.
type gateAsk struct {
	shape   answerShape
	reached bool
}

func askThrough(edge http.Handler, sentinel *chainSentinel, method, path string, internal bool) gateAsk {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	if internal {
		req = req.WithContext(listenerorigin.WithInternal(req.Context()))
	}
	before := sentinel.reached.Load()
	rec := httptest.NewRecorder()
	edge.ServeHTTP(rec, req)
	return gateAsk{
		shape: answerShape{
			code:   rec.Code,
			ctype:  rec.Header().Get("Content-Type"),
			body:   rec.Body.String(),
			nosnif: rec.Header().Get("X-Content-Type-Options"),
		},
		reached: sentinel.reached.Load() > before,
	}
}

// internalSpaceRow — один опрашиваемый внутренний адрес.
type internalSpaceRow struct {
	method, path, fqn string
	// class: "absent" (пути нет ни у одного публичного биндинга), "otherMethod"
	// (путь публичен под другим методом), "unbound" (метод без google.api.http).
	class string
	space string
}

// internalSpaceCensus — перечень, выведенный из дескрипторов, и число
// исключённых выводом.
type internalSpaceCensus struct {
	rows     []internalSpaceRow
	excluded []string
	public   []publicBinding
}

// deriveInternalSpaces строит перечень из той же таблицы биндингов, что
// исполняет диспетчер, и классифицирует его тем же сопоставителем.
func deriveInternalSpaces() internalSpaceCensus {
	var c internalSpaceCensus
	var publicRoutes []internalRoute
	for _, b := range loadedHTTPBindings() {
		if b.internal || !underDeclaredRoot(b.fqn) {
			continue
		}
		publicRoutes = append(publicRoutes, internalRoute{method: b.method, segs: b.segs})
		c.public = append(c.public, publicBinding{method: b.method, path: probePath(b.template), fqn: b.fqn})
	}
	for _, b := range loadedHTTPBindings() {
		if !b.internal || !underDeclaredRoot(b.fqn) {
			continue
		}
		p := probePath(b.template)
		segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
		sameMethod, otherMethod := false, false
		for _, pr := range publicRoutes {
			if pr.matches(b.method, segs) {
				sameMethod = true
				break
			}
			if pr.matches(pr.method, segs) {
				otherMethod = true
			}
		}
		if sameMethod {
			c.excluded = append(c.excluded, b.method+" "+b.template+" ("+b.fqn+")")
			continue
		}
		class := "absent"
		if otherMethod {
			class = "otherMethod"
		}
		c.rows = append(c.rows, internalSpaceRow{method: b.method, path: p, fqn: b.fqn, class: class, space: "/" + segs[0]})
	}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !underDeclaredRoot(string(fd.Package())) {
			return true
		}
		for i := 0; i < fd.Services().Len(); i++ {
			svc := fd.Services().Get(i)
			if !isInternalServiceName(string(svc.Name())) {
				continue
			}
			for j := 0; j < svc.Methods().Len(); j++ {
				m := svc.Methods().Get(j)
				if rule, _ := proto.GetExtension(m.Options(), annotations.E_Http).(*annotations.HttpRule); rule != nil {
					continue
				}
				c.rows = append(c.rows, internalSpaceRow{
					method: http.MethodPost,
					path:   "/" + string(svc.FullName()) + "/" + string(m.Name()),
					fqn:    string(svc.FullName()) + "/" + string(m.Name()),
					class:  "unbound",
					space:  "/" + string(svc.FullName()),
				})
			}
		}
		return true
	})
	sort.Slice(c.rows, func(i, j int) bool {
		if c.rows[i].path != c.rows[j].path {
			return c.rows[i].path < c.rows[j].path
		}
		return c.rows[i].method < c.rows[j].method
	})
	return c
}

// absentTwinPath — путь, которого нет ни у одного биндинга: проверяется, а не
// предполагается.
const absentTwinPath = "/kachoProbeAbsentSpace/v1/kachoProbeAbsentResource"

// internalSpaceFindings прогоняет перечень через сборку edge и возвращает
// находки. Не падает сам: его зовут и проба (находок ноль), и инъекция
// (находки обязаны быть).
func internalSpaceFindings(t *testing.T, edge http.Handler, sentinel *chainSentinel, c internalSpaceCensus) (findings []string, unmountedInside int) {
	t.Helper()
	for _, b := range loadedHTTPBindings() {
		if b.method == unservedMethod {
			t.Fatalf("метод %q появился в контракте (%s) — близнец больше не «то, чего нет»", unservedMethod, b.fqn)
		}
		if (internalRoute{method: b.method, segs: b.segs}).matches(b.method, strings.Split(strings.TrimPrefix(absentTwinPath, "/"), "/")) {
			t.Fatalf("путь-близнец %s совпал с биндингом %s — он больше не «то, чего нет»", absentTwinPath, b.fqn)
		}
	}
	for _, row := range c.rows {
		got := askThrough(edge, sentinel, row.method, row.path, false)
		var twin gateAsk
		switch row.class {
		case "otherMethod":
			twin = askThrough(edge, sentinel, unservedMethod, row.path, false)
		default:
			twin = askThrough(edge, sentinel, row.method, absentTwinPath, false)
		}
		if got.reached {
			findings = append(findings, "  снаружи дошёл до цепочки аутентификации и прав: "+row.method+" "+row.path+" ("+row.fqn+") -> "+got.shape.String())
		}
		if twin.reached {
			findings = append(findings, "  близнец снаружи дошёл до цепочки: "+row.method+" "+row.path+" ("+row.fqn+")")
		}
		if got.shape != twin.shape {
			findings = append(findings, "  снаружи отвечает не как «маршрута нет»: "+row.method+" "+row.path+" ("+row.fqn+")\n"+
				"      внутренний : "+got.shape.String()+"\n"+
				"      близнец    : "+twin.shape.String())
		}
		wantCode := http.StatusNotFound
		if row.class == "otherMethod" {
			wantCode = http.StatusNotImplemented
		}
		if got.shape.code != wantCode || !gatewayProduced(got.shape) {
			findings = append(findings, "  снаружи не «маршрута нет» производителя grpc-gateway: "+row.method+" "+row.path+" ("+row.fqn+") -> "+got.shape.String())
		}

		inside := askThrough(edge, sentinel, row.method, row.path, true)
		if !inside.reached {
			findings = append(findings, "  близнец внутри не дошёл до цепочки: "+row.method+" "+row.path+" ("+row.fqn+")")
		}
		if inside.shape.code == http.StatusNotFound {
			if row.class == "unbound" {
				// Метод без REST-правила, служба которого не зарегистрирована и на
				// внутреннем mux'е: REST-маршрута у него нет нигде.
				unmountedInside++
				continue
			}
			findings = append(findings, "  близнец внутри отвечает 404 — проба не отличит «маршрута нет снаружи» от «маршрута нет нигде»: "+
				row.method+" "+row.path+" ("+row.fqn+")")
		}
	}
	return findings, unmountedInside
}

// newGateUnderTest — сборка края под пробой: сторож маршрута перед цепочкой.
func newGateUnderTest(t *testing.T) (edge http.Handler, sentinel *chainSentinel, m *Mux) {
	t.Helper()
	m, err := NewMux(context.Background(), probeAddrsAll(t), nil, nil, 30*time.Second)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	sentinel = &chainSentinel{next: m}
	return m.ExternalRouteGate(nil, sentinel), sentinel, m
}

// probeAddrsAll — адреса composition root'а плюс те домены, что root
// объявляет условно: внутренние службы всех доменов обязаны быть смонтированы
// внутри, иначе близнец на внутреннем слушателе ответит 404.
func probeAddrsAll(t *testing.T) map[string]string {
	t.Helper()
	out := probeAddrs(t)
	for _, k := range []string{"geo", "geoInternal", "registry", "registryInternal", "storage", "storageInternal", "loadbalancer", "loadbalancerInternal"} {
		if out[k] == "" {
			out[k] = "127.0.0.1:1"
		}
	}
	return out
}

// TestExternalListener_InternalSpacesAnswerNoRouteBeforeTheChain — предикат
// kacho#3053 на уровне кода.
func TestExternalListener_InternalSpacesAnswerNoRouteBeforeTheChain(t *testing.T) {
	c := deriveInternalSpaces()
	if len(c.rows) == 0 {
		t.Fatal("перечень внутренних пространств пуст — пробе нечего утверждать (дескрипторы не слинкованы?)")
	}
	byClass := map[string]int{}
	spaces := map[string]bool{}
	for _, r := range c.rows {
		byClass[r.class]++
		spaces[r.space] = true
	}
	if byClass["absent"] == 0 {
		t.Fatal("ни один внутренний биндинг не свободен от публичного маршрута — сверка с близнецом «пути нет» выродилась")
	}

	edge, sentinel, _ := newGateUnderTest(t)
	findings, unmounted := internalSpaceFindings(t, edge, sentinel, c)

	// Положительный контроль: публичная поверхность за сторожем не теряется —
	// иначе «маршрута нет» на внутреннем получено тем, что снаружи не отвечает
	// ничего.
	var lost []string
	for _, b := range c.public {
		if got := askThrough(edge, sentinel, b.method, b.path, false); !got.reached {
			lost = append(lost, "  "+b.method+" "+b.path+" ("+b.fqn+") -> "+got.shape.String())
		}
	}
	excludedReached := 0
	for _, e := range c.excluded {
		method, rest, _ := strings.Cut(e, " ")
		tmpl, _, _ := strings.Cut(rest, " ")
		if askThrough(edge, sentinel, method, probePath(tmpl), false).reached {
			excludedReached++
		}
	}

	t.Logf("перепись: опрошено внутренних адресов %d в %d пространствах (нет публичного пути %d · путь публичен под другим методом %d · без REST-правила %d, из них не смонтированы и внутри %d); "+
		"исключено выводом (публичная пара) %d, из них дошли до цепочки %d; публичных биндингов %d, потеряно сторожем %d",
		len(c.rows), len(spaces), byClass["absent"], byClass["otherMethod"], byClass["unbound"], unmounted,
		len(c.excluded), excludedReached, len(c.public), len(lost))

	if len(findings) > 0 {
		t.Errorf("%d находок: внутреннее пространство на внешнем слушателе не отвечает «маршрута нет» до аутентификации (ban06, kacho#3053):\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
	if len(lost) > 0 {
		t.Errorf("%d публичных биндингов сторож не пропустил к цепочке — снаружи они перестали обслуживаться:\n%s",
			len(lost), strings.Join(lost, "\n"))
	}
	if excludedReached != len(c.excluded) {
		t.Errorf("исключённые выводом пары (публичный биндинг под тем же методом) дошли до цепочки %d из %d — вывод исключения разошёлся с маршрутом",
			excludedReached, len(c.excluded))
	}
}

// TestExternalRouteGate_InternalMountedOnTheExternalSideIsRed — инъекция
// настоящим входом: ровно та ошибка сборки, от которой держит условие
// `mux == internalMux`, — административная служба зарегистрирована на стороне
// внешнего слушателя. Проба обязана покраснеть и назвать пары этой службы;
// законный близнец — та же сборка без инъекции — молчит.
func TestExternalRouteGate_InternalMountedOnTheExternalSideIsRed(t *testing.T) {
	c := deriveInternalSpaces()

	edge, sentinel, _ := newGateUnderTest(t)
	clean, _ := internalSpaceFindings(t, edge, sentinel, c)
	if len(clean) != 0 {
		t.Fatalf("законный близнец (сборка без инъекции) не молчит: %d находок\n%s", len(clean), strings.Join(clean, "\n"))
	}

	edge, sentinel, m := newGateUnderTest(t)
	const injected = "kacho.cloud.geo.v1.InternalRegionService/"
	for _, mux := range []*runtime.ServeMux{m.public, m.routes} {
		if err := geopb.RegisterInternalRegionServiceHandlerFromEndpoint(context.Background(), mux, "127.0.0.1:1",
			[]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}); err != nil {
			t.Fatalf("инъекция: %v", err)
		}
	}
	subject := 0
	for _, r := range c.rows {
		if strings.HasPrefix(r.fqn, injected) {
			subject++
		}
	}
	if subject == 0 {
		t.Fatalf("в перечне нет ни одной пары %s — инъекции нечего ронять", injected)
	}
	findings, _ := internalSpaceFindings(t, edge, sentinel, c)
	named, foreign := 0, 0
	for _, f := range findings {
		if strings.Contains(f, injected) {
			named++
		} else {
			foreign++
		}
	}
	t.Logf("инъекция: пар внедрённой службы %d, находок %d, из них названа внедрённая служба %d, чужих %d", subject, len(findings), named, foreign)
	if named == 0 {
		t.Fatalf("внутренняя служба смонтирована на внешний слушатель, а проба зелёная — она не видит предмета ban06")
	}
	if foreign != 0 {
		t.Fatalf("инъекция одной службы дала %d находок о других — красное пришло от соседа:\n%s", foreign, strings.Join(findings, "\n"))
	}
}

// TestExternalRouteGate_NeverCallsABackendOnItsOwn — отказ fail-closed: на
// любой ветке сторож либо отдаёт запрос цепочке, либо отвечает промахом; моста
// к бэкенду он не вызывает. Проверяется по транспорту: все бэкенды указывают на
// слушатель-счётчик, цепочка подменена ответом без моста.
func TestExternalRouteGate_NeverCallsABackendOnItsOwn(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })
	var accepted atomic.Int64
	firstAccept := make(chan struct{}, 1)
	go func() {
		for {
			conn, aErr := lis.Accept()
			if aErr != nil {
				return
			}
			accepted.Add(1)
			select {
			case firstAccept <- struct{}{}:
			default:
			}
			_ = conn.Close()
		}
	}()
	addrs := probeAddrsAll(t)
	for k, v := range addrs {
		if v != "" {
			addrs[k] = lis.Addr().String()
		}
	}
	m, err := NewMux(context.Background(), addrs, nil, nil, 2*time.Second)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	stub := &chainSentinel{next: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	edge := m.ExternalRouteGate(nil, stub)

	c := deriveInternalSpaces()
	asked := 0
	for _, r := range c.rows {
		askThrough(edge, stub, r.method, r.path, false)
		asked++
	}
	for _, b := range c.public {
		askThrough(edge, stub, b.method, b.path, false)
		asked++
	}
	askThrough(edge, stub, http.MethodGet, absentTwinPath, false)
	asked++
	if n := accepted.Load(); n != 0 {
		t.Fatalf("сторож дозвонился до бэкенда %d раз на %d запросах — он обслуживает в обход цепочки", n, asked)
	}

	// Положительный контроль счётчика: диспетчер за сторожем до бэкенда
	// дозванивается — иначе «ноль соединений» значил бы, что счётчик слеп.
	pub := c.public[0]
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(pub.method, pub.path, strings.NewReader("{}")))
	select {
	case <-firstAccept:
	case <-time.After(5 * time.Second):
	}
	t.Logf("запросов через сторож %d, соединений с бэкендом 0; контроль: диспетчер напрямую -> %d, соединений %d",
		asked, rec.Code, accepted.Load())
	if accepted.Load() == 0 {
		t.Fatal("диспетчер напрямую не дозвонился до слушателя-счётчика — счётчик ничего не различает")
	}
}

// TestExternalRouteGate_OwnEdgePathsAndOriginalRequestReachTheChain — свои пути
// края (любой шаблон own, кроме `/`) уходят цепочке; цепочке уходит ИСХОДНЫЙ
// запрос — с телом и с методом, который прислал вызывающий.
func TestExternalRouteGate_OwnEdgePathsAndOriginalRequestReachTheChain(t *testing.T) {
	m, err := NewMux(context.Background(), probeAddrsAll(t), nil, nil, 30*time.Second)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	var gotMethod, gotBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	own := http.NewServeMux()
	own.HandleFunc("/kachoProbeOwnEdgePath", func(http.ResponseWriter, *http.Request) {})
	own.Handle("/", next)
	edge := m.ExternalRouteGate(own, next)

	rec := httptest.NewRecorder()
	edge.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/kachoProbeOwnEdgePath", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("свой путь края не дошёл до цепочки: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	edge.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, absentTwinPath, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("общий шаблон own `/` принят за свой путь края: путь, которого нет, дошёл до цепочки (%d)", rec.Code)
	}

	// Публичный биндинг с телом.
	var pub publicBinding
	for _, b := range deriveInternalSpaces().public {
		if b.method == http.MethodPost {
			pub = b
			break
		}
	}
	if pub.path == "" {
		t.Fatal("в контракте нет публичного POST-биндинга — проверять сохранность тела не на чем")
	}
	rec = httptest.NewRecorder()
	edge.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, pub.path, strings.NewReader(`{"kachoProbe":"body"}`)))
	if rec.Code != http.StatusNoContent || gotMethod != http.MethodPost || gotBody != `{"kachoProbe":"body"}` {
		t.Fatalf("цепочке ушёл не исходный запрос: код %d, метод %q, тело %q", rec.Code, gotMethod, gotBody)
	}

	// Форма с подменой метода: таблица маршрутов разбирает форму и меняет метод
	// у СВОЕЙ копии; цепочка получает запрос нетронутым.
	var get publicBinding
	for _, b := range deriveInternalSpaces().public {
		if b.method == http.MethodGet {
			get = b
			break
		}
	}
	req := httptest.NewRequest(http.MethodPost, get.path, strings.NewReader("a=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-HTTP-Method-Override", http.MethodGet)
	rec = httptest.NewRecorder()
	edge.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || gotMethod != http.MethodPost || gotBody != "a=b" {
		t.Fatalf("форма с подменой метода: цепочке ушёл изменённый запрос: код %d, метод %q, тело %q", rec.Code, gotMethod, gotBody)
	}
}
