// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta_test

// presented_test.go — предъявленное целиком доезжает до перепроса открытых
// потоков и не выходит ни в одну форму печати (kacho#2900).

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// TestCredentialFromRequest_CarriesWhatTheLaneRecorded — строитель, которым
// проекция снимает удостоверение открытого потока, несёт предъявленное, которое
// записала полоса приёма. Близнец — тот же запрос без записи: предъявленного нет.
func TestCredentialFromRequest_CarriesWhatTheLaneRecorded(t *testing.T) {
	headers := map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000001",
	}

	bare := principalmeta.CredentialFromRequest(req(headers))
	if bare.Presented != (principalmeta.Presented{}) {
		t.Fatal("предъявленное появилось без записи полосы — его подложил не тот, кто проверял")
	}

	r := req(headers)
	r = r.WithContext(principalmeta.WithPresented(r.Context(), principalmeta.PresentedSession("brw-value")))
	c := principalmeta.CredentialFromRequest(r)
	if got := c.Presented.SessionBearer(); got != "brw-value" {
		t.Fatalf("носитель сессии %q — перепрос спросил бы отметку адреса нечем", got)
	}

	r = req(headers)
	r = r.WithContext(principalmeta.WithPresented(r.Context(), principalmeta.PresentedToken("tok-value", true)))
	raw, ours, recorded := principalmeta.CredentialFromRequest(r).Presented.Token()
	if !recorded || raw != "tok-value" || !ours {
		t.Fatalf("токен %q, наш=%v, записан=%v — перепрос задал бы вопрос чужой полосы либо никакой",
			raw, ours, recorded)
	}
}

// TestPresentedNeverPrintsItsValue — значение выходит только через свои
// аксессоры. Ни одна форма печати удостоверения и предъявленного — fmt всеми
// глаголами, журнал текстом и JSON — его не несёт. Положительный контроль:
// аксессоры значение отдают, иначе «не напечатано» верно для пустого значения.
func TestPresentedNeverPrintsItsValue(t *testing.T) {
	const bearer = "brw-secret-value-0123"
	const token = "tok-secret-value-4567"
	session := principalmeta.PresentedSession(bearer)
	tok := principalmeta.PresentedToken(token, true)
	if session.SessionBearer() != bearer {
		t.Fatal("положительный контроль: аксессор носителя не отдал значение")
	}
	if raw, _, _ := tok.Token(); raw != token {
		t.Fatal("положительный контроль: аксессор токена не отдал значение")
	}

	for _, p := range []principalmeta.Presented{session, tok} {
		cred := principalmeta.Credential{UserID: "usr00000000000000001", Presented: p}
		var out []string
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			out = append(out, fmt.Sprintf(verb, p), fmt.Sprintf(verb, cred))
		}
		var text, js bytes.Buffer
		slog.New(slog.NewTextHandler(&text, nil)).Info("x", "presented", p, "cred", cred)
		slog.New(slog.NewJSONHandler(&js, nil)).Info("x", "presented", p, "cred", cred)
		out = append(out, text.String(), js.String())

		for _, o := range out {
			if strings.Contains(o, bearer) || strings.Contains(o, token) {
				t.Fatalf("значение предъявленного вышло в печать: %s", o)
			}
		}
	}
}
