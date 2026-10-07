// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// harness_test.go — стенд пробы в процессе: база pgtest с миграциями бинаря,
// тестовый УЦ и сертификаты со SPIFFE-идентификаторами, посеянный владелец
// модели прав (Check звена решения и BatchCheck сужателя) на своём слушателе.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/notify/feed"
	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/config"
)

const (
	trustDomain = "kacho.cloud"
	notifySAN   = "spiffe://kacho.cloud/ns/kacho/sa/kacho-notify"
	gatewaySAN  = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"
	// readerOnFeed — вопрос, который обязан задать Claim от notify.
	readerOnFeed = "service:notify reader notification_feed:notify-probe"
)

// testCA — УЦ пробы: выпускает серверный сертификат слушателей и клиентские
// сертификаты с одним URI-SAN.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
	dir  string
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "notify-probe test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
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
	ca := &testCA{cert: cert, key: key, dir: t.TempDir(),
		pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
	ca.write(t, "ca.pem", ca.pem)
	return ca
}

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

// issue выпускает лист: server — DNS localhost и 127.0.0.1; иначе — клиент с
// одним URI-SAN san. Возвращает пути сертификата и ключа.
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
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(sn),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		u, err := url.Parse(san)
		if err != nil {
			t.Fatal(err)
		}
		tmpl.URIs = []*url.URL{u}
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

// clientCreds — транспорт вызывающего с клиентским сертификатом san.
func (ca *testCA) clientCreds(t *testing.T, name, san string) credentials.TransportCredentials {
	t.Helper()
	certFile, keyFile := ca.issue(t, name, false, san)
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pair},
		RootCAs:      pool,
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	})
}

// serverTLS — серверные креды слушателя пробы с проверкой клиентского
// сертификата этим УЦ.
func (ca *testCA) serverTLS(t *testing.T) grpcsrv.TLSServer {
	t.Helper()
	certFile, keyFile := ca.issue(t, "notify-probe", true, "")
	return grpcsrv.TLSServer{Enable: true, CertFile: certFile, KeyFile: keyFile,
		ClientCAFiles: []string{filepath.Join(ca.dir, "ca.pem")}}
}

// freePort — свободный порт петли: занят и отпущен, чтобы слушатель носителя
// встал на него (носитель слушает «:порт»).
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// model — посеянный владелец модели прав: Check звена решения о доступе и
// BatchCheck сужателя потока отвечают по одному множеству кортежей
// «субъект отношение объект» и записывают заданные вопросы.
type model struct {
	allow map[string]bool

	mu    sync.Mutex
	asked []string
}

func (m *model) record(q string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.asked = append(m.asked, q)
	return m.allow[q]
}

// questions — заданные вопросы; срез-копия.
func (m *model) questions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.asked...)
}

// checkSide — Check звена решения о доступе (InternalIAMService).
type checkSide struct {
	iamv1.UnimplementedInternalIAMServiceServer
	m *model
}

func (c checkSide) Check(_ context.Context, r *iamv1.CheckRequest) (*iamv1.CheckResponse, error) {
	return &iamv1.CheckResponse{Allowed: c.m.record(r.GetSubjectId() + " " + r.GetRelation() + " " + r.GetObject())}, nil
}

// narrowSide — BatchCheck сужателя потока (AuthorizeService).
type narrowSide struct {
	iamv1.UnimplementedAuthorizeServiceServer
	m *model
}

func (n narrowSide) BatchCheck(_ context.Context, r *iamv1.BatchAuthorizeCheckRequest) (*iamv1.BatchAuthorizeCheckResponse, error) {
	out := &iamv1.BatchAuthorizeCheckResponse{}
	for _, c := range r.GetChecks() {
		q := fmt.Sprintf("%s %s %s:%s", c.GetSubject(), c.GetRequiredRelation(),
			c.GetResource().GetType(), c.GetResource().GetId())
		out.Responses = append(out.Responses, &iamv1.AuthorizeCheckResponse{Allowed: n.m.record(q)})
	}
	return out, nil
}

// serveModel поднимает владельца модели на петле и отдаёт его адрес.
func serveModel(t *testing.T, m *model) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	iamv1.RegisterInternalIAMServiceServer(srv, checkSide{m: m})
	iamv1.RegisterAuthorizeServiceServer(srv, narrowSide{m: m})
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	return l.Addr().String()
}

// keyringFile — файл кольца ключей ленты формы фундамента.
func keyringFile(t *testing.T) string {
	t.Helper()
	key := make([]byte, feed.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "keyring.json")
	body := `{"active":{"id":1,"key":"` + base64.StdEncoding.EncodeToString(key) + `"}}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// dbEnv — переменные базы пробы из строки подключения pgtest, прочитанной
// разбором драйвера (pgconn.ParseConfig, Д93): части те, с которыми драйвер
// соединился бы по этой строке. Текст отказа строки не цитирует.
func dbEnv(t *testing.T, dsn string) map[string]string {
	t.Helper()
	c, err := pgconn.ParseConfig(dsn)
	if err != nil {
		t.Fatal("строка подключения pgtest не разобрана драйвером (текст разбора несёт куски строки)")
	}
	return map[string]string{
		"KACHO_NOTIFYPROBE_DB_HOST":     c.Host,
		"KACHO_NOTIFYPROBE_DB_PORT":     strconv.Itoa(int(c.Port)),
		"KACHO_NOTIFYPROBE_DB_USER":     c.User,
		"KACHO_NOTIFYPROBE_DB_PASSWORD": c.Password,
		"KACHO_NOTIFYPROBE_DB_NAME":     c.Database,
	}
}

// standEnv — окружение процесса пробы на стенде в процессе: dev-посадка,
// внутренний слушатель с mTLS тестового УЦ, свободные порты, владелец модели
// по адресу iamAddr, флаг доставки flag ("" — переменная не задана вовсе).
func standEnv(t *testing.T, dsn, iamAddr, flag string, ca *testCA) map[string]string {
	t.Helper()
	env := dbEnv(t, dsn)
	srv := ca.serverTLS(t)
	for k, v := range map[string]string{
		"KACHO_NOTIFYPROBE_AUTH_MODE":                          "dev",
		"KACHO_NOTIFYPROBE_GRPC_PORT":                          freePort(t),
		"KACHO_NOTIFYPROBE_INTERNAL_PORT":                      freePort(t),
		"KACHO_NOTIFYPROBE_METRICS_ADDR":                       "127.0.0.1:" + freePort(t),
		"KACHO_NOTIFYPROBE_AUTHZ_IAM_GRPC_ADDR":                iamAddr,
		"KACHO_NOTIFYPROBE_AUTHZ_TRUST_DOMAIN":                 trustDomain,
		"KACHO_NOTIFYPROBE_AUTHZ_TRUSTED_FORWARDER_SANS":       gatewaySAN,
		"KACHO_NOTIFYPROBE_PUBLIC_SERVER_MTLS_ENABLE":          "true",
		"KACHO_NOTIFYPROBE_PUBLIC_SERVER_MTLS_CERTFILE":        srv.CertFile,
		"KACHO_NOTIFYPROBE_PUBLIC_SERVER_MTLS_KEYFILE":         srv.KeyFile,
		"KACHO_NOTIFYPROBE_PUBLIC_SERVER_MTLS_CLIENTCAFILES":   srv.ClientCAFiles[0],
		"KACHO_NOTIFYPROBE_INTERNAL_SERVER_MTLS_ENABLE":        "true",
		"KACHO_NOTIFYPROBE_INTERNAL_SERVER_MTLS_CERTFILE":      srv.CertFile,
		"KACHO_NOTIFYPROBE_INTERNAL_SERVER_MTLS_KEYFILE":       srv.KeyFile,
		"KACHO_NOTIFYPROBE_INTERNAL_SERVER_MTLS_CLIENTCAFILES": srv.ClientCAFiles[0],
	} {
		env[k] = v
	}
	if flag != "" {
		env[config.FlagKnob] = flag
	}
	if flag == "true" {
		env[config.KeyringKnob] = keyringFile(t)
		env[config.NotifySANKnob] = notifySAN
	}
	return env
}

// setEnv выставляет окружение процесса и снимает переменные пробы, которых в
// нём нет: проба не наследует чужой посадки.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "KACHO_NOTIFYPROBE_") {
			if _, keep := env[k]; !keep {
				t.Setenv(k, "")
				if err := os.Unsetenv(k); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	// Флаг, которого в окружении нет, обязан ОТСУТСТВОВАТЬ, а не быть пустым:
	// пустая строка — другой случай разбора.
	if _, ok := env[config.FlagKnob]; !ok {
		t.Setenv(config.FlagKnob, "")
		if err := os.Unsetenv(config.FlagKnob); err != nil {
			t.Fatal(err)
		}
	}
}
