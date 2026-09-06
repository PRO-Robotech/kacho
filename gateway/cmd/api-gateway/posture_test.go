// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// posture_test.go — посадка края судится ЦЕНТРАЛЬНЫМ дескриптором
// (задача продукта #1407).

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

func gatewayPosture(t *testing.T, authnMode string) (servicecontract.Descriptor, error) {
	t.Helper()
	// Домен доверия — величина установки, и без неё дескриптор не принимается:
	// край, не назвавший домена, не признаёт своим ни одного предъявителя
	// сертификата. Здесь он назван затем, чтобы отрицания ниже проверяли СВОЮ
	// ось, а не эту.
	cfg := config.Config{AuthNMode: authnMode, AuthNTrustDomain: "kacho.cloud"}
	return describePosture(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestPosture_AcceptedForEveryDeclaredMode — положительный контроль, и он стоит
// первым: без него все отрицания ниже зеленели бы и на дескрипторе, отвергающем
// вообще всё.
func TestPosture_AcceptedForEveryDeclaredMode(t *testing.T) {
	for _, m := range []string{"dev", "production", "production-strict"} {
		desc, err := gatewayPosture(t, m)
		if err != nil {
			t.Fatalf("посадка %q обязана приниматься, получен отказ: %v", m, err)
		}
		if !desc.Accepted() {
			t.Fatalf("посадка %q: дескриптор не помечен принятым", m)
		}
	}
}

// TestPosture_RefusesAnUnparseableMode — метка вне словаря роняет старт и
// называет ручку.
//
// Прежде здесь была мягкая посадка: неизвестное читалось как боевое. Исход
// fail-closed и потому защитим, но опечатка в ручке не проявлялась ничем —
// процесс работал в режиме, которого никто не выбирал.
func TestPosture_RefusesAnUnparseableMode(t *testing.T) {
	for _, m := range []string{"", "prod", "что-то-новое"} {
		_, err := gatewayPosture(t, m)
		if err == nil {
			t.Fatalf("метка посадки %q принята — режим, который никто не выбрал, "+
				"есть решение о доступе, принятое никем", m)
		}
		if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_AUTHN_MODE") {
			t.Fatalf("отказ обязан назвать ручку, которую править, получено: %v", err)
		}
	}
}

// TestPosture_DBLinkIsWithdrawnWithAReason — ось шифрования до собственной базы
// изъята, и изъятие несёт НЕПУСТУЮ причину. Пустая причина объявлением не
// является: она неотличима от забывчивости.
func TestPosture_DBLinkIsWithdrawnWithAReason(t *testing.T) {
	desc, err := gatewayPosture(t, "production")
	if err != nil {
		t.Fatalf("посадка обязана приниматься: %v", err)
	}
	if _, given := desc.Spec().DBSSLMode.Get(); given {
		t.Fatal("край объявил значение sslmode: своей базы у него нет, и любое значение " +
			"здесь было бы утверждением о соединении, которого он не открывает")
	}
	because, withdrawn := desc.Spec().DBSSLMode.NotApplicableBecause()
	if !withdrawn || strings.TrimSpace(because) == "" {
		t.Fatal("ось не объявлена ни значением, ни изъятием с причиной — молчаливое " +
			"заполнение исходом не является")
	}
}

// TestPosture_ForwarderCircleIsWithdrawnWithAReason — то же для круга
// отправителей переданной личности: край её отправляет, а не принимает.
func TestPosture_ForwarderCircleIsWithdrawnWithAReason(t *testing.T) {
	desc, err := gatewayPosture(t, "production")
	if err != nil {
		t.Fatalf("посадка обязана приниматься: %v", err)
	}
	if _, given := desc.Spec().Forwarders.Get(); given {
		t.Fatal("край объявил круг отправителей значением: звена, читающего переданную " +
			"личность, у него нет, и объявленный круг был бы защитой, которой нет")
	}
	because, withdrawn := desc.Spec().Forwarders.NotApplicableBecause()
	if !withdrawn || strings.TrimSpace(because) == "" {
		t.Fatal("ось не объявлена ни значением, ни изъятием с причиной")
	}
}

// TestPosture_CarriesNoCarrierWiring — контур входящего пути края носитель не
// поднимает, и проводки носителя корень не приносит. Утверждается ИСХОД
// (дескриптор объявил собственный контур), а не намерение автора.
func TestPosture_CarriesNoCarrierWiring(t *testing.T) {
	desc, err := gatewayPosture(t, "production")
	if err != nil {
		t.Fatalf("посадка обязана приниматься: %v", err)
	}
	if desc.OwnContour() == "" {
		t.Fatal("край объявил, что его контур поднимает носитель: у края четыре слушателя " +
			"двух протоколов и ни одной службы своего домена — поднимать носителю нечего")
	}
}
