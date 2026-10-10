// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notify_binding_routability_test.go — маршруты notify следуют ОБЪЯВЛЕННОЙ
// установке, и только ей.
//
// У notify один слушатель — внутренний слушатель notify-api — и одно поле адреса
// без умолчания (NTF-4 Р20, NTF-5 З18). Значит, у маршрутов notify ровно два
// законных состояния: адрес объявлен — все биндинги notify отмаршрутизированы
// (публичные NoticeService — на обоих mux'ах, InternalNoticeService — только на
// внутреннем); адрес не объявлен — ни одного, и вызывающий получает отказ
// маршрута, а не вызов по пустому адресу.
package restmux

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

const notifyPkg = "kacho.cloud.notify.v1."

// notifyProbeService — служба стендовой пробы notify-probe: тот же пакет
// контракта, но другой корень и своя ручка адреса. Её биндинг следует
// объявлению адреса пробы, а не notify-api, и судится своим близнецом
// (TestNotifyProbeRouteIsInternalOnly), поэтому из предмета notify-api выведен.
const notifyProbeService = notifyPkg + "InternalNotifyProbeService/"

// notifyBindings — биндинги, которые служит notify-api.
func notifyBindings(subject []publicBinding) []publicBinding {
	var out []publicBinding
	for _, b := range subject {
		if strings.HasPrefix(b.fqn, notifyPkg) && !strings.HasPrefix(b.fqn, notifyProbeService) {
			out = append(out, b)
		}
	}
	return out
}

// TestNotifyBindingsFollowTheDeclaredInstallation — обе стороны одной оси на
// одном предмете: отличается только объявление адреса notify.
func TestNotifyBindingsFollowTheDeclaredInstallation(t *testing.T) {
	public := notifyBindings(publicBindingsFromDescriptors())
	internal := notifyBindings(internalBindingsFromDescriptors())
	if len(public) == 0 || len(internal) == 0 {
		t.Fatalf("биндингов notify в дескрипторах: публичных %d, административных %d — "+
			"предмет пробы потерян", len(public), len(internal))
	}
	t.Logf("предмет: %d публичных и %d административных биндингов notify", len(public), len(internal))

	t.Run("declared", func(t *testing.T) {
		addrs := probeAddrs(t)
		if addrs["notifyInternal"] == "" {
			t.Fatal("адрес notify объявлен, а ключа notifyInternal в карте composition root'а нет")
		}
		if _, ok := addrs["notify"]; ok {
			t.Fatal("в карте адресов есть второй ключ notify — у notify одно поле адреса (З18 п.1)")
		}
		if missing := unrouted(t, addrs, public); len(missing) > 0 {
			t.Fatalf("адрес notify объявлен, а %d публичных биндингов без маршрута (первый — %s %s)",
				len(missing), missing[0].method, missing[0].path)
		}
		if missing, _ := servedOnInternalOrigin(t, addrs, internal); len(missing) > 0 {
			t.Fatalf("адрес notify объявлен, а %d административных биндингов без маршрута "+
				"на внутреннем листенере (первый — %s %s)", len(missing), missing[0].method, missing[0].path)
		}
	})

	t.Run("undeclared", func(t *testing.T) {
		t.Setenv(notifyAddrKnob, "")
		addrs := loadProbeAddrs(t)
		if _, ok := addrs["notifyInternal"]; ok {
			t.Fatal("адрес notify не объявлен, а ключ notifyInternal в карте есть")
		}
		if missing := unrouted(t, addrs, public); len(missing) != len(public) {
			t.Fatalf("адрес notify не объявлен, а %d из %d публичных биндингов маршрутизируются",
				len(public)-len(missing), len(public))
		}
		if missing, _ := servedOnInternalOrigin(t, addrs, internal); len(missing) != len(internal) {
			t.Fatalf("адрес notify не объявлен, а %d из %d административных биндингов маршрутизируются",
				len(internal)-len(missing), len(internal))
		}
	})
}

// TestNotifyAdminBindingsAreHiddenOnTheExternalListener — InternalNoticeService
// при объявленном notify на ВНЕШНЕМ происхождении отвечает отказом маршрута
// (запрет #6): его биндинги зарегистрированы только на внутреннем mux.
//
// Близнец — те же запросы на внутреннем происхождении маршрутизируются: иначе
// отказ ниже мог бы означать, что маршрутов нет вообще.
func TestNotifyAdminBindingsAreHiddenOnTheExternalListener(t *testing.T) {
	addrs := probeAddrs(t)
	internal := notifyBindings(internalBindingsFromDescriptors())
	if len(internal) != 7 {
		t.Fatalf("административных биндингов InternalNoticeService %d, ожидалось 7", len(internal))
	}
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	for _, b := range internal {
		// Внешнее происхождение — это отсутствие маркера (fail-closed умолчание).
		req := httptest.NewRequest(b.method, b.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if !routingRefusal(rec.Code) {
			t.Errorf("%s %s (%s) на внешнем листенере ответил %d — административный маршрут виден снаружи",
				b.method, b.path, b.fqn, rec.Code)
		}
	}
	if missing, _ := servedOnInternalOrigin(t, addrs, internal); len(missing) > 0 {
		t.Fatalf("близнец: на внутреннем листенере %d биндингов без маршрута (первый — %s %s)",
			len(missing), missing[0].method, missing[0].path)
	}
}
