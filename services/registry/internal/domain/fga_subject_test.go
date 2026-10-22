// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

// fga_subject_test.go — единый источник subject-encoding: FGASubjectFromID
// (data-plane, id-prefix) обязан совпадать с FGASubjectFromPrincipal
// (control-plane, Principal.Type) для тех же principal'ов, иначе owner-tuple,
// записанный control-plane, не матчится Check'ом data-plane (deny владельцу).

import "testing"

// Законные субъекты — порождённой продуктом формы (kaname pkg/generatedid): префикс
// вида и 17 знаков crockford-base32. Форму здесь не пересказывает ни одно выражение:
// её судит публичный предикат kaname, а значения ниже — его законные близнецы.
const (
	formUser       = "usr0123456789abcdefg"
	formSA         = "sva0123456789abcdefg"
	formOtherKind  = "grp0123456789abcdefg"
	formAccessKey  = "ak-0123456789abcdefg"
	formOtherKind2 = "cag_0123456789abcdefg"
	// Порождённая форма, но префикс субъектного вида ей не вид: `sva_` + тело —
	// не то, чем служба доступа чеканит учётку; `sv` + тело на `a…` — вид `sv`.
	formSASepNotMinted = "sva_0123456789abcdefg"
	formSVKind         = "sva0123456789abcdef"
)

// TestFGASubjectFromID — kacho#3305: тип субъекта выводится только из идентификатора
// порождённой формы известного вида; всё прочее — "" (субъекта нет, вызывающий
// обязан отказать), а не субъект какого-либо класса. Отрицательные кейсы меняют
// ровно один факт против законного близнеца (форма либо вид).
func TestFGASubjectFromID(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want string
	}{
		{"user-of-the-generated-form", formUser, "user:" + formUser},
		{"service-account-of-the-generated-form", formSA, "service_account:" + formSA},
		{"empty", "", ""},
		// Форма порождённого, но вид не субъектный — не субъект ни одного класса.
		{"generated-form-group-kind-refused", formOtherKind, ""},
		{"generated-form-access-key-kind-refused", formAccessKey, ""},
		{"generated-form-grant-kind-refused", formOtherKind2, ""},
		// Известный префикс, но не порождённая форма — отказ (одна правка против близнеца).
		{"user-prefix-unknown-form-refused", "usr_alice", ""},
		{"user-prefix-legacy-dash-refused", "usr-legacy", ""},
		{"service-account-prefix-unknown-form-refused", "sva-ci", ""},
		{"service-account-prefix-with-separator-not-minted-refused", formSASepNotMinted, ""},
		{"two-letter-kind-sharing-the-prefix-refused", formSVKind, ""},
		{"service-account-uppercase-refused", "SVA0123456789ABCDEFG", ""},
		{"service-account-trailing-space-refused", formSA + " ", ""},
		{"model-subject-spelling-refused", "service_account:" + formSA, ""},
		// Прежний дефект: неизвестный префикс считался service_account.
		{"unknown-prefix-refused-not-service-account", "grp_admins", ""},
		{"breakglass-word-refused", "bootstrap", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FGASubjectFromID(c.id); got != c.want {
				t.Fatalf("FGASubjectFromID(%q) = %q, want %q", c.id, got, c.want)
			}
		})
	}
}

// TestFGASubjectForPrincipalID_Anon — RG-1 D-7: плоскость данных сводит настроенный
// анонимный идентификатор (kaname AnonymousClientID) к системному классу
// FGASubjectAnonymousClass («system_class:anonymous», приёмка kaname SC-39; подстановка
// говорящим не бывает); любой другой идентификатор судит FGASubjectFromID — по форме
// и виду. Пустой anonPrincipalID → анонимный поток выключен: анонимный идентификатор
// судится как любой другой и, не будучи порождённой формы, субъекта не даёт ("") —
// secure-by-default. Это единственная точка, где ДЕЙСТВУЮЩИЙ анонимный Bearer
// становится публичным принципалом только для чтения (B03/B14).
func TestFGASubjectForPrincipalID_Anon(t *testing.T) {
	const anonID = "cid-registry-anon"
	cases := []struct {
		name   string
		id     string
		anonID string
		want   string
	}{
		// Configured anon id → класс anonymous (B03 anon pull, B14 anon no-write).
		{"anon-id-configured-maps-to-the-anonymous-class", anonID, anonID, FGASubjectAnonymousClass},
		{"anonymous-class-spelling", anonID, anonID, "system_class:anonymous"},
		// Настоящая учётка не задета и при включённом анониме (судится по форме и виду).
		{"sa-unaffected-when-anon-enabled", formSA, anonID, "service_account:" + formSA},
		{"user-unaffected-when-anon-enabled", formUser, anonID, "user:" + formUser},
		// Аноним ВЫКЛЮЧЕН (пустой anonID): анонимный идентификатор молча классом не
		// становится и, не будучи порождённой формы, принципалом тоже: "" (kacho#3305,
		// отказ у вызывающего), а не service_account:<анонимный идентификатор>.
		{"anon-disabled-empty-anonid-no-wildcard", anonID, "", ""},
		{"empty-id-empty-anonid", "", "", ""},
		// Аноним включён, но пришёл ДРУГОЙ sub → это не анонимный принципал.
		{"different-sub-not-anon", formUser, anonID, "user:" + formUser},
		{"different-sub-unknown-form-refused", "sva_other", anonID, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FGASubjectForPrincipalID(c.id, c.anonID); got != c.want {
				t.Fatalf("FGASubjectForPrincipalID(%q, %q) = %q, want %q", c.id, c.anonID, got, c.want)
			}
		})
	}
}

// TestFGASubject_TwoEncodersAgree — оба encoder'а (id-prefix vs Principal.Type)
// дают идентичную subject-строку для согласованных id/type пар (инвариант против
// рассинхронизации planes, ради которой vocabulary централизован в domain).
func TestFGASubject_TwoEncodersAgree(t *testing.T) {
	pairs := []struct {
		pType string
		id    string
	}{
		{"user", formUser},
		{"service_account", formSA},
	}
	for _, p := range pairs {
		fromID := FGASubjectFromID(p.id)
		fromPrincipal := FGASubjectFromPrincipal(p.pType, p.id)
		if fromID != fromPrincipal {
			t.Fatalf("encoders disagree for (%q,%q): fromID=%q fromPrincipal=%q",
				p.pType, p.id, fromID, fromPrincipal)
		}
	}
}
