// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// address_refusal_step_test.go — текст отказа адреса называет следующий шаг
// (приёмка F6b, Р3 с редакции 7; сторона края kaname#526, kacho#3069).
//
// Служба с редакции 5 своей приёмки произносит отказ положения подтверждения
// текстом, который называет шаг, снимающий отказ. Край произносит тот же отказ
// раньше неё своей копией (address_refusal.go) — и обязан произнести его
// побайтово тем же текстом на обеих поверхностях: иначе у одной причины два
// написания, и консоль, показывающая текст, называет шаг на одном входе и
// молчит о нём на другом.
//
// Пара «отказ — близнец» отличается ОДНИМ фактом — путём обращения: та же
// неподтверждённая сессия, тот же носитель; на пути платформы край отказывает
// текстом, на пути, который текст называет, — пропускает к службе. Шаг, который
// отказ называет, но край сам же закрывает, был бы ложной подсказкой.
package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
)

// approvedR3Text — текст отказа Р3 приёмки F6b (редакция 7, kaname#526)
// побайтово; служба держит то же значение своей пробой
// (`internal/admission/admission_526_test.go`, `approvedR3Text`).
const approvedR3Text = "email address is not verified: confirm it with the code from the letter (POST /iam/v1/auth/verify-email/confirm)"

// refusalStep — шаг, названный текстом отказа: метод и путь между «(» и «)».
func refusalStep(t *testing.T, text string) (method, path string) {
	t.Helper()
	open, closing := strings.LastIndex(text, "("), strings.LastIndex(text, ")")
	if open < 0 || closing != len(text)-1 || closing < open {
		t.Fatalf("текст отказа %q не называет шага: выреза «(… )» в конце нет", text)
	}
	method, path, ok := strings.Cut(text[open+1:closing], " ")
	if !ok || method == "" || !strings.HasPrefix(path, "/") {
		t.Fatalf("текст отказа %q: шаг %q — не «МЕТОД /путь»", text, text[open+1:closing])
	}
	return method, path
}

func TestAddressRefusal_526_HTTPTextNamesTheConfirmStepVerbatim(t *testing.T) {
	rig := newAddressRig(t, unverifiedOwnSession())
	rec := rig.present(http.MethodGet, "/iam/v1/projects", true)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("отказ Р3 обязан быть 403, получено %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело отказа не разбирается: %v; тело %s", err, rec.Body.String())
	}
	if body.Message != approvedR3Text {
		t.Fatalf("текст отказа края — не текст Р3 службы:\n  есть  %q\n  ждали %q", body.Message, approvedR3Text)
	}
	want := `{"code":7,"message":"` + approvedR3Text +
		`","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"EMAIL_NOT_VERIFIED","domain":"iam.kaname.cloud"}]}`
	if got := rec.Body.String(); got != want {
		t.Fatalf("тело отказа Р3 не то побайтово:\nполучено %s\nожидалось %s", got, want)
	}
}

func TestAddressRefusal_526_GRPCTextIsTheSameValue(t *testing.T) {
	st := addressRefusalStatus()
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("код отказа на нативной поверхности %s, ожидался PermissionDenied", st.Code())
	}
	if st.Message() != approvedR3Text {
		t.Fatalf("текст отказа края на нативной поверхности — не текст Р3:\n  есть  %q\n  ждали %q", st.Message(), approvedR3Text)
	}
}

// Близнец отказа: та же неподтверждённая сессия тем же носителем на шаге,
// который назван текстом отказа, — ретранслирована полосе формы, а не отвергнута.
func TestAddressRefusal_526_NamedStepIsOpenToTheRefusedSession(t *testing.T) {
	rig := newAddressRig(t, unverifiedOwnSession())
	refused := rig.present(http.MethodGet, "/iam/v1/projects", true)
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(refused.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело отказа не разбирается: %v; тело %s", err, refused.Body.String())
	}
	method, path := refusalStep(t, body.Message)
	if method != http.MethodPost || path != LoginLanePathVerifyEmailConfirm {
		t.Fatalf("отказ называет шаг %s %s, ожидался POST %s — глагол подтверждения кодом", method, path, LoginLanePathVerifyEmailConfirm)
	}
	rec := rig.present(method, path, true)
	if !notAddressRefusal(rec) {
		t.Fatalf("шаг %s %s, названный отказом, край сам отверг: %d %s", method, path, rec.Code, rec.Body.String())
	}
	if rig.reached[path] != 1 || rig.relayed[RelayTargetForm] != 1 {
		t.Fatalf("шаг %s %s не ретранслирован полосе формы: дошёл %d, к форме %d", method, path, rig.reached[path], rig.relayed[RelayTargetForm])
	}
}
