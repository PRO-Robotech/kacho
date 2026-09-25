// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEdgeKnowsExactlyOneSessionCarrierOurs — край читает и гасит РОВНО одно
// имя носителя браузерной сессии, наше (#2792).
//
// Чужое имя в перечне гашения — это печенье, которое край стирает у клиента, не
// читая его нигде: выход и отказ по отсечке выдают гашение имени без читателя, а
// там, где чужая сторона ставит печенью `Domain`, гашение края с ним не
// совпадает вовсе и отказ становится стоящим. Снимается вместе с читателем.
func TestEdgeKnowsExactlyOneSessionCarrierOurs(t *testing.T) {
	names := SessionCarrierNames()
	if len(names) != 1 || names[0] != OurSessionCarrierName {
		t.Fatalf("перечень носителей сессии края = %q, ожидался ровно наш %q: имя без "+
			"читателя гасится у клиента без основания", names, OurSessionCarrierName)
	}
	endings := SessionCarrierEndings()
	if len(endings) != 1 || endings[0].Name != OurSessionCarrierName {
		got := make([]string, 0, len(endings))
		for _, c := range endings {
			got = append(got, c.Name)
		}
		t.Fatalf("край гасит %q, ожидался ровно наш носитель %q", got, OurSessionCarrierName)
	}
}

// Граница имени: носитель — печенье ровно с нашим именем. Имя, оказавшееся
// ЧАСТЬЮ чужого имени печенья или его ЗНАЧЕНИЕМ, предъявлением не является.
// Половины парные — и сужение обязано не съесть законное.
func TestOurCarrierPredicate_MatchesTheNameAndNotItsSubstring(t *testing.T) {
	cases := []struct {
		name    string
		cookies []*http.Cookie
		want    bool
	}{
		{"своё имя — предъявлено", []*http.Cookie{{Name: OurSessionCarrierName, Value: "v"}}, true},
		{"рядом с чужим — предъявлено", []*http.Cookie{
			{Name: foreignSessionCarrierName, Value: "f"},
			{Name: OurSessionCarrierName, Value: "v"},
		}, true},
		{"имя как ПРИСТАВКА чужого печенья", []*http.Cookie{
			{Name: OurSessionCarrierName + "_debug", Value: "v"}}, false},
		{"имя как ОКОНЧАНИЕ чужого печенья", []*http.Cookie{
			{Name: "x_" + OurSessionCarrierName, Value: "v"}}, false},
		{"имя в ЗНАЧЕНИИ чужого печенья", []*http.Cookie{
			{Name: "note", Value: OurSessionCarrierName}}, false},
		{"печенья нет вовсе", nil, false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, platformPath, nil)
		for _, c := range tc.cookies {
			req.AddCookie(c)
		}
		if _, got := ourSessionCarrierOf(req); got != tc.want {
			t.Errorf("%s: предикат ответил %v, ожидалось %v (заголовок %q)",
				tc.name, got, tc.want, req.Header.Get("Cookie"))
		}
	}
	t.Logf("перепись: форм заголовка проверено %d · положительных 2 · отрицательных 4", len(cases))
}

// Значение, которого браузер провести НЕ МОЖЕТ, носителем не является.
//
// Выбор fail-closed и назван вслух: принять неразбираемое значение значило бы
// передать службе то, чего мы сами не прочитали, а отказ здесь неотличим для
// человека от «сессии нет» — состояния, в котором он и находится, раз его
// печенье до нас не доехало целым.
func TestCarrierPredicates_AValueNoBrowserCanFrameIsNotACarrier(t *testing.T) {
	// Октеты вне RFC 6265 для значения печенья: разбор их отвергает, а клиент
	// Go при отправке печатает «dropping invalid bytes».
	const unframeable = "знач;ение"
	req := httptest.NewRequest(http.MethodGet, platformPath, nil)
	req.Header.Set("Cookie", OurSessionCarrierName+"="+unframeable)
	if _, ours := ourSessionCarrierOf(req); ours {
		t.Error("наш носитель признан предъявленным на значении, которого браузер провести не может")
	}

	// Положительная половина: то же имя с ПРОВОДИМЫМ значением — носитель.
	// Без неё проба зеленела бы и на предикате, отвергающем всё подряд.
	ok := httptest.NewRequest(http.MethodGet, platformPath, nil)
	ok.AddCookie(&http.Cookie{Name: OurSessionCarrierName, Value: "v-own"})
	if _, ours := ourSessionCarrierOf(ok); !ours {
		t.Fatal("наш носитель с проводимым значением не признан — предикат отвергает законный вход")
	}
}
