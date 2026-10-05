// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

// fixture_test.go — фикстура проб полосы N3 (исход строки, срок обработки,
// контекст `Ack`; замысел З21, З22, SDR-Н1). Испытуемого здесь нет: файл и его
// самопроверка (fixture_selfcheck_test.go) собираются и исполняются без
// пакета `deliver`, провязка к испытуемому — только harness_test.go.
//
// # Что в фикстуре настоящее
//
//   - Сборка шаблонов — каталоги, проверенные настоящим загрузчиком
//     `corelib/notify/spec` (LoadFS): шаблон, который загрузчик отверг бы,
//     в сборку пробы не попадает. Ревизию сборки задаёт проба — это и есть
//     условие клетки 1 («ревизия строки ≠ ревизии сборки»).
//   - Право — настоящий `grant.Resolver` с настоящим классификатором ответа;
//     поддельный только порт kaname (Peer), и он считает вызовы ResolveSend.
//   - SMTP — настоящий отправитель `smtp.Sender` против узла `smtptest` на
//     петле с TLS; сессии и принятые письма считает узел.
//
// # Что поддельное и почему оно не снисходительнее продукта
//
//   - Лента (`Ack`) — fakeFeed: правила записи исхода — те же, что у
//     оператора `Ack` corelib (З9): токен аренды, `defer_for` только у DEFER и
//     в [feed.MinDefer..feed.MaxDefer], EXPIRED в `Ack` отвергнут, после конца
//     аренды без записанного исхода — LEASE_LOST, повтор той же пары — успех,
//     иная пара — OUTCOME_ALREADY_RECORDED. Конец аренды — момент отправки
//     `Claim` плюс `lease_remaining` по МОНОТОННЫМ часам пробы. Контекст
//     вызова соблюдается так, как его соблюдает клиент gRPC: истёкший или
//     отменённый — ошибка вызова с кодом из контекста, ответа нет.
//   - Сетка — fakeLimiter: резерв считается; исчерпание — тот же тип ошибки,
//     что у `limits.Limiter` (`*limits.ExhaustedError`, `errors.Is` к
//     `limits.ErrRecipientNetExhausted`).
//   - Рендер — fakeRenderer: предмет N5, здесь только считает вызовы и отдаёт
//     письмо постоянного вида.
//   - Часы — testClock: монотонная арифметика по настоящим монотонным часам
//     со сдвигом пробы; «стенные часы прыгнули» видны только тому, кто снял
//     монотонное показание (`.UTC()`, `.Round(0)` …) — ровно класс УК80.

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/grant"
	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
	"github.com/PRO-Robotech/kacho/services/notify/internal/peeranswer"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp/smtptest"
)

const (
	// Пространства фикстуры. `kaname` — единственное пространство
	// `identityNamespaces` (Р6, З20); `probe` — стендовое пространство пробы.
	nsKaname = "kaname"
	nsProbe  = "probe"
	nsProbeB = "probe-b"

	// fixtureFrom — отправитель установки в конверте; отличим от настоящего.
	fixtureFrom = "n3-notify@example.invalid"
	// fixtureTo — адресат строк фикстуры; отличим от настоящего.
	fixtureTo = "n3-user@example.invalid"

	// Сроки процесса notify в пробах — нижние границы ручек (config.*Min):
	// сумма сроков обработки строки = 100ms + 1s + AckMargin(5s) = 6.1s.
	probeResolveSendTimeout = config.ResolveSendTimeoutMin
	probeSMTPSessionTimeout = config.SMTPSessionTimeoutMin
	probeDeferFor           = 30 * time.Second

	// leaseOK — аренда, которой хватает на сумму сроков с запасом 1.9 с.
	leaseOK = 8 * time.Second
	// leaseShort — аренда короче суммы сроков (6.1 с) на 0.1 с: «аренда
	// фикстурного сервера короче константы сборки notify» (УК65).
	leaseShort = 6 * time.Second
	// expiresFar — срок строки, далёкий от любой пробы.
	expiresFar = time.Hour
)

// sumOfDeadlines — сумма сроков обработки строки процесса пробы (З20, З21).
const sumOfDeadlines = probeResolveSendTimeout + probeSMTPSessionTimeout + config.AckMargin

// ── источники ────────────────────────────────────────────────────────────────

// sourceOf — запись перечня источника. Исключение `certificate` — только у
// `kaname` (NTF1-G22); форма `address` задаётся пробой.
func sourceOf(module string, forms ...config.RecipientForm) config.Source {
	auth := config.AuthorizationResolveSend
	if module == nsKaname {
		auth = config.AuthorizationCertificate
	}
	return config.Source{
		Module:         module,
		FeedAddr:       module + ".feed.example.invalid:9091",
		SAN:            "spiffe://kacho.cloud/ns/kacho/sa/kacho-" + module,
		Classes:        []feed.Class{feed.ClassSecurity, feed.ClassNotice},
		RecipientForms: forms,
		Authorization:  auth,
	}
}

// ── сборка шаблонов ──────────────────────────────────────────────────────────

// tmplFiles — файлы одного шаблона: notification.yaml и тела двух локалей.
type tmplFiles struct {
	notification string
	body         string // блоки одной локали; ru и en — одни и те же блоки
}

// Шаблоны фикстуры (формы — NTF1-B29, B30, G24 приёмки).
var fixtureTemplates = map[string]tmplFiles{
	// probe-bhello — G02: простейший шаблон с одной ссылкой.
	"probe-bhello": {
		notification: `name: probe-bhello
class: notice
ttl: 1h
attributes:
  target: {type: path, presence: required}
subject:
  ru: "Проба"
  en: "Probe"
`,
		body: `blocks:
  - heading: "Probe"
  - button: {text: "Open", path: target}
`,
	},
	// probe-sec — G20: шаблон класса security.
	"probe-sec": {
		notification: `name: probe-sec
class: security
ttl: 1h
limits:
  - {scope: recipient, window: 24h, max: 3}
attributes:
  target: {type: path, presence: required}
subject:
  ru: "Безопасность"
  en: "Security"
`,
		body: `blocks:
  - heading: "Security"
  - button: {text: "Open", path: target}
`,
	},
	// probe-link — G24: path, token и текст, входящий в тему.
	"probe-link": {
		notification: `name: probe-link
class: notice
ttl: 1h
attributes:
  target: {type: path, presence: required}
  token: {type: token, presence: required}
  subject_name: {type: text, presence: required}
subject:
  ru: "{{ subject_name }}"
  en: "{{ subject_name }}"
`,
		body: `blocks:
  - heading: "Link"
  - button: {text: "Open", path: target}
  - button: {text: "Accept", token: token, path: "/iam/invitations/accept"}
`,
	},
	// probe-all — G25 (B29): шесть атрибутов пяти типов.
	"probe-all": {
		notification: `name: probe-all
class: notice
ttl: 1h
attributes:
  subject_name: {type: text, presence: required}
  note: {type: text, presence: required}
  code: {type: secret, presence: required}
  target: {type: path, presence: required}
  token: {type: token, presence: required}
  issued_at: {type: timestamp, presence: required}
subject:
  ru: "{{ subject_name }}"
  en: "{{ subject_name }}"
`,
		body: `blocks:
  - heading: "All"
  - p: "{{ note }} {{ issued_at }}"
  - code: "{{ code }}"
  - button: {text: "Open", path: target}
  - button: {text: "Accept", token: token, path: "/iam/invitations/accept"}
`,
	},
	// probe-all-r1 — ревизия 1 того же шаблона: без атрибута note (G25).
	// Каталог фикстуры другой (имя каталога = имя шаблона), в сборку
	// ревизии 1 он кладётся под именем probe-all — см. buildOf.
	"probe-all-r1": {
		notification: `name: probe-all
class: notice
ttl: 1h
attributes:
  subject_name: {type: text, presence: required}
  code: {type: secret, presence: required}
  target: {type: path, presence: required}
  token: {type: token, presence: required}
  issued_at: {type: timestamp, presence: required}
subject:
  ru: "{{ subject_name }}"
  en: "{{ subject_name }}"
`,
		body: `blocks:
  - heading: "All"
  - p: "{{ issued_at }}"
  - code: "{{ code }}"
  - button: {text: "Open", path: target}
  - button: {text: "Accept", token: token, path: "/iam/invitations/accept"}
`,
	},
	// probe-opt — G26 (B30): атрибуты optional и блоки с when.
	"probe-opt": {
		notification: `name: probe-opt
class: notice
ttl: 1h
attributes:
  subject_name: {type: text, presence: required}
  inviter: {type: text, presence: optional}
  target: {type: path, presence: optional}
subject:
  ru: "{{ subject_name }}"
  en: "{{ subject_name }}"
`,
		body: `blocks:
  - heading: "Opt"
  - p: "{{ inviter }}"
    when: inviter
  - p: "plain"
  - button: {text: "Open", path: target}
    when: target
`,
	},
	// probe-opt-security — ревизия 2 probe-opt, отличающаяся одним классом
	// (G26 (д)). Кладётся в сборку под именем probe-opt.
	"probe-opt-security": {
		notification: `name: probe-opt
class: security
ttl: 1h
limits:
  - {scope: recipient, window: 24h, max: 3}
attributes:
  subject_name: {type: text, presence: required}
  inviter: {type: text, presence: optional}
  target: {type: path, presence: optional}
subject:
  ru: "{{ subject_name }}"
  en: "{{ subject_name }}"
`,
		body: `blocks:
  - heading: "Opt"
  - p: "{{ inviter }}"
    when: inviter
  - p: "plain"
  - button: {text: "Open", path: target}
    when: target
`,
	},
}

// loadTemplate проверяет шаблон фикстуры настоящим загрузчиком и ставит ему
// ревизию сборки rev. Отказ загрузчика — дефект фикстуры: проба
// останавливается своим текстом, а не выдаёт его за поведение испытуемого.
func loadTemplate(t testing.TB, fixtureName string, rev int) spec.Template {
	t.Helper()
	f, ok := fixtureTemplates[fixtureName]
	if !ok {
		t.Fatalf("ФИКСТУРА: шаблона %q в фикстуре нет", fixtureName)
	}
	name := strings.TrimSuffix(strings.TrimSuffix(fixtureName, "-r1"), "-security")
	fsys := fstest.MapFS{
		name + "/notification.yaml": {Data: []byte(f.notification)},
		name + "/body.ru.yaml":      {Data: []byte(f.body)},
		name + "/body.en.yaml":      {Data: []byte(f.body)},
	}
	cat, census, err := spec.LoadFS(fs.FS(fsys), ".")
	if err != nil {
		t.Fatalf("ФИКСТУРА: загрузчик spec отверг шаблон %q: %v", fixtureName, err)
	}
	if census.Templates != 1 || len(cat.Templates) != 1 {
		t.Fatalf("ФИКСТУРА: шаблон %q: прочитано %d, проверено %d — ждали 1 и 1",
			fixtureName, census.Templates, len(cat.Templates))
	}
	tpl := cat.Templates[0]
	tpl.Revision = spec.Revision{Number: rev}
	tpl.HasRevision = true
	return tpl
}

// fixtureBuild — сборка notify пробы: «пространство/имя» → проверенный шаблон.
type fixtureBuild struct {
	templates map[string]*spec.Template
}

// buildOf собирает сборку из записей «пространство, шаблон фикстуры, ревизия».
func buildOf(t testing.TB, entries ...buildEntry) *fixtureBuild {
	t.Helper()
	b := &fixtureBuild{templates: map[string]*spec.Template{}}
	for _, e := range entries {
		tpl := loadTemplate(t, e.fixture, e.rev)
		key := e.namespace + "/" + tpl.Name
		if _, dup := b.templates[key]; dup {
			t.Fatalf("ФИКСТУРА: шаблон %s назван в сборке дважды", key)
		}
		b.templates[key] = &tpl
	}
	return b
}

type buildEntry struct {
	namespace string
	fixture   string
	rev       int
}

// Template — шаблон сборки по пространству и имени.
func (b *fixtureBuild) Template(namespace, name string) (*spec.Template, bool) {
	tpl, ok := b.templates[namespace+"/"+name]
	return tpl, ok
}

// ── строки ленты ─────────────────────────────────────────────────────────────

// rowSpec — строка пачки `Claim` в форме ответа сервера ленты.
type rowSpec struct {
	template  string
	rev       uint32
	class     notifyv1.NotificationClass
	to        string
	attrs     map[string]string
	lease     time.Duration
	expiresIn time.Duration
	// enqueuedShift — сдвиг часов базы источника относительно часов notify
	// (УК71): enqueued_at строки = настоящий момент + сдвиг.
	enqueuedShift time.Duration
}

// claimed строит строку ответа `Claim`: новый идентификатор и токен аренды
// (UUID — форма, которую требует оператор `Ack`, УК73).
func claimed(r rowSpec) *notifyv1.ClaimedNotification {
	lease := r.lease
	if lease == 0 {
		lease = leaseOK
	}
	exp := r.expiresIn
	if exp == 0 {
		exp = expiresFar
	}
	class := r.class
	if class == notifyv1.NotificationClass_NOTIFICATION_CLASS_UNSPECIFIED {
		class = notifyv1.NotificationClass_NOTICE
	}
	to := r.to
	if to == "" {
		to = fixtureTo
	}
	attrs := make(map[string]string, len(r.attrs))
	for k, v := range r.attrs {
		attrs[k] = v
	}
	return &notifyv1.ClaimedNotification{
		Id:             newRowID(),
		LeaseToken:     uuid.NewString(),
		LeaseRemaining: durationpb.New(lease),
		Template:       r.template,
		SchemaRev:      r.rev,
		Class:          class,
		Recipient:      &notifyv1.ClaimedNotification_Address{Address: to},
		Attrs:          attrs,
		EnqueuedAt:     timestamppb.New(time.Now().Add(r.enqueuedShift)),
		ExpiresIn:      durationpb.New(exp),
	}
}

// newRowID — идентификатор строки формы `ntf-` + 17 знаков [0-9a-v].
func newRowID() string {
	const alphabet = "0123456789abcdefghijklmnopqrstuv"
	var b [17]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "ntf-" + string(b[:])
}

// withAttrs — копия набора с заменами; значение delAttr удаляет ключ.
func withAttrs(base map[string]string, edits map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(edits))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range edits {
		if v == delAttr {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}

// delAttr — правка withAttrs «ключа в строке нет».
const delAttr = "\x00delete"

// ── порт kaname: ResolveSend ─────────────────────────────────────────────────

// fakePeer — порт kaname: решение задаёт проба, вызовы считаются.
type fakePeer struct {
	mu       sync.Mutex
	decision grant.Decision
	delay    time.Duration
	calls    int
}

func (p *fakePeer) ResolveSend(ctx context.Context, _ grant.Query) (grant.Decision, error) {
	p.mu.Lock()
	p.calls++
	d, delay := p.decision, p.delay
	p.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return grant.DecisionUnset, status.FromContextError(ctx.Err()).Err()
		}
	}
	return d, nil
}

func (p *fakePeer) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// newResolver — настоящий путь ResolveSend на поддельном порту.
func newResolver(t testing.TB, peer grant.Peer, reg prometheus.Registerer, sources ...config.Source) *grant.Resolver {
	t.Helper()
	sig, err := peeranswer.NewSignals(reg)
	if err != nil {
		t.Fatalf("ФИКСТУРА: сигналы misconfigured: %v", err)
	}
	r, err := grant.New(peer, sources, grant.Policy{CallTimeout: probeResolveSendTimeout, DeferFor: probeDeferFor}, reg, sig)
	if err != nil {
		t.Fatalf("ФИКСТУРА: grant.New: %v", err)
	}
	return r
}

// ── сетка ────────────────────────────────────────────────────────────────────

// fakeLimiter — сетка на адресата: резервы и освобождения считаются; при
// exhausted резерв отвергается тем же типом, что у limits.Limiter.
type fakeLimiter struct {
	mu        sync.Mutex
	exhausted bool
	reserves  []limits.Row
	releases  int
}

func (l *fakeLimiter) Reserve(_ context.Context, row limits.Row) (*limits.Reservation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reserves = append(l.reserves, row)
	if l.exhausted {
		return nil, &limits.ExhaustedError{Class: row.Class, FreeAt: time.Now().Add(time.Hour)}
	}
	return &limits.Reservation{}, nil
}

func (l *fakeLimiter) Release(_ context.Context, res *limits.Reservation) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if res == nil {
		return errors.New("fakeLimiter: освобождение без резерва")
	}
	l.releases++
	return nil
}

func (l *fakeLimiter) Reserves() []limits.Row {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]limits.Row(nil), l.reserves...)
}

// ── рендер ───────────────────────────────────────────────────────────────────

// fakeRenderer — рендер (предмет N5): письмо постоянного вида, вызовы считаются.
type fakeRenderer struct {
	mu    sync.Mutex
	calls int
}

const fixtureMessage = "From: <" + fixtureFrom + ">\r\nTo: <" + fixtureTo + ">\r\nSubject: n3\r\n\r\nn3 body\r\n"

func (r *fakeRenderer) render() ([]byte, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return []byte(fixtureMessage), nil
}

func (r *fakeRenderer) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// ── SMTP ─────────────────────────────────────────────────────────────────────

// recordingSender — настоящий smtp.Sender, у которого записан срок контекста
// каждой сессии: срок, который испытуемый поставил SMTP (крайний момент
// строки, З21). Решений не принимает и ничего не подменяет.
type recordingSender struct {
	inner *smtp.Sender
	// innerTimeout — собственный предел сессии отправителя (Relay.SessionTimeout).
	innerTimeout time.Duration

	mu        sync.Mutex
	deadlines []time.Time // срок ctx на входе Send; нулевой — срока нет
	starts    []time.Time
	effective []time.Time // меньшее из срока ctx и собственного предела
}

func (s *recordingSender) Send(ctx context.Context, env smtp.Envelope, msg []byte) smtp.Attempt {
	now := time.Now()
	d, ok := ctx.Deadline()
	eff := now.Add(s.innerTimeout)
	if ok && d.Before(eff) {
		eff = d
	}
	s.mu.Lock()
	if !ok {
		d = time.Time{}
	}
	s.deadlines = append(s.deadlines, d)
	s.starts = append(s.starts, now)
	s.effective = append(s.effective, eff)
	s.mu.Unlock()
	return s.inner.Send(ctx, env, msg)
}

// LastEffective — меньшее из срока ctx и собственного предела последней сессии.
func (s *recordingSender) LastEffective() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.effective) == 0 {
		return time.Time{}, false
	}
	return s.effective[len(s.effective)-1], true
}

func (s *recordingSender) Deadlines() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.deadlines...)
}

// relayWith поднимает узел и собирает к нему настоящий отправитель с
// собственным пределом сессии innerTimeout (предел notify — дело испытуемого).
func relayWith(t testing.TB, script smtptest.Script, innerTimeout time.Duration) (*smtptest.Relay, *recordingSender) {
	t.Helper()
	relay := smtptest.Start(t, script)
	snd, err := smtp.NewSender(smtp.Relay{
		Host:           relay.Host,
		Port:           relay.Port,
		Roots:          relay.Roots,
		SessionTimeout: innerTimeout,
	})
	if err != nil {
		t.Fatalf("ФИКСТУРА: smtp.NewSender: %v", err)
	}
	return relay, &recordingSender{inner: snd, innerTimeout: innerTimeout}
}

// ── лента: Ack ───────────────────────────────────────────────────────────────

// leaseLost — текст отказа оператора `Ack` по потерянной аренде (З9).
const (
	reasonLeaseLost       = "LEASE_LOST"
	reasonOutcomeRecorded = "OUTCOME_ALREADY_RECORDED"
)

// fakeFeed — сервер ленты одного источника в части `Ack` (правила — З9).
type fakeFeed struct {
	mu     sync.Mutex
	leases map[string]*feedLease // по id строки
	calls  []ackCall
	// ackDelay — задержка ответа на `Ack` строки (по id); ноль — сразу.
	ackDelay map[string]func() time.Time
	// failFirst — код первого ответа на `Ack` каждой строки; OK — без отказа.
	failFirst codes.Code
	failed    map[string]bool
}

type feedLease struct {
	token    string
	end      time.Time // момент отправки Claim + lease_remaining (монотонный)
	recorded *notifyv1.Outcome
}

// ackCall — один вызов `Ack`, как его увидел сервер.
type ackCall struct {
	req      *notifyv1.AckRequest
	at       time.Time
	deadline time.Time // нулевой — срока у контекста нет
	err      error
}

func newFakeFeed() *fakeFeed {
	return &fakeFeed{leases: map[string]*feedLease{}, ackDelay: map[string]func() time.Time{}, failed: map[string]bool{}}
}

// lease регистрирует выданную аренду: конец — sentAt + lease_remaining.
func (f *fakeFeed) lease(sentAt time.Time, rows ...*notifyv1.ClaimedNotification) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range rows {
		prev := f.leases[r.GetId()]
		l := &feedLease{token: r.GetLeaseToken(), end: sentAt.Add(r.GetLeaseRemaining().AsDuration())}
		if prev != nil {
			l.recorded = prev.recorded
		}
		f.leases[r.GetId()] = l
	}
}

// Ack — сигнатура клиента gRPC ленты.
func (f *fakeFeed) Ack(ctx context.Context, req *notifyv1.AckRequest, _ ...grpc.CallOption) (*notifyv1.AckResponse, error) {
	call := ackCall{req: req, at: time.Now()}
	if d, ok := ctx.Deadline(); ok {
		call.deadline = d
	}
	resp, err := f.ack(ctx, req)
	call.err = err
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	return resp, err
}

func (f *fakeFeed) ack(ctx context.Context, req *notifyv1.AckRequest) (*notifyv1.AckResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	f.mu.Lock()
	delay := f.ackDelay[req.GetId()]
	first := f.failFirst != codes.OK && !f.failed[req.GetId()]
	if first {
		f.failed[req.GetId()] = true
	}
	f.mu.Unlock()
	if delay != nil {
		select {
		case <-time.After(time.Until(delay())):
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	}
	if first {
		return nil, status.Error(f.failFirst, "fixture: first Ack refused")
	}
	if err := validateAck(req); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.leases[req.GetId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "notification not found")
	}
	if l.recorded != nil {
		if l.recorded.GetKind() == req.GetOutcome().GetKind() && l.recorded.GetReason() == req.GetOutcome().GetReason() {
			return &notifyv1.AckResponse{}, nil
		}
		return nil, status.Error(codes.FailedPrecondition, reasonOutcomeRecorded)
	}
	if req.GetLeaseToken() != l.token || !time.Now().Before(l.end) {
		return nil, status.Error(codes.FailedPrecondition, reasonLeaseLost)
	}
	l.recorded = req.GetOutcome()
	return &notifyv1.AckResponse{}, nil
}

// validateAck — проверки оператора `Ack` до SQL (З9, УК72, УК73).
func validateAck(req *notifyv1.AckRequest) error {
	if _, err := uuid.Parse(req.GetLeaseToken()); err != nil {
		return status.Error(codes.InvalidArgument, "lease_token: must be a UUID")
	}
	kind := req.GetOutcome().GetKind()
	switch kind {
	case notifyv1.OutcomeKind_OUTCOME_UNSPECIFIED:
		return status.Error(codes.InvalidArgument, "outcome: required")
	case notifyv1.OutcomeKind_EXPIRED:
		return status.Error(codes.InvalidArgument, "outcome: EXPIRED is set by the sweeper only")
	case notifyv1.OutcomeKind_DEFER:
		d := req.GetDeferFor()
		if d == nil || d.CheckValid() != nil || d.AsDuration() < feed.MinDefer || d.AsDuration() > feed.MaxDefer {
			return status.Error(codes.InvalidArgument, "defer_for: must be in [1s..15m]")
		}
	default:
		if req.GetDeferFor() != nil {
			return status.Error(codes.InvalidArgument, "defer_for: only with DEFER")
		}
	}
	return nil
}

// Recorded — записанный исход строки; nil — исхода нет.
func (f *fakeFeed) Recorded(id string) *notifyv1.Outcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l, ok := f.leases[id]; ok {
		return l.recorded
	}
	return nil
}

// Calls — вызовы `Ack` строки по порядку.
func (f *fakeFeed) Calls(id string) []ackCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ackCall
	for _, c := range f.calls {
		if c.req.GetId() == id {
			out = append(out, c)
		}
	}
	return out
}

// ── часы ─────────────────────────────────────────────────────────────────────

// testClock — внедряемые часы notify в пробе. Монотонное время — настоящее
// плюс advance. Стенные часы «прыгают» на wallJump: скачок виден только
// арифметике над моментом, с которого монотонное показание снято.
type testClock struct {
	mu       sync.Mutex
	advance  time.Duration
	wallJump time.Duration
}

// Now — показание часов (несёт монотонное показание).
func (c *testClock) Now() time.Time {
	c.mu.Lock()
	adv := c.advance
	c.mu.Unlock()
	return time.Now().Add(adv)
}

// Since — сколько прошло от t по часам notify.
func (c *testClock) Since(t time.Time) time.Duration {
	c.mu.Lock()
	adv, jump := c.advance, c.wallJump
	c.mu.Unlock()
	now := time.Now().Add(adv)
	if hasMonotonic(t) {
		return now.Sub(t)
	}
	return now.Round(0).Add(jump).Sub(t)
}

// Until — сколько осталось до t по часам notify.
func (c *testClock) Until(t time.Time) time.Duration { return -c.Since(t) }

// hasMonotonic — несёт ли момент монотонное показание. Признак — часть
// «m=±…» в String(): так её документирует пакет time.
func hasMonotonic(t time.Time) bool { return strings.Contains(t.String(), " m=") }

// ── метрики и журнал ─────────────────────────────────────────────────────────

// counterValue — значение счётчика name с метками labels в реестре; found —
// серия есть.
func counterValue(t testing.TB, g prometheus.Gatherer, name string, labels map[string]string) (float64, bool) {
	t.Helper()
	mfs, err := g.Gather()
	if err != nil {
		t.Fatalf("ФИКСТУРА: сбор метрик: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			got := map[string]string{}
			for _, lp := range m.GetLabel() {
				got[lp.GetName()] = lp.GetValue()
			}
			match := true
			for k, v := range labels {
				if got[k] != v {
					match = false
				}
			}
			if match {
				return m.GetCounter().GetValue(), true
			}
		}
	}
	return 0, false
}

// logBuffer — журнал процесса пробы в JSON, безопасный для одновременной записи.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newLog() (*slog.Logger, *logBuffer) {
	lb := &logBuffer{}
	return slog.New(slog.NewJSONHandler(lb, &slog.HandlerOptions{Level: slog.LevelDebug})), lb
}

// ── печать исхода ────────────────────────────────────────────────────────────

func outcomeString(o *notifyv1.Outcome) string {
	if o == nil {
		return "<исхода нет>"
	}
	return fmt.Sprintf("%s(%s)", o.GetKind(), o.GetReason())
}
