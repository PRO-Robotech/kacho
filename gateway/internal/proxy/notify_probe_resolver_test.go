// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package proxy_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// TestResolver_NotifyProbeSendIsNotRoutedOutside — глагол стендовой пробы
// notify-probe (решение владельца 2026-10-08 (1)) зовётся только через
// внутренний REST края; gRPC-резолвер внешнего края его не маршрутизирует даже
// при живом соединении домена notify (запрет #6).
//
// Близнец — публичный NoticeService/List того же домена на том же наборе
// соединений: он маршрутизируется, так что отказ пробы не следствие «у
// резолвера нет notify».
func TestResolver_NotifyProbeSendIsNotRoutedOutside(t *testing.T) {
	resolve := proxy.Resolver(makeTestBackends(t, []string{"notify"}))

	const twin = "/kacho.cloud.notify.v1.NoticeService/List"
	if _, conn, ok := resolve(twin); !ok || conn == nil {
		t.Fatalf("близнец %q не маршрутизирован при соединении notify — проба ниже ничего не доказала бы", twin)
	}

	const probe = "/kacho.cloud.notify.v1.InternalNotifyProbeService/Send"
	if !proxy.IsInternalRoute(probe) {
		t.Fatalf("%q обязан опознаваться как Internal*-путь", probe)
	}
	if _, _, ok := resolve(probe); ok {
		t.Errorf("Internal-глагол пробы %q маршрутизирован внешним резолвером (запрет #6)", probe)
	}
}
