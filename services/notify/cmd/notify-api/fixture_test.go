// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// fixture_test.go — оснастка проб NTF-5 стадии S1 для развёртывания
// `notify-api` (полоса N2 маршрута issue-2924; приёмка
// sub-phase-NTF-5-operator-notices-acceptance.md редакции 20, отпечаток
// ccbb0102…, §6 «Средства построения условий»).
//
// Здесь — только средства «Дано», ни одного символа испытуемого: удостоверяющий
// центр с тремя удостоверениями (край, notify-api, notify-sender), подмена
// службы доступа с журналом вызовов по сертификату вызывающего, управляемый
// разрыв пути (С-А), управляемые часы, база kacho_notify ТЕМИ миграциями, что
// встраивает точка наката, посев строк извещений, клиент края с пересланным
// принципалом. Испытуемого называет один файл — harness_test.go; самопроверка
// оснастки (fixture_selfcheck_test.go) собирается и исполняется без него.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/corelib/principalwire"
	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
)

// Удостоверения развёртываний (NTF-3 Р8, NTF-4 Р16, Р20). Значения отличимы от
// боевых по имени пространства: ни одна проба не сойдётся случайно с посадкой.
const (
	trustDomain = "kacho.cloud"
	apiSAN      = "spiffe://kacho.cloud/ns/n2-probe/sa/kacho-notify-api"
	senderSAN   = "spiffe://kacho.cloud/ns/n2-probe/sa/kacho-notify"
	gatewaySAN  = "spiffe://kacho.cloud/ns/n2-probe/sa/kacho-api-gateway"
)

// t0 — часы notify в общем «Дано» G0 (§6).
var t0 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// Субъекты G0 и сценариев (§6). Аккаунт `acc-1`, проект `prj-1`.
const (
	usrOp  = "usr-op"
	usrOp2 = "usr-op2"
	usrC   = "usr-c"
	usrV   = "usr-v"
	usrX   = "usr-x"
	usrOwn = "usr-own"
)

// Вопросы модели прав в форме «субъект отношение объект» — так их пишет журнал
// подмены и так их называет приёмка (`Check(user:usr-c, v_get, account:acc-1)`).
func question(subject, relation, object string) string {
	return "user:" + subject + " " + relation + " " + object
}

// ---------------------------------------------------------------------------
// Удостоверяющий центр
// ---------------------------------------------------------------------------

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	dir  string
	pool *x509.CertPool
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "n2-probe test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(2 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	ca := &testCA{cert: cert, key: key, dir: t.TempDir(), pool: pool}
	ca.write(t, "ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return ca
}

func (ca *testCA) caFile() string { return filepath.Join(ca.dir, "ca.pem") }

func (ca *testCA) write(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(ca.dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

var serial struct {
	mu sync.Mutex
	n  int64
}

// issue выпускает лист с одним URI-SAN san; server — ещё и DNS localhost,
// 127.0.0.1 и назначение «сервер» (сервер развёртывания несёт тот же SAN, что
// и его клиентская сторона).
func (ca *testCA) issue(t *testing.T, name string, server bool, san string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial.mu.Lock()
	serial.n++
	sn := serial.n + 1
	serial.mu.Unlock()
	u, err := url.Parse(san)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(sn),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(2 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:         []*url.URL{u},
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile = ca.write(t, name+".pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyFile = ca.write(t, name+".key", pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}))
	return certFile, keyFile
}

// clientTLS — транспорт вызывающего с удостоверением san; сервер обязан
// предъявить wantServerSAN (так край и notify-api проверяют собеседника).
func (ca *testCA) clientTLS(t *testing.T, name, san, wantServerSAN string) credentials.TransportCredentials {
	t.Helper()
	certFile, keyFile := ca.issue(t, name, false, san)
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{pair},
		RootCAs:      ca.pool,
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	}
	if wantServerSAN != "" {
		cfg.VerifyPeerCertificate = func(_ [][]byte, chains [][]*x509.Certificate) error {
			for _, ch := range chains {
				for _, u := range ch[0].URIs {
					if u.String() == wantServerSAN {
						return nil
					}
				}
			}
			return errors.New("server SAN is not " + wantServerSAN)
		}
	}
	return credentials.NewTLS(cfg)
}

// serverFiles — серверное удостоверение san с проверкой клиентов этим УЦ.
func (ca *testCA) serverFiles(t *testing.T, name, san string) grpcsrv.TLSServer {
	t.Helper()
	certFile, keyFile := ca.issue(t, name, true, san)
	return grpcsrv.TLSServer{Enable: true, CertFile: certFile, KeyFile: keyFile,
		ClientCAFiles: []string{ca.caFile()}}
}

// serverCreds — те же файлы, собранные в транспорт сервера с обязательным
// клиентским сертификатом.
func (ca *testCA) serverCreds(t *testing.T, name, san string) credentials.TransportCredentials {
	t.Helper()
	f := ca.serverFiles(t, name, san)
	pair, err := tls.LoadX509KeyPair(f.CertFile, f.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pair},
		ClientCAs:    ca.pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	})
}

// peerSAN — URI-SAN проверенного клиентского сертификата вызова; "" — нет.
func peerSAN(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ""
	}
	ti, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(ti.State.PeerCertificates) == 0 {
		return ""
	}
	for _, u := range ti.State.PeerCertificates[0].URIs {
		return u.String()
	}
	return ""
}

// ---------------------------------------------------------------------------
// Управляемые часы (§6 «время»)
// ---------------------------------------------------------------------------

type ctlClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock(at time.Time) *ctlClock { return &ctlClock{now: at} }

func (c *ctlClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *ctlClock) Set(at time.Time) {
	c.mu.Lock()
	c.now = at
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Подмена службы доступа со стороны notify-api (§6)
// ---------------------------------------------------------------------------

// kanameCall — строка журнала подмены: метод, сертификат вызывающего, вопрос.
type kanameCall struct {
	Method   string
	PeerSAN  string
	Question string
}

// kanameDouble отвечает на `Check` (InternalIAMService) и пакетную проверку
// сужателя (AuthorizeService.BatchCheck) по множеству посеянных кортежей;
// вызов любого иного метода (справочник и прочее) журнал пишет и отвечает
// Unimplemented — «вызовов справочника не было» видно по журналу.
// Отказ отобранного вызова (§6) — код на конкретный вопрос.
type kanameDouble struct {
	mu     sync.Mutex
	allow  map[string]bool
	refuse map[string]codes.Code
	log    []kanameCall
	addr   string
}

func (k *kanameDouble) grant(qs ...string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, q := range qs {
		k.allow[q] = true
	}
}

func (k *kanameDouble) revoke(q string) {
	k.mu.Lock()
	delete(k.allow, q)
	k.mu.Unlock()
}

func (k *kanameDouble) refuseOn(q string, c codes.Code) {
	k.mu.Lock()
	k.refuse[q] = c
	k.mu.Unlock()
}

// answer записывает вопрос и отвечает по кортежам либо отобранным отказом.
func (k *kanameDouble) answer(ctx context.Context, method, q string) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.log = append(k.log, kanameCall{Method: method, PeerSAN: peerSAN(ctx), Question: q})
	if c, ok := k.refuse[q]; ok {
		return false, status.Error(c, "n2-probe: selected refusal")
	}
	return k.allow[q], nil
}

func (k *kanameDouble) calls() []kanameCall {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]kanameCall(nil), k.log...)
}

// mark — длина журнала: «вызовы после шага» — срез calls()[mark:].
func (k *kanameDouble) mark() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.log)
}

func (k *kanameDouble) since(mark int) []kanameCall {
	return k.calls()[mark:]
}

const (
	methodCheck      = "/kaname.cloud.iam.v1.InternalIAMService/Check"
	methodBatchCheck = "/kaname.cloud.iam.v1.AuthorizeService/BatchCheck"
)

type checkSide struct {
	iamv1.UnimplementedInternalIAMServiceServer
	k *kanameDouble
}

func (c checkSide) Check(ctx context.Context, r *iamv1.CheckRequest) (*iamv1.CheckResponse, error) {
	ok, err := c.k.answer(ctx, methodCheck, r.GetSubjectId()+" "+r.GetRelation()+" "+r.GetObject())
	if err != nil {
		return nil, err
	}
	return &iamv1.CheckResponse{Allowed: ok}, nil
}

type narrowSide struct {
	iamv1.UnimplementedAuthorizeServiceServer
	k *kanameDouble
}

// BatchCheck пишет ОДНУ строку журнала на пакет: вопрос — перечень проверок
// пакета через «; » в порядке пакета, как их задал вызывающий. Ответ — по
// каждой проверке своим кортежем.
func (n narrowSide) BatchCheck(ctx context.Context, r *iamv1.BatchAuthorizeCheckRequest) (*iamv1.BatchAuthorizeCheckResponse, error) {
	qs := make([]string, 0, len(r.GetChecks()))
	for _, c := range r.GetChecks() {
		qs = append(qs, fmt.Sprintf("%s %s %s:%s", c.GetSubject(), c.GetRequiredRelation(),
			c.GetResource().GetType(), c.GetResource().GetId()))
	}
	n.k.mu.Lock()
	n.k.log = append(n.k.log, kanameCall{Method: methodBatchCheck, PeerSAN: peerSAN(ctx), Question: strings.Join(qs, "; ")})
	out := &iamv1.BatchAuthorizeCheckResponse{}
	for _, q := range qs {
		out.Responses = append(out.Responses, &iamv1.AuthorizeCheckResponse{Allowed: n.k.allow[q]})
	}
	n.k.mu.Unlock()
	return out, nil
}

// serveKaname поднимает подмену на петле с mTLS этого УЦ.
func serveKaname(t *testing.T, ca *testCA) *kanameDouble {
	t.Helper()
	k := &kanameDouble{allow: map[string]bool{}, refuse: map[string]codes.Code{}}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(
		grpc.Creds(ca.serverCreds(t, "kaname-double", "spiffe://kacho.cloud/ns/n2-probe/sa/kaname")),
		grpc.UnknownServiceHandler(func(_ any, ss grpc.ServerStream) error {
			m, _ := grpc.MethodFromServerStream(ss)
			k.mu.Lock()
			k.log = append(k.log, kanameCall{Method: m, PeerSAN: peerSAN(ss.Context())})
			k.mu.Unlock()
			return status.Error(codes.Unimplemented, "n2-probe: method is not served by the double")
		}),
	)
	iamv1.RegisterInternalIAMServiceServer(srv, checkSide{k: k})
	iamv1.RegisterAuthorizeServiceServer(srv, narrowSide{k: k})
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	k.addr = l.Addr().String()
	return k
}

// ---------------------------------------------------------------------------
// Разрыв пути (С-А, §6): управляемый TCP-прокси
// ---------------------------------------------------------------------------

type cutProxy struct {
	target string
	l      net.Listener

	mu    sync.Mutex
	cut   bool
	conns map[net.Conn]struct{}
}

func newCutProxy(t *testing.T, target string) *cutProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &cutProxy{target: target, l: l, conns: map[net.Conn]struct{}{}}
	go p.serve()
	t.Cleanup(func() {
		_ = l.Close()
		p.dropAll()
	})
	return p
}

func (p *cutProxy) addr() string { return p.l.Addr().String() }

func (p *cutProxy) serve() {
	for {
		in, err := p.l.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		if p.cut {
			p.mu.Unlock()
			_ = in.Close()
			continue
		}
		out, err := net.Dial("tcp", p.target)
		if err != nil {
			p.mu.Unlock()
			_ = in.Close()
			continue
		}
		p.conns[in], p.conns[out] = struct{}{}, struct{}{}
		p.mu.Unlock()
		go p.pipe(in, out)
		go p.pipe(out, in)
	}
}

func (p *cutProxy) pipe(dst, src net.Conn) {
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}

func (p *cutProxy) dropAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.conns {
		_ = c.Close()
		delete(p.conns, c)
	}
}

// Cut сбрасывает живые соединения и отвергает новые; Restore — снимает разрыв.
func (p *cutProxy) Cut() {
	p.mu.Lock()
	p.cut = true
	p.mu.Unlock()
	p.dropAll()
}

func (p *cutProxy) Restore() {
	p.mu.Lock()
	p.cut = false
	p.mu.Unlock()
}

// ---------------------------------------------------------------------------
// База kacho_notify
// ---------------------------------------------------------------------------

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.NewDB(t))
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: пул базы пробы не открыт: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, pool)
	return pool
}

func count(t *testing.T, pool *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("перепись %q не прочитана: %v", q, err)
	}
	return n
}

// pendingCreates — заявки создания (`notify_notice_create_pending` по базе).
func pendingCreates(t *testing.T, pool *pgxpool.Pool) int {
	return count(t, pool, `SELECT count(*) FROM notice_create_requests`)
}

func noticeRows(t *testing.T, pool *pgxpool.Pool) int {
	return count(t, pool, `SELECT count(*) FROM notices`)
}

// notifyOperations — операции notify (приставка `nop`, Х1) в базе службы.
func notifyOperations(t *testing.T, pool *pgxpool.Pool) int {
	return count(t, pool, `SELECT count(*) FROM operations WHERE id LIKE $1`, ids.PrefixOperationNotify+"%")
}

func stageEvents(t *testing.T, pool *pgxpool.Pool, noticeID, stage string) int {
	return count(t, pool, `SELECT count(*) FROM notice_stage_events WHERE notice_id = $1 AND stage = $2`, noticeID, stage)
}

// ---------------------------------------------------------------------------
// Посев извещений — «Дано», построенное строками, которые судят ограничения
// схемы (фикстура не снисходительнее продукта: строку, которой схема не
// допускает, посев не поставит).
// ---------------------------------------------------------------------------

type scope struct {
	Type    string // account | project
	ID      string
	Account string // аккаунт проекта; у области-аккаунта — он сам
}

type affected struct{ Type, ID string }

type reminderRow struct {
	At       time.Time
	Revision int
}

type noticeSeed struct {
	ID          string
	Kind        string
	State       string
	StartsAt    time.Time
	EndsAt      *time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	CancelledAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Revision    int
	CreatedBy   string
	AllAccounts bool
	Audience    []scope
	Affected    []affected
	Reminders   []reminderRow
}

func ptr(t time.Time) *time.Time { return &t }

func accountScope(id string) scope          { return scope{Type: "account", ID: id, Account: id} }
func projectScope(id, account string) scope { return scope{Type: "project", ID: id, Account: account} }
func newNoticeID() string                   { return ids.NewHyphenID("ntc") }
func at(s string) time.Time                 { v, _ := time.Parse(time.RFC3339, s); return v }
func ts(s string) *timestamppb.Timestamp    { return timestamppb.New(at(s)) }

// maintenance01 — извещение NTF5-01 в SCHEDULED, как его фиксирует notify-sender:
// принято при T0, окно 2026-10-03T02:00–04:00, одно напоминание за 24 ч.
func maintenance01(audience ...scope) noticeSeed {
	if len(audience) == 0 {
		audience = []scope{accountScope("acc-1")}
	}
	return noticeSeed{
		ID: newNoticeID(), Kind: "MAINTENANCE", State: "SCHEDULED",
		StartsAt: at("2026-10-03T02:00:00Z"), EndsAt: ptr(at("2026-10-03T04:00:00Z")),
		CreatedAt: t0, UpdatedAt: t0, Revision: 1, CreatedBy: "user:" + usrOp,
		Audience:  audience,
		Reminders: []reminderRow{{At: at("2026-10-02T02:00:00Z"), Revision: 1}},
	}
}

// outage02 — извещение NTF5-02: авария в IN_PROGRESS, начало — момент приёма.
func outage02() noticeSeed {
	return noticeSeed{
		ID: newNoticeID(), Kind: "OUTAGE", State: "IN_PROGRESS",
		StartsAt: t0, StartedAt: ptr(t0), CreatedAt: t0, UpdatedAt: t0, Revision: 1,
		CreatedBy: "user:" + usrOp, Audience: []scope{accountScope("acc-1")},
	}
}

// started27 — NTF5-01, начатое при 2026-10-03T02:05:00Z (NTF5-27).
func started27() noticeSeed {
	s := maintenance01()
	s.State, s.StartedAt, s.UpdatedAt, s.Reminders = "IN_PROGRESS",
		ptr(at("2026-10-03T02:05:00Z")), at("2026-10-03T02:05:00Z"), nil
	return s
}

func seed(t *testing.T, pool *pgxpool.Pool, s noticeSeed) noticeSeed {
	t.Helper()
	if err := seedErr(pool, s); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: «Дано» не построено — посев извещения %s (%s) отвергнут схемой: %v", s.ID, s.Kind, err)
	}
	return s
}

// seedErr ставит извещение одной транзакцией (отложенный триггер формы
// аудитории судит её на фиксации).
func seedErr(pool *pgxpool.Pool, s noticeSeed) error {
	ctx := context.Background()
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO notices (id, kind, state, starts_at, ends_at,
			audience_all, revision, created_by, created_at, updated_at, started_at, completed_at, cancelled_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			s.ID, s.Kind, s.State, s.StartsAt, s.EndsAt, s.AllAccounts, s.Revision, s.CreatedBy,
			s.CreatedAt, s.UpdatedAt, s.StartedAt, s.CompletedAt, s.CancelledAt); err != nil {
			return fmt.Errorf("notices: %w", err)
		}
		for i, a := range s.Audience {
			if _, err := tx.Exec(ctx, `INSERT INTO notice_audience (notice_id, ordinal, scope_type, scope_id, account_id)
				VALUES ($1,$2,$3,$4,$5)`, s.ID, i, a.Type, a.ID, a.Account); err != nil {
				return fmt.Errorf("notice_audience: %w", err)
			}
		}
		for i, r := range s.Affected {
			if _, err := tx.Exec(ctx, `INSERT INTO notice_affected_resources (notice_id, notice_kind, ordinal, resource_type, resource_id)
				VALUES ($1,$2,$3,$4,$5)`, s.ID, s.Kind, i, r.Type, r.ID); err != nil {
				return fmt.Errorf("notice_affected_resources: %w", err)
			}
		}
		for _, r := range s.Reminders {
			if _, err := tx.Exec(ctx, `INSERT INTO notice_reminders (notice_id, at, revision) VALUES ($1,$2,$3)`,
				s.ID, r.At, r.Revision); err != nil {
				return fmt.Errorf("notice_reminders: %w", err)
			}
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Клиент края: SAN края, пересланный принципал, ступень (§6 «служба notify и
// её база», «звено прав notify-api»)
// ---------------------------------------------------------------------------

type edge struct{ conn *grpc.ClientConn }

func dialEdge(t *testing.T, ca *testCA, addr string) edge {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(ca.clientTLS(t, "edge", gatewaySAN, apiSAN)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return edge{conn: conn}
}

// as — контекст вызова от имени пользователя с утверждением acr, как его
// пересылает край.
func as(t *testing.T, user, acr string) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return metadata.AppendToOutgoingContext(ctx,
		principalwire.MetaPrincipalType, "user",
		principalwire.MetaPrincipalID, user,
		principalwire.MetaTokenACR, acr)
}

func (e edge) internal() notifyv1.InternalNoticeServiceClient {
	return notifyv1.NewInternalNoticeServiceClient(e.conn)
}

func (e edge) public() notifyv1.NoticeServiceClient { return notifyv1.NewNoticeServiceClient(e.conn) }

func (e edge) operations() operationv1.OperationServiceClient {
	return operationv1.NewOperationServiceClient(e.conn)
}

// ---------------------------------------------------------------------------
// Формы ответов
// ---------------------------------------------------------------------------

const crockford = `[0-9abcdefghjkmnpqrstvwxyz]`

var (
	// id операции notify: приставка `nop` и 17 символов (Р2, Х1).
	opIDForm = regexp.MustCompile(`^` + ids.PrefixOperationNotify + crockford + `{17}$`)
	// id извещения: `ntc-` и 17 символов (Р3).
	noticeIDForm = regexp.MustCompile(`^ntc-` + crockford + `{17}$`)
)

// refusal — тройка синхронного отказа: код, текст, reason деталей (если есть).
type refusal struct {
	Code    codes.Code
	Message string
	Reasons []string
}

func refusalOf(err error) refusal {
	st, _ := status.FromError(err)
	r := refusal{Code: st.Code(), Message: st.Message()}
	for _, d := range st.Details() {
		if ei, ok := d.(*errdetails.ErrorInfo); ok {
			r.Reasons = append(r.Reasons, ei.GetReason())
		}
	}
	return r
}

// requireRefusal — вызов обязан вернуть статус (а не ответ) с кодом и точным
// текстом; reason — если задан, обязан быть среди деталей.
func requireRefusal(t *testing.T, what string, err error, code codes.Code, message, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: ответ вызова — успех, ожидался синхронный %s %q", what, code, message)
	}
	got := refusalOf(err)
	if got.Code != code || got.Message != message {
		t.Fatalf("%s: отказ %s %q, ожидался %s %q", what, got.Code, got.Message, code, message)
	}
	if reason != "" {
		for _, r := range got.Reasons {
			if r == reason {
				return
			}
		}
		t.Fatalf("%s: в деталях отказа нет reason=%s (есть %v)", what, reason, got.Reasons)
	}
}

// directoryCalls — вызовы подмены, отличные от звена прав и сужателя: вопрос
// справочнику и любой иной метод службы доступа.
func directoryCalls(calls []kanameCall) []kanameCall {
	var out []kanameCall
	for _, c := range calls {
		if c.Method != methodCheck && c.Method != methodBatchCheck {
			out = append(out, c)
		}
	}
	return out
}

func checkQuestions(calls []kanameCall) []string {
	var out []string
	for _, c := range calls {
		if c.Method == methodCheck {
			out = append(out, c.Question)
		}
	}
	return out
}

func batchQuestions(calls []kanameCall) []string {
	var out []string
	for _, c := range calls {
		if c.Method == methodBatchCheck {
			out = append(out, c.Question)
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Общее «Дано» G0 (§6) — оснастка без испытуемого
// ---------------------------------------------------------------------------

// world — части оснастки одной пробы. Испытуемый поднимается поверх неё
// (harness_test.go) и получает: базу, часы, путь к подмене через разрыв С-А.
type world struct {
	ca     *testCA
	clock  *ctlClock
	kaname *kanameDouble
	path   *cutProxy
	pool   *pgxpool.Pool
}

// g0 строит общее «Дано» G0 в части, которую видит notify-api: часы T0,
// кортежи модели прав. Расширение против текста G0 одно и вынесено в вопрос к
// приёмке: у `usr-op` кроме `system_admin` посеян `system_viewer` на
// `cluster:cluster_root` — модель службы доступа (fga_model.fga, тип
// cluster) держит их независимыми прямыми отношениями, а сценарии NTF5-39,
// 40, 47 (ж)–(и) зовут внутреннее чтение именно `usr-op`.
func g0(t *testing.T) *world {
	t.Helper()
	ca := newTestCA(t)
	k := serveKaname(t, ca)
	k.grant(
		question(usrOp, "system_admin", "cluster:cluster_root"),
		question(usrOp, "system_viewer", "cluster:cluster_root"),
		question(usrC, "v_get", "account:acc-1"),
		question(usrOwn, "v_get", "account:acc-1"),
		question(usrOwn, "v_update", "account:acc-1"),
	)
	return &world{
		ca: ca, clock: newClock(t0), kaname: k,
		path: newCutProxy(t, k.addr), pool: newPool(t),
	}
}

// kanameConn — соединение к подмене через разрыв С-А под удостоверением
// notify-api (журнал подмены пишет его SAN).
func (w *world) kanameConn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(w.path.addr(), grpc.WithTransportCredentials(
		w.ca.clientTLS(t, "notify-api-client", apiSAN, "spiffe://kacho.cloud/ns/n2-probe/sa/kaname")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// freePort — свободный порт петли: занят и отпущен, чтобы слушатель встал на него.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
}

// waitListening ждёт, пока слушатель примет соединение, либо исход носителя.
func waitListening(t *testing.T, addr string, stopped <-chan struct{}, serveErr func() error) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-stopped:
			t.Fatalf("носитель notify-api завершился до открытия слушателя %s: %v", addr, serveErr())
		default:
		}
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("слушатель notify-api %s не открылся за 20 с", addr)
}
