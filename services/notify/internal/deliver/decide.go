// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/notify/form"
	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
)

// identityNamespaces — пространства, которым разрешён класс `security`
// (Р6, константа сборки notify; клетка 3).
var identityNamespaces = []string{"kaname"}

// Resolved — описание строки после клеток 1–6: шаблон сборки, проверенный
// набор атрибутов и нормализованный адресат. Колонки `class` строки в нём нет
// (CX1-52 (а)): клетки 3–10, сетка и рендер берут класс из Template.Class.
type Resolved struct {
	// Namespace — пространство ленты (модуль источника).
	Namespace string
	// ID — идентификатор строки ленты (`ntf-…`).
	ID string
	// Template — шаблон сборки той же ревизии, что строка.
	Template *spec.Template
	// Attrs — заданные атрибуты, проверенные notify/form. Optional без ключа в
	// строке здесь отсутствует: блок с `when` судится по этому набору (CX1-51 (б)).
	Attrs map[string]form.Value
	// Recipient — адресат, разобранный address.Normalize.
	Recipient address.Normalized
}

// stepKind — что делать со строкой после клетки.
type stepKind uint8

const (
	// stepNext — клетка строку пропустила: судит следующая.
	stepNext stepKind = iota
	// stepAck — исход выбран: записать его `Ack`.
	stepAck
	// stepSilent — строку не начинать и `Ack` не слать (клетка 8, ошибка базы
	// резерва): исхода Р11 для «не начал» нет, строку выдаст следующий `Claim`.
	stepSilent
)

// Step — решение клетки. Отсрочка неотделима от исхода DEFER: её несёт тот же
// Step.
type Step struct {
	kind     stepKind
	outcome  feed.Outcome
	deferFor time.Duration
}

func next() Step   { return Step{kind: stepNext} }
func silent() Step { return Step{kind: stepSilent} }

func outcome(kind feed.Kind, reason feed.Reason) Step {
	return Step{kind: stepAck, outcome: feed.Outcome{Kind: kind, Reason: reason}}
}

func deferred(reason feed.Reason, d time.Duration) Step {
	return Step{kind: stepAck, outcome: feed.Outcome{Kind: feed.KindDefer, Reason: reason}, deferFor: d}
}

func invalid(reason feed.Reason) Step { return outcome(feed.KindInvalid, reason) }

// job — запись обработки одной строки у исполнителя.
type job struct {
	rt  route
	row *notifyv1.ClaimedNotification
	// leaseEnd, expiresAt — конец аренды и срока строки для notify:
	// `t_send + lease_remaining` и `t_send + expires_in` (З21). Моменты несут
	// монотонное показание t_send.
	leaseEnd  time.Time
	expiresAt time.Time
	res       Resolved
	// reservation — резерв сетки строки; освобождается однажды на исходе,
	// отличном от SENT (З24).
	reservation *limits.Reservation
}

// cell — клетка порядка исхода строки.
type cell struct {
	name  string
	judge func(w *Worker, ctx context.Context, j *job) Step
}

// cells — ПОРЯДОК клеток исхода строки (З22, CX1-47 (а)): единственное его
// объявление. `template_skew` — первым (CX1-47 (в)); клетки 2–6 — до права.
var cells = [...]cell{
	{"template", (*Worker).cellTemplate},              // 1 DEFER(template_skew)
	{"class", (*Worker).cellClass},                    // 2 INVALID(class_mismatch)
	{"class_namespace", (*Worker).cellClassNamespace}, // 3 INVALID(class_not_allowed)
	{"recipient_form", (*Worker).cellRecipientForm},   // 4 INVALID(recipient_form_not_allowed)
	{"recipient", (*Worker).cellRecipient},            // 5 INVALID(recipient_invalid)
	{"attrs", (*Worker).cellAttrs},                    // 6 INVALID(attrs_invalid)
	{"grant", (*Worker).cellGrant},                    // 7 DENIED(revoked) · DEFER(grant_skew | platform_unavailable)
	{"lease_budget", (*Worker).cellLeaseBudget},       // 8 без Ack
	{"recipient_net", (*Worker).cellRecipientNet},     // 9 DROPPED | DEFER(recipient_net)
	{"send", (*Worker).cellSend},                      // 10 клетки Р11 ретранслятора
}

// decide — исход строки: клетки по порядку [cells] до первой, выбравшей исход.
// ctx несёт крайний момент строки.
func (w *Worker) decide(ctx context.Context, j *job) Step {
	for _, c := range cells {
		st := c.judge(w, ctx, j)
		if st.kind != stepNext {
			return st
		}
	}
	// Последняя клетка всегда выбирает исход; дойти сюда — дефект порядка.
	w.log.Error("порядок клеток не выбрал исход строки", "source", j.rt.src.Module, "id", j.row.GetId())
	return silent()
}

// handle — обработка одной строки: крайний момент, исход, Ack, освобождение.
func (w *Worker) handle(base context.Context, rt route, sentAt time.Time, row *notifyv1.ClaimedNotification) {
	j := &job{
		rt:        rt,
		row:       row,
		leaseEnd:  sentAt.Add(row.GetLeaseRemaining().AsDuration()),
		expiresAt: sentAt.Add(row.GetExpiresIn().AsDuration()),
	}
	// Крайний момент строки (З21): min(t_send + lease_remaining − AckMargin,
	// t_start + resolveSendTimeout + smtpSessionTimeout) — длительностью от
	// внедряемых часов, t_start — сейчас.
	rowCtx, cancel := context.WithTimeout(base, min(w.clock.Until(j.leaseEnd)-config.AckMargin, w.resolve+w.session))
	defer cancel()

	st := w.decide(rowCtx, j)
	if st.kind == stepAck {
		w.ack(rowCtx, j, st)
	}
	if j.reservation != nil && (st.kind != stepAck || st.outcome.Kind != feed.KindSent) {
		// Освобождение берёт собственный контекст (limits.Release, SDR-Н1);
		// неудачу оно записывает в журнал само — вклад остаётся до конца окна.
		_ = w.limiter.Release(rowCtx, j.reservation)
	}
}

// ── клетки 1–6: описание строки ──────────────────────────────────────────────

// Направления расхождения ревизии (метка direction, CX1-48).
const (
	dirMissing = "missing"
	dirOlder   = "older"
	dirNewer   = "newer"
)

func directions() []string { return []string{dirMissing, dirOlder, dirNewer} }

// cellTemplate — клетка 1: шаблон `<пространство ленты>/<имя>` в сборке той же
// ревизии. Направление — из того же сравнения, что выбрало клетку (CX1-48).
func (w *Worker) cellTemplate(_ context.Context, j *job) Step {
	module := j.rt.src.Module
	tpl, ok := w.build.Template(module, j.row.GetTemplate())
	if !ok || !tpl.HasRevision {
		return w.skew(j, dirMissing)
	}
	rowRev, buildRev := int64(j.row.GetSchemaRev()), int64(tpl.Revision.Number)
	switch {
	case rowRev < buildRev:
		return w.skew(j, dirOlder)
	case rowRev > buildRev:
		return w.skew(j, dirNewer)
	}
	j.res = Resolved{Namespace: module, ID: j.row.GetId(), Template: tpl}
	return next()
}

func (w *Worker) skew(j *job, direction string) Step {
	w.m.templateSkew.WithLabelValues(j.rt.src.Module, direction).Inc()
	w.log.Info("строка чужой ревизии шаблона: template_skew",
		"source", j.rt.src.Module, "id", j.row.GetId(), "template", j.row.GetTemplate(), "direction", direction)
	return deferred(feed.ReasonTemplateSkew, w.deferFor)
}

// cellClass — клетка 2: колонка `class` строки равна классу сборки. Это
// ЕДИНСТВЕННОЕ чтение колонки в пакете (гейт TestRowClassIsReadByOneLine).
func (w *Worker) cellClass(_ context.Context, j *job) Step {
	want, ok := wireClassOf(j.res.Template.Class)
	if !ok || j.row.GetClass() != want {
		w.log.Warn("колонка class строки не равна классу сборки: class_mismatch",
			"source", j.rt.src.Module, "id", j.row.GetId(), "template", j.res.Template.Name)
		return invalid(feed.ReasonClassMismatch)
	}
	return next()
}

// cellClassNamespace — клетка 3: класс сборки `security` — только у
// identityNamespaces (NTF1-G20).
func (w *Worker) cellClassNamespace(_ context.Context, j *job) Step {
	if j.res.Template.Class == spec.ClassSecurity && !slices.Contains(identityNamespaces, j.rt.src.Module) {
		w.log.Warn("класс security вне identityNamespaces: class_not_allowed",
			"source", j.rt.src.Module, "id", j.row.GetId(), "template", j.res.Template.Name)
		return invalid(feed.ReasonClassNotAllowed)
	}
	return next()
}

// cellRecipientForm — клетка 4: форма адресата строки разрешена записи
// перечня (NTF1-G21). Строка без адресата формы не имеет — не разрешена.
func (w *Worker) cellRecipientForm(_ context.Context, j *job) Step {
	f, ok := recipientFormOf(j.row)
	if !ok || !slices.Contains(j.rt.src.RecipientForms, f) {
		w.log.Warn("форма адресата не разрешена записи перечня: recipient_form_not_allowed",
			"source", j.rt.src.Module, "id", j.row.GetId(), "form", string(f))
		return invalid(feed.ReasonRecipientFormNotAllowed)
	}
	return next()
}

// recipientFormOf — форма адресата по варианту строки. Набор вариантов
// контракта закрыт; вариант, которого notify не знает, — «формы нет».
func recipientFormOf(row *notifyv1.ClaimedNotification) (config.RecipientForm, bool) {
	switch row.GetRecipient().(type) {
	case *notifyv1.ClaimedNotification_Address:
		return config.RecipientAddress, true
	}
	return "", false
}

// cellRecipient — клетка 5: адрес разбирается address.Normalize. Журнал
// значения не несёт.
func (w *Worker) cellRecipient(_ context.Context, j *job) Step {
	n, err := address.Normalize(j.row.GetAddress())
	if err != nil {
		w.log.Warn("адрес строки не разбирается: recipient_invalid",
			"source", j.rt.src.Module, "id", j.row.GetId(), "rule", err.Error())
		return invalid(feed.ReasonRecipientInvalid)
	}
	j.res.Recipient = n
	return next()
}

// Отказы набора атрибутов, у которых нет ошибки notify/form.
var (
	errAttrMissing  = errors.New("required: ключа нет")
	errAttrZero     = errors.New("значение — нуль своего вида; отсутствие пишется отсутствием ключа")
	errAttrExtra    = errors.New("атрибут не объявлен шаблоном сборки")
	errAttrPresence = errors.New("обязательность атрибута вне перечня")
)

// cellAttrs — клетка 6: набор атрибутов по описанию сборки (NTF1-G24, G25,
// G26). Журнал называет строку и атрибут, значения не несёт.
func (w *Worker) cellAttrs(_ context.Context, j *job) Step {
	attrs, name, err := judgeAttrs(j.res.Template, j.row.GetAttrs())
	if err != nil {
		w.log.Warn("атрибут строки вне описания сборки: attrs_invalid",
			"source", j.rt.src.Module, "id", j.row.GetId(), "template", j.res.Template.Name,
			"attr", name, "rule", err.Error())
		return invalid(feed.ReasonAttrsInvalid)
	}
	j.res.Attrs = attrs
	return next()
}

// judgeAttrs — набор строки против объявленных атрибутов шаблона: ключ
// required есть; значение — Given по notify/form (нуль — не значение, у
// optional отсутствие пишется отсутствием ключа, NTF1-G26); атрибут темы —
// form.HeaderText; лишних ключей нет. Первое нарушение в порядке атрибутов
// шаблона, затем лишние по имени; ошибка значения не несёт.
func judgeAttrs(tpl *spec.Template, raw map[string]string) (map[string]form.Value, string, error) {
	out := make(map[string]form.Value, len(tpl.Attrs))
	declared := make(map[string]bool, len(tpl.Attrs))
	for _, a := range tpl.Attrs {
		declared[a.Name] = true
		s, given := raw[a.Name]
		if !given {
			switch a.Presence {
			case spec.PresenceRequired:
				return nil, a.Name, errAttrMissing
			case spec.PresenceOptional:
				continue
			}
			return nil, a.Name, errAttrPresence
		}
		v, present, err := form.Presence(a.Kind, s)
		if err != nil {
			return nil, a.Name, err
		}
		if present == form.Absent {
			return nil, a.Name, errAttrZero
		}
		if a.Subject {
			if _, err := form.ParseHeaderText(s); err != nil {
				return nil, a.Name, err
			}
		}
		out[a.Name] = v
	}
	for _, k := range slices.Sorted(maps.Keys(raw)) {
		if !declared[k] {
			return nil, k, errAttrExtra
		}
	}
	return out, "", nil
}

// ── клетки 7–10: право, срок, сетка, отправка ────────────────────────────────

// cellGrant — клетка 7: право источника на письмо (исключение `kaname` по
// сертификату либо ResolveSend под своим сроком поверх крайнего момента).
// Отметка постановки — вопрос к kaname, в сроки notify она не входит (УК71).
func (w *Worker) cellGrant(ctx context.Context, j *job) Step {
	v := j.rt.gate.Decide(ctx, j.res.Template.Name, j.row.GetEnqueuedAt().AsTime())
	if v.Allowed() {
		return next()
	}
	o, d := v.Outcome()
	return Step{kind: stepAck, outcome: o, deferFor: d}
}

// cellLeaseBudget — клетка 8: остаток аренды не меньше суммы сроков и срок
// строки не прошёл — оба по монотонным часам notify от отправки `Claim`
// (З21, УК65, УК80). Иначе строка не начинается и `Ack` не шлётся.
func (w *Worker) cellLeaseBudget(_ context.Context, j *job) Step {
	if left := w.clock.Until(j.leaseEnd); left < w.resolve+w.session+config.AckMargin {
		w.m.leaseBudgetShort.WithLabelValues(j.rt.src.Module).Inc()
		w.log.Warn("остаток аренды меньше суммы сроков обработки: строка не начата, без Ack",
			"source", j.rt.src.Module, "id", j.row.GetId(), "lease_left", left.String())
		return silent()
	}
	if w.clock.Until(j.expiresAt) <= 0 {
		w.log.Info("срок строки прошёл: строка не начата, без Ack",
			"source", j.rt.src.Module, "id", j.row.GetId())
		return silent()
	}
	return next()
}

// cellRecipientNet — клетка 9: резерв сетки на адресата по классу сборки.
// Исчерпание — исход клетки; сторож «ключ заменён», потолок потока и ошибка
// базы — не исход строки: без Ack (З24, CX1-68).
func (w *Worker) cellRecipientNet(ctx context.Context, j *job) Step {
	module := j.rt.src.Module
	class, ok := feedClassOf(j.res.Template.Class)
	if !ok {
		w.log.Error("класс шаблона сборки вне перечня ленты: строка не начата",
			"source", module, "id", j.row.GetId(), "template", j.res.Template.Name)
		return silent()
	}
	res, err := w.limiter.Reserve(ctx, limits.Row{Source: module, Class: class, To: j.res.Recipient})
	var exhausted *limits.ExhaustedError
	switch {
	case err == nil:
		j.reservation = res
		return next()
	case errors.As(err, &exhausted):
		if class == feed.ClassSecurity {
			return outcome(feed.KindDropped, feed.ReasonRecipientNet)
		}
		return deferred(feed.ReasonRecipientNet, clampDefer(w.clock.Until(exhausted.FreeAt)))
	case errors.Is(err, limits.ErrRecipientKeySuperseded):
		// Остановку приёма реплики по ограде ведёт её владелец (З24 (в), N7);
		// строка здесь лишь не начата: MAIL FROM не было, письма нет.
		w.log.Warn("ключ сетки заменён: строка не начата, без Ack",
			"source", module, "id", j.row.GetId())
		return silent()
	case errors.Is(err, limits.ErrGlobalCeilingReached):
		w.log.Info("суточный потолок потока достигнут: строка не начата, без Ack",
			"source", module, "id", j.row.GetId())
		return silent()
	}
	w.log.Warn("резерв сетки не выполнен (не сторож): строка не начата, без Ack",
		"source", module, "id", j.row.GetId(), "err", err.Error())
	return silent()
}

// clampDefer — время до освобождения окна сетки в границах defer_for ленты.
func clampDefer(d time.Duration) time.Duration {
	return min(max(d, feed.MinDefer), feed.MaxDefer)
}

// cellSend — клетка 10: рендер, подпись DKIM и одна SMTP-сессия под крайним
// моментом строки; исход — клетка Р11 таблицы smtp.Classify.
func (w *Worker) cellSend(ctx context.Context, j *job) Step {
	letter, err := w.render.Render(j.res)
	if err != nil {
		w.log.Error("письмо не собрано: строка отложена",
			"source", j.rt.src.Module, "id", j.row.GetId(), "template", j.res.Template.Name, "err", err.Error())
		return deferred(feed.ReasonPlatformUnavailable, w.deferFor)
	}
	msg, err := w.signer.Sign(letter)
	if err != nil {
		w.log.Error("письмо не подписано DKIM: строка отложена",
			"source", j.rt.src.Module, "id", j.row.GetId(), "template", j.res.Template.Name, "err", err.Error())
		return deferred(feed.ReasonPlatformUnavailable, w.deferFor)
	}
	to, err := j.res.Recipient.Value()
	if err != nil {
		w.log.Error("адресат строки не задан после клетки 5: строка отложена",
			"source", j.rt.src.Module, "id", j.row.GetId())
		return deferred(feed.ReasonPlatformUnavailable, w.deferFor)
	}
	a := w.sender.Send(ctx, smtp.Envelope{From: w.from, To: to}, msg)
	c := smtp.Classify(a)
	w.log.Info("сессия SMTP окончена", "source", j.rt.src.Module, "id", j.row.GetId(),
		"cell", c.Name, "stage", a.Stage.String(), "code", a.Code)
	if c.Outcome.Kind == feed.KindDefer {
		return deferred(c.Outcome.Reason, w.deferFor)
	}
	return Step{kind: stepAck, outcome: c.Outcome}
}

// ── классы ───────────────────────────────────────────────────────────────────

// wireClassOf — класс сборки в значение провода. Перечень закрыт.
func wireClassOf(c spec.Class) (notifyv1.NotificationClass, bool) {
	switch c {
	case spec.ClassSecurity:
		return notifyv1.NotificationClass_SECURITY, true
	case spec.ClassNotice:
		return notifyv1.NotificationClass_NOTICE, true
	}
	return notifyv1.NotificationClass_NOTIFICATION_CLASS_UNSPECIFIED, false
}

// feedClassOf — класс сборки в класс ленты (метка и ключ сетки).
func feedClassOf(c spec.Class) (feed.Class, bool) {
	switch c {
	case spec.ClassSecurity:
		return feed.ClassSecurity, true
	case spec.ClassNotice:
		return feed.ClassNotice, true
	}
	return "", false
}
