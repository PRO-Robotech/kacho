// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// own_session_record_forward_test.go — слой края исхода (а) kaname#133:
// ссылка на запись сессии (приёмка kaname
// docs/engineering/acceptance/assurance-level-is-declared-by-our-session.md,
// Ф11-45…47; kacho#1280).
//
// ПРЕДМЕТ. Ответ службы о живой сессии S называет ссылку R_S на запись этой
// сессии. Край пересылает её следующему звену ключом каталога фундамента
// `principalwire.MetaTokenSessionID` дословно, ничего в ней не сопоставляя, и
// только с полосы нашей сессии. Клиент назвать её не может: всё пространство
// `x-kacho-` снимается до выбора полосы. Наружу она не выходит.
//
// ОСНАСТКА. Одна цепочка `AuthInterceptor.HTTP` несёт обе полосы, которые
// сравнивают сценарии: полосу нашей сессии (дублёр ответа службы о носителе —
// подстановка законна, предмет — что край делает с ответом, §8 приёмки) и
// полосу предъявителя (токен RS256 той же фикстуры, что у пробы личности из
// утверждений). Следующее звено записывает ВСЕ значения ключа ссылки в любой
// поверхностной форме — голой и мостовой — по нормализованному имени, а не
// по одной выписанной строке: форма, которую проба не назвала, не ускользает.
//
// ОБЪЁМ. Ф11-45 утверждает у следующего звена ОБЕ поверхностные формы ключа —
// голую и мостовую, как у уровня подтверждения, — каждую ровно с одним
// значением R_S, и третьей формы нет. Что служба сквозь мост видит ровно одно
// значение, держит restmux TestEdgeForwardsTheSessionRecordAndDropsTheClientOne,
// а сведение двух форм к одному значению — проба строителя в principalmeta.
// Ф11-46 и Ф11-47 сравнивают исходы и утверждают отсутствие клиентского
// значения в любой форме.

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

const (
	recordUserU   = "usr-0000000000001280u"
	recordCarrier = "carrier-1280-s"
	// recordS — ссылка R_S, которую служба называет в ответе о сессии S.
	recordS = "hss-00000000000001280s"
	// recordV — ссылка R_V на сессию другой личности, приложенная клиентом.
	recordV = "hss-00000000000001280v"
	// recordRoute — глагол без пола: исход не зависит от уровня.
	recordRoute = "/iam/v1/projects"
)

// recordSessionReader — дублёр ответа службы о носителе: носитель S — живая
// сессия личности U уровня «1» со ссылкой R_S; прочие носители — «сессии нет».
type recordSessionReader struct{ asked int }

func (r *recordSessionReader) ResolveHumanSession(_ context.Context, bearer string) (middleware.HumanSession, bool, error) {
	r.asked++
	if bearer != recordCarrier {
		return middleware.HumanSession{}, false, nil
	}
	at := time.Now().Add(-time.Minute).Truncate(time.Second)
	return middleware.HumanSession{
		UserID: recordUserU, Email: "u1280@example.test", DisplayName: "U1280",
		AuthenticatedAt: at, ExpiresAt: at.Add(time.Hour),
		AssuranceLevel: "1", EmailVerified: true, SessionID: recordS,
	}, true, nil
}

// recordNext — следующее звено: значения ключа ссылки по поверхностным формам
// (имя заголовка как пришло → значения) и пересланная личность.
type recordNext struct {
	served    int
	forwarded map[string][]string
	principal string
}

func (n *recordNext) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.served++
	n.forwarded = map[string][]string{}
	for name, vals := range r.Header {
		if key, ok := principalmeta.KachoNamespaceKey(name); ok && key == principalmeta.MetaTokenSessionID {
			n.forwarded[name] = append([]string(nil), vals...)
		}
	}
	n.principal = r.Header.Get(principalmeta.HeaderPrincipalID)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"projects":[]}`))
}

type recordRig struct {
	reader *recordSessionReader
	next   *recordNext
	chain  http.Handler
	fix    *jwksFixture
	log    *bytes.Buffer
}

func newRecordRig(t *testing.T) *recordRig {
	t.Helper()
	fix := newJWKSFixture(t, "RS256")
	reader := &recordSessionReader{}
	next := &recordNext{}
	log := &bytes.Buffer{}
	a := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", &countingLookup{},
		slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))).
		WithHumanSession(reader).
		WithVerifier(rs256Verifier(t, fix))
	return &recordRig{reader: reader, next: next, chain: a.HTTP(next), fix: fix, log: log}
}

// forgeRecordBothForms — клиент прикладывает ключ ссылки в обеих поверхностных
// формах со значением R_V.
func forgeRecordBothForms(req *http.Request) {
	req.Header.Set(principalmeta.HeaderTokenSessionID, recordV)
	req.Header.Set(principalmeta.HeaderGRPCMetaTokenSessionID, recordV)
}

func (r *recordRig) bySession(arrange func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, recordRoute, nil)
	req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: recordCarrier})
	if arrange != nil {
		arrange(req)
	}
	rec := httptest.NewRecorder()
	r.chain.ServeHTTP(rec, req)
	return rec
}

func (r *recordRig) byBearer(t *testing.T, arrange func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, recordRoute, nil)
	req.Header.Set("Authorization", "Bearer "+r.fix.sign(t, issuerClaims("user", recordUserU)))
	if arrange != nil {
		arrange(req)
	}
	rec := httptest.NewRecorder()
	r.chain.ServeHTTP(rec, req)
	return rec
}

// requireNothingOutward — ответ клиенту не несёт ни ключа ссылки (в любой
// форме, по нормализованному имени), ни названных значений — ни в заголовках,
// ни в теле.
func requireNothingOutward(t *testing.T, rec *httptest.ResponseRecorder, values ...string) {
	t.Helper()
	for name, vals := range rec.Result().Header {
		if key, ok := principalmeta.KachoNamespaceKey(name); ok && key == principalmeta.MetaTokenSessionID {
			t.Errorf("ответ клиенту несёт ключ ссылки %q = %q", name, vals)
		}
		for _, v := range vals {
			for _, s := range values {
				if strings.Contains(v, s) {
					t.Errorf("заголовок ответа %q несёт значение %q", name, s)
				}
			}
		}
	}
	for _, s := range values {
		if strings.Contains(rec.Body.String(), s) {
			t.Errorf("тело ответа несёт значение %q: %s", s, rec.Body.String())
		}
	}
}

// requireBothForms — у следующего звена ключ ссылки ровно в двух поверхностных
// формах, голой и мостовой, каждая — ровно [want]; других форм ключа нет.
func (n *recordNext) requireBothForms(t *testing.T, want string) {
	t.Helper()
	wantForms := map[string][]string{
		http.CanonicalHeaderKey(principalmeta.HeaderTokenSessionID):         {want},
		http.CanonicalHeaderKey(principalmeta.HeaderGRPCMetaTokenSessionID): {want},
	}
	if !reflect.DeepEqual(n.forwarded, wantForms) {
		t.Fatalf("следующее звено получило ключ ссылки %v, want %v — обе формы, в каждой ссылка из ответа службы, дословно и одна",
			n.forwarded, wantForms)
	}
}

// allForwardedValues — значения ключа ссылки у следующего звена во всех формах.
func (n *recordNext) allForwardedValues() []string {
	var out []string
	for _, vals := range n.forwarded {
		out = append(out, vals...)
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Предпосылка: дублёр службы отвечает о носителе S живой сессией со ссылкой
// R_S, а о прочих — «сессии нет». Без неё «ключа нет у следующего звена» было
// бы зелено по пустому входу.

func TestOwnSessionRecord_Precondition_ServiceDoubleNamesTheRecord(t *testing.T) {
	r := &recordSessionReader{}
	sess, found, err := r.ResolveHumanSession(context.Background(), recordCarrier)
	if err != nil || !found || sess.SessionID != recordS || sess.UserID != recordUserU {
		t.Fatalf("дублёр о носителе S: found=%v err=%v sess=%+v — ожидалась живая сессия U со ссылкой %q",
			found, err, sess, recordS)
	}
	if _, found, _ := r.ResolveHumanSession(context.Background(), "other"); found {
		t.Fatal("дублёр признал чужой носитель — полоса предъявителя не была бы отличима от полосы сессии")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф11-45 — полоса нашей сессии, ответ службы о S называет R_S: запрос проходит;
// следующее звено получает ключ ссылки в обеих формах со значением R_S дословно; ответ
// клиенту не несёт ни ключа, ни значения R_S.

func TestOwnSessionRecord_F11_45_SessionLaneForwardsTheServiceRecordVerbatimAndNotOutward(t *testing.T) {
	rig := newRecordRig(t)
	rec := rig.bySession(nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("запрос по носителю S обязан проходить: %d %s", rec.Code, rec.Body.String())
	}
	if rig.reader.asked == 0 || rig.next.served != 1 || rig.next.principal != recordUserU {
		t.Fatalf("предпосылка: полоса нашей сессии не исполнена — asked=%d served=%d principal=%q",
			rig.reader.asked, rig.next.served, rig.next.principal)
	}
	rig.next.requireBothForms(t, recordS)
	requireNothingOutward(t, rec, recordS)
	if strings.Contains(rig.log.String(), recordS) {
		t.Errorf("ссылка R_S попала в журнал пути запроса:\n%s", rig.log.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф11-46 — близнец Ф11-45, один факт: клиент приложил ключ ссылки в обеих
// формах со значением R_V. Исход побайтово равен Ф11-45; R_V не читается нигде.

func TestOwnSessionRecord_F11_46_ClientRecordInBothFormsIsNotRead(t *testing.T) {
	rig := newRecordRig(t)
	plain := rig.bySession(nil)
	plainForwarded := rig.next.forwarded
	plainPrincipal := rig.next.principal

	forged := rig.bySession(forgeRecordBothForms)

	if forged.Code != plain.Code || forged.Body.String() != plain.Body.String() ||
		!reflect.DeepEqual(forged.Result().Header, plain.Result().Header) {
		t.Fatalf("исход с приложенным R_V отличается от Ф11-45:\n%d %v %s\n%d %v %s",
			forged.Code, forged.Result().Header, forged.Body.String(),
			plain.Code, plain.Result().Header, plain.Body.String())
	}
	if !reflect.DeepEqual(rig.next.forwarded, plainForwarded) || rig.next.principal != plainPrincipal {
		t.Fatalf("следующее звено получило %v (личность %q), а у Ф11-45 — %v (личность %q)",
			rig.next.forwarded, rig.next.principal, plainForwarded, plainPrincipal)
	}
	// Положительный контроль пары к Ф11-47: на этой оснастке ключ ссылки
	// следующим звеном виден.
	rig.next.requireBothForms(t, recordS)
	for _, v := range rig.next.allForwardedValues() {
		if v == recordV {
			t.Fatalf("клиентское значение %q доехало до следующего звена", recordV)
		}
	}
	requireNothingOutward(t, forged, recordS, recordV)
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф11-47 — близнец Ф11-46, один факт: запрос идёт полосой предъявителя —
// токеном RS256 личности U, нашей сессии за ним нет; клиент приложил ключ
// ссылки в обеих формах со значением R_V. Запрос проходит; следующее звено не
// получает ключа ссылки ни в одной форме.

func TestOwnSessionRecord_F11_47_BearerLaneForwardsNoRecordInAnyForm(t *testing.T) {
	rig := newRecordRig(t)
	rec := rig.byBearer(t, forgeRecordBothForms)

	if rec.Code != http.StatusOK {
		t.Fatalf("запрос полосой предъявителя обязан проходить: %d %s", rec.Code, rec.Body.String())
	}
	if rig.reader.asked != 0 || rig.next.served != 1 || rig.next.principal != recordUserU {
		t.Fatalf("предпосылка: полоса предъявителя не исполнена либо за ней спрошена сессия — asked=%d served=%d principal=%q",
			rig.reader.asked, rig.next.served, rig.next.principal)
	}
	if len(rig.next.forwarded) != 0 {
		t.Fatalf("следующее звено получило ключ ссылки на полосе предъявителя: %v", rig.next.forwarded)
	}
	requireNothingOutward(t, rec, recordS, recordV)

	// Положительный контроль на той же оснастке: полоса нашей сессии ключ
	// ставит — иначе «ключа нет» было бы зелено на крае, не пересылающем ничего.
	rig.bySession(forgeRecordBothForms)
	rig.next.requireBothForms(t, recordS)
}
