// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

// decide_test.go — исход строки выбирает одна функция с объявленным порядком
// клеток (замысел З22; приёмка NTF-1 G02, G20, G21, G24, G25, G26; условия к
// коду УК57, УК60, УК67). Наблюдаемое каждой пробы — исход, записанный лентой
// (`Ack`), и то, до чего строка дошла: вызовов ResolveSend, резервов сетки,
// вызовов рендера, SMTP-сессий. У каждого отрицательного кейса — близнец,
// отличающийся одним названным фактом.

import (
	"strings"
	"testing"
	"time"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/grant"
)

// Исходы в форме ленты.
var (
	outSent               = &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_SENT}
	outTemplateSkew       = &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_DEFER, Reason: notifyv1.OutcomeReason_TEMPLATE_SKEW}
	outAttrsInvalid       = &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_INVALID, Reason: notifyv1.OutcomeReason_ATTRS_INVALID}
	outRecipientInvalid   = &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_INVALID, Reason: notifyv1.OutcomeReason_RECIPIENT_INVALID}
	outClassMismatch      = &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_INVALID, Reason: notifyv1.OutcomeReason_CLASS_MISMATCH}
	outClassNotAllowed    = &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_INVALID, Reason: notifyv1.OutcomeReason_CLASS_NOT_ALLOWED}
	outFormNotAllowed     = &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_INVALID, Reason: notifyv1.OutcomeReason_RECIPIENT_FORM_NOT_ALLOWED}
	outRevoked            = &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_DENIED, Reason: notifyv1.OutcomeReason_REVOKED}
	outNetDeferred        = &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_DEFER, Reason: notifyv1.OutcomeReason_RECIPIENT_NET}
	outNetDroppedSecurity = &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_DROPPED, Reason: notifyv1.OutcomeReason_RECIPIENT_NET}
)

// wantOutcome — лента записала ровно этот исход.
func wantOutcome(t *testing.T, got, want *notifyv1.Outcome) {
	t.Helper()
	if got == nil || got.GetKind() != want.GetKind() || got.GetReason() != want.GetReason() {
		t.Fatalf("исход строки %s, ждали %s", outcomeString(got), outcomeString(want))
	}
}

// wantStoppedBefore — строка остановлена до права: ни ResolveSend, ни
// резерва, ни рендера, ни SMTP («что дальше не судится — всё», З22).
func (r *rig) wantStoppedBefore(t *testing.T) {
	t.Helper()
	if n := r.peer.Calls(); n != 0 {
		t.Errorf("вызовов ResolveSend %d, ждали 0", n)
	}
	if n := len(r.limiter.Reserves()); n != 0 {
		t.Errorf("резервов сетки %d, ждали 0", n)
	}
	if n := r.render.Calls(); n != 0 {
		t.Errorf("вызовов рендера %d, ждали 0", n)
	}
	if n := r.sessions(); n != 0 {
		t.Errorf("SMTP-сессий %d, ждали 0", n)
	}
}

// wantSent — письмо ушло: одна сессия, одно письмо, исход SENT.
func (r *rig) wantSent(t *testing.T, row *notifyv1.ClaimedNotification, got *notifyv1.Outcome) {
	t.Helper()
	wantOutcome(t, got, outSent)
	if r.sessions() != 1 || r.letters() != 1 {
		t.Fatalf("SMTP-сессий %d, писем %d — ждали 1 и 1", r.sessions(), r.letters())
	}
	if rc := r.relay.Sessions()[0].Rcpts; len(rc) != 1 || rc[0] != row.GetAddress() {
		t.Fatalf("RCPT %v, ждали [%s]", rc, row.GetAddress())
	}
}

// wantDeferFor — DEFER строки несёт defer_for ручки notify.deferFor (З23).
func (r *rig) wantDeferFor(t *testing.T, module string, row *notifyv1.ClaimedNotification, want time.Duration) {
	t.Helper()
	calls := r.feeds[module].Calls(row.GetId())
	if len(calls) == 0 {
		t.Fatal("Ack по строке не звался")
	}
	if got := calls[len(calls)-1].req.GetDeferFor().AsDuration(); got != want {
		t.Fatalf("defer_for %v, ждали %v", got, want)
	}
}

// wantSkewCounter — notify_template_skew_total{source, direction} = want.
func (r *rig) wantSkewCounter(t *testing.T, module, direction string, want float64) {
	t.Helper()
	got, ok := counterValue(t, r.reg, "notify_template_skew_total", map[string]string{"source": module, "direction": direction})
	if !ok || got != want {
		t.Fatalf("notify_template_skew_total{source=%q,direction=%q} = %v (серия есть: %v), ждали %v", module, direction, got, ok, want)
	}
}

// wantLogNamesNotValue — журнал называет строку и атрибут, значения не несёт
// (G24, G25: «идентификатор строки и имя атрибута, значения нет»).
func (r *rig) wantLogNamesNotValue(t *testing.T, row *notifyv1.ClaimedNotification, attr, value string) {
	t.Helper()
	log := r.log.String()
	if !strings.Contains(log, row.GetId()) {
		t.Errorf("журнал не называет строку %s", row.GetId())
	}
	if attr != "" && !strings.Contains(log, attr) {
		t.Errorf("журнал не называет атрибут %q", attr)
	}
	// Значение проверяется по каждому своему куску без управляющих знаков:
	// журнал JSON экранирует CR и LF, и сырое значение в нём не встречается.
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r < 0x20 }) {
		if len(part) >= 4 && strings.Contains(log, part) {
			t.Errorf("журнал несёт значение атрибута %q (кусок %q)", value, part)
		}
	}
}

// ── NTF1-G02 ─────────────────────────────────────────────────────────────────

// TestDeliver_NTF1G02_ForeignNamespaceTemplateIsTemplateSkew — служба А не
// отправит шаблон службы Б: шаблон ищется в пространстве ленты.
func TestDeliver_NTF1G02_ForeignNamespaceTemplateIsTemplateSkew(t *testing.T) {
	row := func() *notifyv1.ClaimedNotification {
		return claimed(rowSpec{template: "probe-bhello", rev: 1, attrs: map[string]string{"target": "/"}})
	}
	t.Run("шаблон только в probe-b", func(t *testing.T) {
		r := newRig(t, rigOpts{build: buildOf(t, buildEntry{nsProbeB, "probe-bhello", 1})})
		rw := row()
		wantOutcome(t, r.one(nsProbe, rw), outTemplateSkew)
		r.wantDeferFor(t, nsProbe, rw, probeDeferFor)
		r.wantStoppedBefore(t)
		r.wantSkewCounter(t, nsProbe, "missing", 1)
	})
	t.Run("близнец: шаблон есть и в probe", func(t *testing.T) {
		r := newRig(t, rigOpts{build: buildOf(t, buildEntry{nsProbeB, "probe-bhello", 1}, buildEntry{nsProbe, "probe-bhello", 1})})
		rw := row()
		r.wantSent(t, rw, r.one(nsProbe, rw))
	})
}

// ── NTF1-G20 ─────────────────────────────────────────────────────────────────

// TestDeliver_NTF1G20_SecurityOnlyForIdentityNamespaces — класс сборки
// security вне identityNamespaces — INVALID(class_not_allowed) до права.
func TestDeliver_NTF1G20_SecurityOnlyForIdentityNamespaces(t *testing.T) {
	build := func(t *testing.T) *fixtureBuild {
		return buildOf(t, buildEntry{nsProbe, "probe-sec", 1}, buildEntry{nsKaname, "probe-sec", 1})
	}
	sources := []config.Source{sourceOf(nsProbe, config.RecipientAddress), sourceOf(nsKaname, config.RecipientAddress)}
	row := func() *notifyv1.ClaimedNotification {
		return claimed(rowSpec{template: "probe-sec", rev: 1, class: notifyv1.NotificationClass_SECURITY, attrs: map[string]string{"target": "/"}})
	}
	t.Run("security в probe", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t), sources: sources})
		wantOutcome(t, r.one(nsProbe, row()), outClassNotAllowed)
		r.wantStoppedBefore(t)
	})
	t.Run("близнец: security в kaname", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t), sources: sources})
		rw := row()
		r.wantSent(t, rw, r.one(nsKaname, rw))
		if res := r.limiter.Reserves(); len(res) != 1 || res[0].Class != feed.ClassSecurity {
			t.Fatalf("резервы %+v, ждали один класса security", res)
		}
	})
}

// ── NTF1-G21 ─────────────────────────────────────────────────────────────────

// TestDeliver_NTF1G21_AddressOnlyForAllowedNamespaces — форма адресата вне
// записи перечня — INVALID(recipient_form_not_allowed) до права.
func TestDeliver_NTF1G21_AddressOnlyForAllowedNamespaces(t *testing.T) {
	build := func(t *testing.T) *fixtureBuild {
		return buildOf(t, buildEntry{nsProbe, "probe-bhello", 1}, buildEntry{nsKaname, "probe-bhello", 1})
	}
	row := func() *notifyv1.ClaimedNotification {
		return claimed(rowSpec{template: "probe-bhello", rev: 1, attrs: map[string]string{"target": "/"}})
	}
	t.Run("probe без формы address", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t), sources: []config.Source{sourceOf(nsProbe), sourceOf(nsKaname, config.RecipientAddress)}})
		wantOutcome(t, r.one(nsProbe, row()), outFormNotAllowed)
		r.wantStoppedBefore(t)
	})
	t.Run("близнец: та же запись probe с формой address", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t), sources: []config.Source{sourceOf(nsProbe, config.RecipientAddress)}})
		rw := row()
		r.wantSent(t, rw, r.one(nsProbe, rw))
	})
	t.Run("близнец приёмки: та же строка из kaname", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t), sources: []config.Source{sourceOf(nsProbe), sourceOf(nsKaname, config.RecipientAddress)}})
		rw := row()
		r.wantSent(t, rw, r.one(nsKaname, rw))
	})
}

// ── NTF1-G24 ─────────────────────────────────────────────────────────────────

// linkTwin — значения близнеца G24.
var linkTwin = map[string]string{
	"target":       "/iam/invitations/inv-0123456789abcdefg",
	"token":        "0123456789abcdefghijklmnopqrstuv",
	"subject_name": "probe",
}

// TestDeliver_NTF1G24_PathTokenAddressOutOfForm — path, token, текст темы и
// адрес вне формы — INVALID до права; журнал без значения.
func TestDeliver_NTF1G24_PathTokenAddressOutOfForm(t *testing.T) {
	build := func(t *testing.T) *fixtureBuild { return buildOf(t, buildEntry{nsProbe, "probe-link", 1}) }
	cases := []struct {
		name, attr, value string
	}{
		{"а: path //узел", "target", "//other.example.invalid/x"},
		{"б: path https://узел", "target", "https://other.example.invalid/x"},
		{"в: path /\\узел", "target", `/\other.example.invalid`},
		{"г: path с ..", "target", "/a/../b"},
		{"д: path с пробелом", "target", "/a b"},
		{"и: path с хвостовой /", "target", "/a/"},
		{"к: path с пустым сегментом", "target", "/a//b"},
		{"е: token вне алфавита", "token", "abcdefghijklmn&next=x"},
		{"ж: token короче границы", "token", "abcdefghijklmno"},
		{"л: CRLF в тексте темы", "subject_name", "a\r\nBcc: x@example.invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, rigOpts{build: build(t)})
			rw := claimed(rowSpec{template: "probe-link", rev: 1, attrs: withAttrs(linkTwin, map[string]string{c.attr: c.value})})
			wantOutcome(t, r.one(nsProbe, rw), outAttrsInvalid)
			r.wantStoppedBefore(t)
			r.wantLogNamesNotValue(t, rw, c.attr, c.value)
		})
	}
	t.Run("з: адрес не разбирается", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t)})
		rw := claimed(rowSpec{template: "probe-link", rev: 1, to: "n3-bad@", attrs: linkTwin})
		wantOutcome(t, r.one(nsProbe, rw), outRecipientInvalid)
		r.wantStoppedBefore(t)
		r.wantLogNamesNotValue(t, rw, "", "n3-bad@")
	})
	t.Run("близнец: значения по форме", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t)})
		rw := claimed(rowSpec{template: "probe-link", rev: 1, attrs: linkTwin})
		r.wantSent(t, rw, r.one(nsProbe, rw))
	})
	t.Run("близнец границы: path /", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t)})
		rw := claimed(rowSpec{template: "probe-link", rev: 1, attrs: withAttrs(linkTwin, map[string]string{"target": "/"})})
		r.wantSent(t, rw, r.one(nsProbe, rw))
	})
}

// ── NTF1-G25 ─────────────────────────────────────────────────────────────────

// allTwin — значения близнеца B29.
var allTwin = map[string]string{
	"subject_name": "probe",
	"note":         "n",
	"code":         "123456",
	"target":       "/",
	"token":        "0123456789abcdefghijklmnopqrstuv",
	"issued_at":    "2026-09-30T00:00:00Z",
}

// TestDeliver_NTF1G25_BuildRevisionValuesAreJudgedForeignAreNot — строка
// ревизии сборки с нулём, неразбираемым, отсутствующим или лишним атрибутом —
// INVALID; строка иной ревизии — template_skew, значения не судятся.
func TestDeliver_NTF1G25_BuildRevisionValuesAreJudgedForeignAreNot(t *testing.T) {
	rev2 := func(t *testing.T) *fixtureBuild { return buildOf(t, buildEntry{nsProbe, "probe-all", 2}) }
	invalid := []struct {
		name, attr, value string
	}{
		{"а: subject_name пуст", "subject_name", ""},
		{"б: note пуст", "note", ""},
		{"в: code пуст", "code", ""},
		{"г: issued_at нулевая отметка", "issued_at", "0001-01-01T00:00:00Z"},
		{"д: note нет", "note", delAttr},
		{"е: лишний extra", "extra", "x"},
		{"ж: issued_at не RFC 3339", "issued_at", "2026-09-30 00:00:00"},
		{"з: issued_at вторым написанием", "issued_at", "2026-09-30T03:00:00+03:00"},
	}
	for _, c := range invalid {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, rigOpts{build: rev2(t)})
			rw := claimed(rowSpec{template: "probe-all", rev: 2, attrs: withAttrs(allTwin, map[string]string{c.attr: c.value})})
			wantOutcome(t, r.one(nsProbe, rw), outAttrsInvalid)
			r.wantStoppedBefore(t)
			value := c.value
			if value == delAttr || value == "" {
				value = ""
			}
			r.wantLogNamesNotValue(t, rw, c.attr, value)
		})
	}
	skew := []struct {
		name      string
		rev       uint32
		edits     map[string]string
		direction string
	}{
		{"ревизия 1 с полным набором", 1, nil, "older"},
		{"ревизия 1 без note", 1, map[string]string{"note": delAttr}, "older"},
		{"ревизия 3 новее сборки", 3, nil, "newer"},
		{"ревизия, нуль: ревизия 1 без note и subject_name пуст", 1, map[string]string{"note": delAttr, "subject_name": ""}, "older"},
	}
	for _, c := range skew {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, rigOpts{build: rev2(t)})
			rw := claimed(rowSpec{template: "probe-all", rev: c.rev, attrs: withAttrs(allTwin, c.edits)})
			wantOutcome(t, r.one(nsProbe, rw), outTemplateSkew)
			r.wantDeferFor(t, nsProbe, rw, probeDeferFor)
			r.wantStoppedBefore(t)
			r.wantSkewCounter(t, nsProbe, c.direction, 1)
		})
	}
	t.Run("близнец: ревизия 2 по форме", func(t *testing.T) {
		r := newRig(t, rigOpts{build: rev2(t)})
		rw := claimed(rowSpec{template: "probe-all", rev: 2, attrs: allTwin})
		r.wantSent(t, rw, r.one(nsProbe, rw))
	})
	t.Run("близнец (ревизия, нуль): та же строка ревизии 2", func(t *testing.T) {
		r := newRig(t, rigOpts{build: rev2(t)})
		rw := claimed(rowSpec{template: "probe-all", rev: 2, attrs: withAttrs(allTwin, map[string]string{"note": delAttr, "subject_name": ""})})
		wantOutcome(t, r.one(nsProbe, rw), outAttrsInvalid)
		r.wantStoppedBefore(t)
	})
	t.Run("цена: сборка ревизии 1, сетка исчерпана — recipient_net", func(t *testing.T) {
		r := newRig(t, rigOpts{build: buildOf(t, buildEntry{nsProbe, "probe-all-r1", 1}), exhausted: true})
		rw := claimed(rowSpec{template: "probe-all", rev: 1, attrs: withAttrs(allTwin, map[string]string{"note": delAttr})})
		wantOutcome(t, r.one(nsProbe, rw), outNetDeferred)
		if r.sessions() != 0 {
			t.Fatalf("SMTP-сессий %d, ждали 0", r.sessions())
		}
	})
	t.Run("цена: после перезапуска на сборке ревизии 2 — template_skew, резервов 0", func(t *testing.T) {
		r := newRig(t, rigOpts{build: rev2(t)})
		rw := claimed(rowSpec{template: "probe-all", rev: 1, attrs: withAttrs(allTwin, map[string]string{"note": delAttr})})
		wantOutcome(t, r.one(nsProbe, rw), outTemplateSkew)
		r.wantStoppedBefore(t)
	})
	t.Run("близнец цены: сборка ревизии 1, сетка свободна — отправлено по описанию ревизии 1", func(t *testing.T) {
		r := newRig(t, rigOpts{build: buildOf(t, buildEntry{nsProbe, "probe-all-r1", 1})})
		rw := claimed(rowSpec{template: "probe-all", rev: 1, attrs: withAttrs(allTwin, map[string]string{"note": delAttr})})
		r.wantSent(t, rw, r.one(nsProbe, rw))
	})
}

// ── NTF1-G26 ─────────────────────────────────────────────────────────────────

// TestDeliver_NTF1G26_OptionalAbsenceAndClassFromBuild — отсутствие optional
// — одним написанием (ключа нет); класс исхода — из сборки; колонка class,
// не равная классу сборки, — class_mismatch до права и резерва (УК60).
func TestDeliver_NTF1G26_OptionalAbsenceAndClassFromBuild(t *testing.T) {
	build := func(t *testing.T) *fixtureBuild { return buildOf(t, buildEntry{nsProbe, "probe-opt", 1}) }
	base := map[string]string{"subject_name": "probe"}
	t.Run("а: ключей inviter и target нет — отправлено", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t)})
		rw := claimed(rowSpec{template: "probe-opt", rev: 1, attrs: base})
		r.wantSent(t, rw, r.one(nsProbe, rw))
	})
	for _, c := range []struct{ name, attr string }{{"б: inviter пуст", "inviter"}, {"в: target пуст", "target"}} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, rigOpts{build: build(t)})
			rw := claimed(rowSpec{template: "probe-opt", rev: 1, attrs: withAttrs(base, map[string]string{c.attr: ""})})
			wantOutcome(t, r.one(nsProbe, rw), outAttrsInvalid)
			r.wantStoppedBefore(t)
			r.wantLogNamesNotValue(t, rw, c.attr, "")
		})
	}
	t.Run("г: колонка class security при классе сборки notice", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t)})
		rw := claimed(rowSpec{template: "probe-opt", rev: 1, class: notifyv1.NotificationClass_SECURITY, attrs: base})
		wantOutcome(t, r.one(nsProbe, rw), outClassMismatch)
		r.wantStoppedBefore(t)
	})
	kanameSources := []config.Source{sourceOf(nsKaname, config.RecipientAddress)}
	t.Run("д: kaname, строка ревизии 1 notice, сборка ревизии 2 security", func(t *testing.T) {
		r := newRig(t, rigOpts{build: buildOf(t, buildEntry{nsKaname, "probe-opt-security", 2}), sources: kanameSources})
		rw := claimed(rowSpec{template: "probe-opt", rev: 1, attrs: base})
		wantOutcome(t, r.one(nsKaname, rw), outTemplateSkew)
		r.wantStoppedBefore(t)
		r.wantSkewCounter(t, nsKaname, "older", 1)
	})
	t.Run("близнец (д): сборка ревизии 1 notice", func(t *testing.T) {
		r := newRig(t, rigOpts{build: buildOf(t, buildEntry{nsKaname, "probe-opt", 1}), sources: kanameSources})
		rw := claimed(rowSpec{template: "probe-opt", rev: 1, attrs: base})
		r.wantSent(t, rw, r.one(nsKaname, rw))
	})
	t.Run("близнец: inviter, target и колонка notice — отправлено, резерв по классу сборки", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t)})
		rw := claimed(rowSpec{template: "probe-opt", rev: 1, attrs: withAttrs(base, map[string]string{"inviter": "i", "target": "/a"})})
		r.wantSent(t, rw, r.one(nsProbe, rw))
		if res := r.limiter.Reserves(); len(res) != 1 || res[0].Class != feed.ClassNotice || res[0].Source != nsProbe {
			t.Fatalf("резервы %+v, ждали один класса notice источника probe", res)
		}
	})
}

// ── УК57, УК67: template_skew — первым ───────────────────────────────────────

// TestDecide_UK57_ForeignRevisionWinsOverLaterCells — чужая ревизия вместе с
// отзывом, адресом вне формы или исчерпанной сеткой — DEFER(template_skew);
// вызовов ResolveSend и резервов 0 (CX1-47 (в)). Близнец каждой пары — та же
// строка ревизии сборки: исход клетки, которую template_skew перебил.
func TestDecide_UK57_ForeignRevisionWinsOverLaterCells(t *testing.T) {
	build := func(t *testing.T) *fixtureBuild { return buildOf(t, buildEntry{nsProbe, "probe-bhello", 2}) }
	attrs := map[string]string{"target": "/"}
	pairs := []struct {
		name      string
		opts      func(o *rigOpts)
		to        string
		ownResult *notifyv1.Outcome
	}{
		{"REVOKED", func(o *rigOpts) { o.decision = grant.DecisionRevoked }, "", outRevoked},
		{"адрес вне формы", func(*rigOpts) {}, "n3-bad@", outRecipientInvalid},
		{"сетка исчерпана", func(o *rigOpts) { o.exhausted = true }, "", outNetDeferred},
	}
	for _, p := range pairs {
		t.Run(p.name+": чужая ревизия", func(t *testing.T) {
			o := rigOpts{build: build(t)}
			p.opts(&o)
			r := newRig(t, o)
			rw := claimed(rowSpec{template: "probe-bhello", rev: 1, to: p.to, attrs: attrs})
			wantOutcome(t, r.one(nsProbe, rw), outTemplateSkew)
			r.wantStoppedBefore(t)
		})
		t.Run(p.name+": близнец — ревизия сборки", func(t *testing.T) {
			o := rigOpts{build: build(t)}
			p.opts(&o)
			r := newRig(t, o)
			rw := claimed(rowSpec{template: "probe-bhello", rev: 2, to: p.to, attrs: attrs})
			wantOutcome(t, r.one(nsProbe, rw), p.ownResult)
			if r.sessions() != 0 {
				t.Fatalf("SMTP-сессий %d, ждали 0", r.sessions())
			}
		})
	}
}

// TestDecide_UK67_ForeignRevisionSecurityColumnExhaustedNet — чужая ревизия,
// колонка security, исчерпанная сетка → template_skew, резервов 0 (CX1-52).
// Близнец — та же строка ревизии сборки: DROPPED(recipient_net) у security.
func TestDecide_UK67_ForeignRevisionSecurityColumnExhaustedNet(t *testing.T) {
	sources := []config.Source{sourceOf(nsKaname, config.RecipientAddress)}
	build := func(t *testing.T) *fixtureBuild { return buildOf(t, buildEntry{nsKaname, "probe-sec", 2}) }
	row := func(rev uint32) *notifyv1.ClaimedNotification {
		return claimed(rowSpec{template: "probe-sec", rev: rev, class: notifyv1.NotificationClass_SECURITY, attrs: map[string]string{"target": "/"}})
	}
	t.Run("чужая ревизия", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t), sources: sources, exhausted: true})
		wantOutcome(t, r.one(nsKaname, row(1)), outTemplateSkew)
		r.wantStoppedBefore(t)
	})
	t.Run("близнец: ревизия сборки", func(t *testing.T) {
		r := newRig(t, rigOpts{build: build(t), sources: sources, exhausted: true})
		wantOutcome(t, r.one(nsKaname, row(2)), outNetDroppedSecurity)
		if len(r.limiter.Reserves()) != 1 || r.sessions() != 0 {
			t.Fatalf("резервов %d, SMTP-сессий %d — ждали 1 и 0", len(r.limiter.Reserves()), r.sessions())
		}
	})
}
