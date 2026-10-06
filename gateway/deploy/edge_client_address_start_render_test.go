// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// edge_client_address_start_render_test.go — КРАЙ СТАРТУЕТ НА ОКРУЖЕНИИ
// РЕНДЕРА КАЖДОЙ ЦЕПОЧКИ (kacho#3028, круг 5; ban16-values-prod).
//
// Четвёртый круг кончился гейтами, зелёными на чарте, на котором край не
// стартовал в шести цепочках из семи: чарт не писал ручку имён звеньев, а
// сборка оператора адреса отказывала в старте. Гейты судили РЕНДЕР, а отказ
// живёт в КОДЕ. Эта проба соединяет одно с другим: окружение контейнера края
// берётся из рендера цепочки тем же вызовом helm, которым стенд поднимается,
// файлы, которые оно называет, кладутся туда, куда их монтирует под (тома
// секретов рендера), — и зовутся ТЕ ЖЕ функции, что у корня до первого
// слушателя:
//
//  1. config.Load — загрузка ручек;
//  2. clientaddress.Start — сборка оператора адреса клиента и все её отказы
//     старта (круг, звенья поимённо, имена звеньев, якорь звеньев, боевой
//     профиль, не доверяющий никому);
//  3. Config.ExternalListenerClientAuth — пул клиентских удостоверяющих
//     центров внешнего TLS-слушателя (без якоря звеньев в нём лист звена
//     рукопожатие не проходит).
//
// Чего проба НЕ судит: весь процесс края (дозвон до соседей, ключи издателей) —
// это держат пробы процесса в cmd/api-gateway (ka1RunEdge). Здесь предмет —
// отказ старта по адресу клиента на настоящем окружении цепочки.
//
// Файлы тома секрета — материал пробы: на каждый секрет свой удостоверяющий
// центр (ca.crt), лист им подписан (tls.crt, tls.key). Два тома ОДНОГО секрета
// получают ОДИН центр — так совпадение якоря звеньев с якорем установки видно
// пробе так же, как процессу.
//
// ЗНАМЕНАТЕЛЬ — цепочки с краем и среди них цепочки с объявленными звеньями:
// ноль первых — судить нечего, ноль вторых — сборка звеньев не исполнена ни
// разу.
package deploy_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/clientaddress"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// edgeStartEnv — окружение контейнера края из рендера, с путями файлов,
// перенесёнными под root, и сами файлы.
type edgeStartEnv struct {
	env    map[string]string
	files  int
	linked bool
}

// edgePod — спецификация пода Deployment края из рендера.
func edgePod(docs []map[string]any) (map[string]any, bool) {
	for _, d := range docs {
		if ka1Str(d, "kind") == "Deployment" && ka1Str(ka1Sub(d, "metadata"), "name") == "api-gateway" {
			return ka1Sub(ka1Sub(ka1Sub(d, "spec"), "template"), "spec"), true
		}
	}
	return nil, false
}

// materialiseEdgeEnv — окружение контейнера края и файлы его томов секретов
// под root. Пути env, лежащие внутри точки монтирования тома секрета,
// переносятся под root; файлы этих томов создаются.
func materialiseEdgeEnv(t *testing.T, pod map[string]any, root string) edgeStartEnv {
	t.Helper()
	secretOf := map[string]map[string]any{} // volume name → secret spec
	for _, v := range ka1Slice(pod, "volumes") {
		vm, _ := v.(map[string]any)
		if s := ka1Sub(vm, "secret"); s != nil {
			secretOf[ka1Str(vm, "name")] = s
		}
	}
	type mount struct {
		path   string
		secret map[string]any
	}
	var mounts []mount
	env := map[string]string{}
	for _, c := range ka1Slice(pod, "containers") {
		cm, _ := c.(map[string]any)
		if ka1Str(cm, "name") != "api-gateway" {
			continue
		}
		for _, e := range ka1Slice(cm, "env") {
			em, _ := e.(map[string]any)
			env[ka1Str(em, "name")] = ka1Str(em, "value")
		}
		for _, m := range ka1Slice(cm, "volumeMounts") {
			mm, _ := m.(map[string]any)
			if s, ok := secretOf[ka1Str(mm, "name")]; ok {
				mounts = append(mounts, mount{path: ka1Str(mm, "mountPath"), secret: s})
			}
		}
	}
	authorities := map[string]startAuthority{}
	out := edgeStartEnv{env: env}
	for _, m := range mounts {
		name := ka1Str(m.secret, "secretName")
		a, ok := authorities[name]
		if !ok {
			a = newStartAuthority(t)
			authorities[name] = a
		}
		dir := filepath.Join(root, m.path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		files := map[string]string{"ca.crt": "ca.crt", "tls.crt": "tls.crt", "tls.key": "tls.key"}
		if items := ka1Slice(m.secret, "items"); len(items) > 0 {
			files = map[string]string{}
			for _, it := range items {
				im, _ := it.(map[string]any)
				files[ka1Str(im, "path")] = ka1Str(im, "key")
			}
		}
		for path, key := range files {
			if err := os.WriteFile(filepath.Join(dir, path), a.material(t, key), 0o600); err != nil {
				t.Fatal(err)
			}
			out.files++
		}
	}
	for k, v := range env {
		for _, m := range mounts {
			if strings.HasPrefix(v, strings.TrimSuffix(m.path, "/")+"/") {
				env[k] = filepath.Join(root, v)
			}
		}
	}
	out.linked = strings.TrimSpace(env[config.TrustedProxyPeersKnob]) != ""
	return out
}

// judgeEdgeStart — зовёт функции корня на окружении env. Пустая строка —
// старт; иначе — текст отказа.
func judgeEdgeStart(t *testing.T, env map[string]string) string {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "KACHO_") {
			t.Setenv(k, "")
			_ = os.Unsetenv(k)
		}
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	cfg, err := config.Load()
	if err != nil {
		return "config.Load: " + err.Error()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resolveAny := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.244.1.17")}, nil
	}
	if _, err := clientaddress.Start(ctx, cfg, resolveAny, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		return "clientaddress.Start: " + err.Error()
	}
	if _, err := cfg.ExternalListenerClientAuth(&tls.Config{}); err != nil {
		return "ExternalListenerClientAuth: " + err.Error()
	}
	return ""
}

func TestEveryStackEdgeStartsOnItsRenderedEnvironment(t *testing.T) {
	stacks := ka1Stacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	var judged, linked int
	var findings []string
	for _, name := range names {
		r := ka1Render(t, stacks[name])
		if r.err != nil {
			t.Fatalf("%s: отрисовка отказала: %v\n%s", name, r.err, ka1LastLines(r.out, 15))
		}
		pod, ok := edgePod(r.docs)
		if !ok {
			continue
		}
		judged++
		m := materialiseEdgeEnv(t, pod, t.TempDir())
		if m.linked {
			linked++
		}
		if refusal := judgeEdgeStart(t, m.env); refusal != "" {
			findings = append(findings, fmt.Sprintf("%s: край не стартует — %s", name, refusal))
		}
	}
	t.Logf("перепись: цепочек %d, с краем %d, с объявленными звеньями %d", len(names), judged, linked)
	if judged == 0 || linked == 0 {
		t.Fatalf("знаменатель пуст: с краем %d, со звеньями %d — проба не исполнилась", judged, linked)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// Инъекции на окружении настоящей цепочки: каждая ломает ровно одно, и проба
// обязана назвать отказ; близнец — то же окружение без правки — старт.
func TestEdgeStartJudgeCatchesAWrongRenderedValue(t *testing.T) {
	const chain = "own"
	stacks := ka1Stacks(t)
	r := ka1Render(t, stacks[chain])
	if r.err != nil {
		t.Fatalf("%s: %v", chain, r.err)
	}
	pod, ok := edgePod(r.docs)
	if !ok {
		t.Fatalf("%s: края нет в рендере", chain)
	}
	base := materialiseEdgeEnv(t, pod, t.TempDir())
	if !base.linked {
		t.Fatalf("%s: звенья не объявлены — инъекциям нечего ломать", chain)
	}
	if refusal := judgeEdgeStart(t, base.env); refusal != "" {
		t.Fatalf("близнец %s: отказ старта на неправленом окружении: %s", chain, refusal)
	}
	for name, c := range map[string]struct {
		mutate func(map[string]string)
		says   string
	}{
		"имена звеньев не отрисованы": {func(e map[string]string) { delete(e, config.TrustedProxySANsKnob) }, config.TrustedProxySANsKnob},
		"якорь звеньев не отрисован":  {func(e map[string]string) { delete(e, config.TrustedProxyCAFileKnob) }, config.TrustedProxyCAFileKnob},
		"якорь звеньев = якорь установки": {func(e map[string]string) {
			e[config.TrustedProxyCAFileKnob] = e["KACHO_API_GATEWAY_MTLS_CA_FILE"]
		}, config.TrustedProxyCAFileKnob},
		"звенья поимённо не отрисованы": {func(e map[string]string) { delete(e, config.TrustedProxyPeersKnob) }, config.TrustedProxyPeersKnob},
	} {
		t.Run(name, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range base.env {
				env[k] = v
			}
			c.mutate(env)
			refusal := judgeEdgeStart(t, env)
			if refusal == "" || !strings.Contains(refusal, c.says) {
				t.Fatalf("инъекция не поймана либо отказ не называет %s: %q", c.says, refusal)
			}
		})
	}
}

// startAuthority — удостоверяющий центр одного секрета пробы.
type startAuthority struct {
	ca      *x509.Certificate
	key     *ecdsa.PrivateKey
	leafPEM []byte
	keyPEM  []byte
}

func newStartAuthority(t *testing.T) startAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "probe-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	lk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ltpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano() + 1), DNSNames: []string{"probe.local"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	lder, err := x509.CreateCertificate(rand.Reader, ltpl, ca, &lk.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, err := x509.MarshalECPrivateKey(lk)
	if err != nil {
		t.Fatal(err)
	}
	return startAuthority{ca: ca, key: key,
		leafPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: lder}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})}
}

func (a startAuthority) material(t *testing.T, key string) []byte {
	switch key {
	case "ca.crt":
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.ca.Raw})
	case "tls.crt":
		return a.leafPEM
	case "tls.key":
		return a.keyPEM
	}
	t.Fatalf("ключ секрета %q неизвестен пробе", key)
	return nil
}
