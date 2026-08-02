// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// external_refusal_shape_test.go — на внешнем листенере укрытый
// административный маршрут обязан отвечать ТЕМ ЖЕ ПРОИЗВОДИТЕЛЕМ, что и любой
// другой запрос, и ни один административный бэкенд не должен быть с него
// достижим.
//
// ЧТО УЖЕ ПРИШПИЛЕНО НА СОСЕДНЕЙ ПОЛОСЕ. У gRPC свойство доказано строго:
// `TestExternalListener_InternalMethodAnswersExactlyAsAMethodThatDoesNotExist`
// (internal/proxy) сверяет код, текст с вырезанным эхом самого запроса И ЧИСЛО
// деталей статуса. Отказ там производит ОДНА функция (`refuseRoute`), поэтому
// формам неоткуда разойтись.
//
// ЧЕГО НЕ БЫЛО НА ЭТОЙ. У REST отказ производили ДВА РАЗНЫХ производителя:
// укрытие вызывало `http.NotFound`, обычный промах отдавал grpc-gateway. Тела и
// заголовки у них разные — `text/plain` + «404 page not found» + заголовок
// `X-Content-Type-Options: nosniff` против `application/json` + `{"code":5,…}`.
// То есть ТИП СОДЕРЖИМОГО ответа отвечал на вопрос «есть ли здесь
// административный путь», и вся админ-поверхность перечислялась снаружи без
// единого удостоверения. Замер: 34 административных биндинга из 34 отличались.
//
// ЧТО УТВЕРЖДАЕТСЯ ЗДЕСЬ. Два разных факта, и ни один не выводится из другого:
//
//  1. ПРОИЗВОДИТЕЛЬ ОДИН. Ответ на административный путь неотличим по
//     производителю от ответа на всё остальное. Проверяется в паре: тот самый
//     `http.NotFound`, что стоял здесь раньше, обязан этот предикат НЕ проходить —
//     иначе он не измеряет ничего.
//  2. АДМИНИСТРАТИВНЫЙ БЭКЕНД НЕДОСТИЖИМ. Укрытие реализовано тем, что запрос
//     отдаётся publicMux'у, а publicMux несёт только публичные маршруты. Если бы
//     административные регистрации там остались, укрытие ОБСЛУЖИЛО БЫ их наружу —
//     то есть лечение оказалось бы хуже болезни. Проверяется по адресу: публичные
//     и административные бэкенды разведены на два разных мёртвых литерала, и в
//     ответе внешнего листенера административный литерал не должен появляться
//     НИКОГДА.
//
// ПОЧЕМУ НЕ «СРАВНИТЬ С БЛИЗНЕЦОМ» ЦЕЛИКОМ. Часть административных биндингов
// делит путь с публичным маршрутом (admin-CRUD каталога DiskType — тот же
// collection-путь, что публичное чтение; `…/networks/{id}:internal` матчится
// публичным `…/networks/{network_id}` как значение переменной). На таком пути
// «маршрута нет» просто неверно: маршрут есть, публичный, и вызывающий обязан
// получить именно его ответ. Сверка с близнецом применяется там, где она имеет
// смысл, — на путях, где публичного маршрута нет, — и её охват назван числом.
package restmux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// answerShape — то, что видит вызывающий: код, тип содержимого, тело и
// заголовок, который ставит только один из двух производителей.
type answerShape struct {
	code   int
	ctype  string
	body   string
	nosnif string
}

func (a answerShape) String() string {
	return itoaShape(a.code) + " ct=" + a.ctype + " nosniff=" + a.nosnif + " body=" + a.body
}

func itoaShape(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func askExternal(t *testing.T, h http.Handler, method, path string) answerShape {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	// Внешнее происхождение — fail-closed умолчание: маркера нет.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return answerShape{
		code:   rec.Code,
		ctype:  rec.Header().Get("Content-Type"),
		body:   rec.Body.String(),
		nosnif: rec.Header().Get("X-Content-Type-Options"),
	}
}

// unservedMethod — HTTP-метод, которого нет ни в одном биндинге контракта.
// Проверяется, а не предполагается (премиса в самом гейте).
const unservedMethod = "TRACE"

// gatewayProduced — ответ произведён маршалером grpc-gateway, а не отдельной
// рукописной веткой. Признак положительный (тип содержимого и форма тела), а не
// «не text/plain»: корзины «всё остальное» не бывает.
func gatewayProduced(a answerShape) bool {
	return strings.HasPrefix(a.ctype, "application/json") &&
		a.nosnif == "" &&
		strings.Contains(a.body, `"code"`) &&
		strings.Contains(a.body, `"message"`)
}

// splitAddrs — карта адресов, в которой ПУБЛИЧНЫЕ и АДМИНИСТРАТИВНЫЕ бэкенды
// разведены на два разных мёртвых литерала. Тело ответа grpc-gateway при
// недозвоне цитирует адрес, поэтому появление административного литерала в
// ответе ВНЕШНЕГО листенера — прямое доказательство того, что запрос дошёл до
// административного бэкенда.
func splitAddrs(t *testing.T) (addrs map[string]string, adminLiteral string) {
	t.Helper()
	src := probeAddrs(t)
	const publicDead = "127.0.0.1:1"
	adminLiteral = "127.0.0.1:2"
	out := make(map[string]string, len(src))
	admins := 0
	for k, v := range src {
		if v == "" {
			out[k] = ""
			continue
		}
		if strings.HasSuffix(k, "Internal") {
			out[k] = adminLiteral
			admins++
			continue
		}
		out[k] = publicDead
	}
	if admins == 0 {
		t.Fatal("в карте адресов нет ни одного административного ключа — у пробы нет предмета")
	}
	return out, adminLiteral
}

// TestExternalListener_HiddenAdminRouteIsAnsweredByTheSameProducer — факт 1.
func TestExternalListener_HiddenAdminRouteIsAnsweredByTheSameProducer(t *testing.T) {
	addrs := probeAddrs(t)
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	subject := internalBindingsFromDescriptors()
	if len(subject) < minInternalBindings {
		t.Fatalf("административных REST-биндингов в дескрипторах %d (< %d) — предмета у пробы нет",
			len(subject), minInternalBindings)
	}

	// Премиса: метод-близнец действительно не обслуживается ни одним биндингом.
	for _, b := range loadedHTTPBindings() {
		if b.method == unservedMethod {
			t.Fatalf("метод %q появился в контракте (%s) — близнец больше не «то, чего нет»",
				unservedMethod, b.fqn)
		}
	}

	var foreign []string
	var twinMismatch []string
	shapes := map[string]int{}
	comparable := 0
	for _, b := range subject {
		got := askExternal(t, h, b.method, b.path)
		shapes[got.String()]++
		if !gatewayProduced(got) {
			foreign = append(foreign, "  "+b.method+" "+b.path+" ("+b.fqn+")\n      "+got.String())
			continue
		}
		// Сверка с близнецом — только там, где она имеет смысл: близнец
		// (неподдерживаемый метод на том же пути) отвечает «маршрута нет» ⇒
		// публичного маршрута на этом пути тоже нет.
		twin := askExternal(t, h, unservedMethod, b.path)
		if twin.code != http.StatusNotFound {
			continue
		}
		comparable++
		if got != twin {
			twinMismatch = append(twinMismatch, "  "+b.method+" "+b.path+" ("+b.fqn+")\n"+
				"      укрытый     : "+got.String()+"\n"+
				"      которого нет: "+unservedMethod+" "+b.path+" -> "+twin.String())
		}
	}

	t.Logf("перепись: %d административных биндингов, %d различных форм ответа, "+
		"%d путей без публичного маршрута сверены с близнецом",
		len(subject), len(shapes), comparable)
	if comparable == 0 {
		t.Fatal("ни один административный путь не оказался свободен от публичного маршрута — " +
			"сверка с близнецом выродилась, и её молчание ничего не значит")
	}

	if len(foreign) > 0 {
		t.Errorf("%d из %d административных REST-биндингов отвечают на внешнем листенере ответом "+
			"ЧУЖОГО производителя. Форма ответа отвечает на вопрос «есть ли здесь административный "+
			"путь» — это оракул существования, перечисляющий админ-поверхность без единого "+
			"удостоверения.\n%s", len(foreign), len(subject), strings.Join(foreign, "\n"))
	}
	if len(twinMismatch) > 0 {
		t.Errorf("%d административных путей, на которых публичного маршрута НЕТ, отвечают не так, "+
			"как этот же листенер отвечает на маршрут, которого у него нет:\n%s",
			len(twinMismatch), strings.Join(twinMismatch, "\n"))
	}
}

// TestExternalRefusalProbe_RejectsASecondProducer — отрицание в паре с
// положительным: берётся ровно тот производитель, что стоял здесь раньше
// (`http.NotFound`), и предикат обязан сказать «чужой». Без этого «ноль чужих»
// значило бы лишь, что предикат истинен на всём.
func TestExternalRefusalProbe_RejectsASecondProducer(t *testing.T) {
	plain := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	subject := internalBindingsFromDescriptors()
	if len(subject) == 0 {
		t.Fatal("предмета нет — контроль потерял смысл")
	}
	var accepted []string
	for _, b := range subject {
		if a := askExternal(t, plain, b.method, b.path); gatewayProduced(a) {
			accepted = append(accepted, b.method+" "+b.path+" -> "+a.String())
		}
	}
	if len(accepted) > 0 {
		t.Fatalf("предикат принял ответ ОТДЕЛЬНОГО производителя (%d из %d) — значит его молчание "+
			"в соседнем тесте не доказывает ничего:\n  %s",
			len(accepted), len(subject), strings.Join(accepted, "\n  "))
	}

	// Законный близнец: настоящий ответ grpc-gateway тот же предикат обязан
	// принять — иначе он отвергает всё подряд, и его красное значит не больше
	// его зелёного.
	addrs := probeAddrs(t)
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	miss := askExternal(t, h, "GET", "/vpc/v1/kachoProbeAbsentResource")
	if !gatewayProduced(miss) {
		t.Fatalf("предикат отверг обычный промах grpc-gateway (%s) — он опознаёт не производителя, "+
			"а что-то другое", miss)
	}
}

// TestExternalListener_NeverReachesAnAdminBackend — факт 2, и он несущий.
//
// Укрытие реализовано делегированием publicMux'у. Свойство «publicMux несёт
// только публичные маршруты» держится тем, что административные регистрации
// ограничены `mux == internalMux`. Стоит этому измениться — и укрытие начнёт
// ОБСЛУЖИВАТЬ административную поверхность наружу.
//
// Проверяется по адресу, а не по коду: публичные бэкенды и административные
// разведены на разные мёртвые литералы, и тело ответа grpc-gateway цитирует тот,
// до которого он пытался дозвониться.
func TestExternalListener_NeverReachesAnAdminBackend(t *testing.T) {
	addrs, adminLiteral := splitAddrs(t)
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	// Премиса: на ВНУТРЕННЕМ листенере административный литерал действительно
	// появляется. Иначе дискриминатор «литерал в теле» ничего не различает и его
	// отсутствие снаружи — не свойство, а артефакт.
	seenInternally := 0
	for _, b := range internalBindingsFromDescriptors() {
		req := httptest.NewRequest(b.method, b.path, nil)
		req = req.WithContext(listenerorigin.WithInternal(req.Context()))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), adminLiteral) {
			seenInternally++
		}
	}
	if seenInternally == 0 {
		t.Fatalf("административный литерал %q не появился НИ РАЗУ даже на внутреннем листенере — "+
			"дискриминатор не работает, и «ноль снаружи» ничего не значит", adminLiteral)
	}

	var leaked []string
	for _, b := range internalBindingsFromDescriptors() {
		if a := askExternal(t, h, b.method, b.path); strings.Contains(a.body, adminLiteral) {
			leaked = append(leaked, "  "+b.method+" "+b.path+" ("+b.fqn+")\n      "+a.String())
		}
	}
	// Публичные биндинги обязаны продолжать ходить в ПУБЛИЧНЫЙ бэкенд — иначе
	// проба зелена оттого, что снаружи вообще ничего не работает.
	publicReached := 0
	for _, b := range publicBindingsFromDescriptors() {
		if a := askExternal(t, h, b.method, b.path); strings.Contains(a.body, "127.0.0.1:1") {
			publicReached++
		}
	}
	t.Logf("перепись: административный литерал виден на внутреннем листенере у %d биндингов, "+
		"снаружи у %d; публичных биндингов, дошедших до публичного бэкенда снаружи: %d",
		seenInternally, len(leaked), publicReached)
	if publicReached == 0 {
		t.Fatal("ни один публичный биндинг не дошёл до бэкенда снаружи — внешний листенер вообще " +
			"ничего не обслуживает, и «утечек нет» получено даром")
	}
	if len(leaked) > 0 {
		t.Errorf("%d административных биндингов ДОСТИГЛИ административного бэкенда с ВНЕШНЕГО "+
			"листенера: укрытие отдаёт запрос mux'у, на котором административные маршруты "+
			"зарегистрированы, и потому обслуживает их наружу (ban #6).\n%s",
			len(leaked), strings.Join(leaked, "\n"))
	}
}
