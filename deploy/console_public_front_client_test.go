// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts && consolefront

// console_public_front_client_test.go — ЗАКОННЫЙ КЛИЕНТ ПРОХОДИТ ФОРМУ ЧЕРЕЗ
// ВНЕШНИЙ ВХОД КОНСОЛИ, ПОДДЕЛАННЫЙ ПРИЗНАК — НЕТ (kacho#3024).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ И ГРАНИЦА
//
// Поднимается НАСТОЯЩАЯ раздача консоли: образ, которым её раскатывает цепочка
// стенда, и настройка, отрендеренная из той же цепочки (карта `<раздача>-nginx`
// целиком: шаблон, сторож листа TLS, вывод адреса DNS). Порты контейнера — те,
// что объявил под. За раздачей вместо края стоит ДУБЛЁР полосы формы: он
// выдаёт печенье контекста формы `kaname_form` с теми же атрибутами, что служба
// доступа (Path=/, HttpOnly, Secure, SameSite=Lax), и судит признак той же
// свёрткой (HMAC-SHA256 с ключом-контекстом над `kaname_form:<вид>`), что
// pinned kaname `humansession.FormToken`/`JudgeFormToken`. Дублёр — не предмет:
// сама проверка признака принадлежит службе доступа и держится её пробами.
// Предмет — ВХОД: доносит ли он законному клиенту печенье, не снимая проверки.
//
// Клиент — `net/http` с `cookiejar`: Secure-печенье он по http не отправляет,
// как и браузер (браузер его по http ещё и не хранит), — то есть отказ
// воспроизводится тем же механизмом, что у человека.
//
//	контроль — прежняя форма стенда: законный клиент по открытому http
//	           (внутренний порт раздачи, единственный, что публиковался
//	           наружу) получает `403 FORM_TOKEN_REJECTED`. Это ПРЕДПОСЫЛКА:
//	           позеленеет — клиент стал носить Secure по http, и вывод пробы
//	           надо пересмотреть;
//	предмет  — тот же клиент через TLS-вход проходит форму;
//	близнецы — через тот же TLS-вход подделанный признак и признак без
//	           контекста по-прежнему получают `403 FORM_TOKEN_REJECTED`;
//	http     — порт 80 входа на POST формы отвечает `308` на объявленное
//	           происхождение и до края запрос НЕ доводит.
//
// Запуск: `go test -tags 'helmcharts consolefront' -run TestConsolePublicFront ./deploy/`
// (нужны helm, docker и собранные зависимости умбреллы — `make -C deploy helm-deps`).
// Отсутствие любого — отказ пробы («условие не создано»), а не пропуск.
package deploy_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	formCookieName  = "kaname_form"
	refusalBody     = `{"code":7,"message":"form token rejected","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"FORM_TOKEN_REJECTED","domain":"iam.kaname.cloud"}]}`
	registerPath    = "/iam/v1/auth/register"
	csrfPathAndForm = "/iam/v1/auth/csrf?form=register"
)

// formToken — свёртка признака формы (та же, что у pinned kaname).
func formToken(context, kind string) string {
	mac := hmac.New(sha256.New, []byte(context))
	_, _ = mac.Write([]byte("kaname_form:" + kind))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// formLaneStandIn — дублёр полосы формы за раздачей; считает дошедшие формы.
func formLaneStandIn(registers *atomic.Int64) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/iam/v1/auth/csrf", func(w http.ResponseWriter, r *http.Request) {
		ctx := ""
		if c, err := r.Cookie(formCookieName); err == nil {
			ctx = c.Value
		}
		if ctx == "" {
			var raw [32]byte
			_, _ = rand.Read(raw[:])
			ctx = base64.RawURLEncoding.EncodeToString(raw[:])
			http.SetCookie(w, &http.Cookie{Name: formCookieName, Value: ctx, Path: "/",
				HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"csrfToken": formToken(ctx, r.URL.Query().Get("form"))})
	})
	mux.HandleFunc(registerPath, func(w http.ResponseWriter, r *http.Request) {
		registers.Add(1)
		var body struct {
			CsrfToken string `json:"csrfToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ctx := ""
		if c, err := r.Cookie(formCookieName); err == nil {
			ctx = c.Value
		}
		w.Header().Set("Content-Type", "application/json")
		if ctx == "" || subtle.ConstantTimeCompare([]byte(formToken(ctx, "register")), []byte(body.CsrfToken)) != 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, refusalBody)
			return
		}
		_, _ = io.WriteString(w, `{"accepted":true}`)
	})
	return mux
}

// frontUnderTest — что нужно, чтобы поднять раздачу из рендера цепочки.
type frontUnderTest struct {
	Image                     string
	Conf, Resolver, Reload    string
	Internal, HTTPS, Redirect int
	Origin                    string
}

func frontFromRender(t *testing.T) frontUnderTest {
	t.Helper()
	for _, r := range readPublicFrontRenders(t) {
		if _, c := judgePublicFronts([]publicFrontRender{r}); c.Fronts != 1 {
			continue
		}
		f := frontUnderTest{Origin: r.Origin}
		for _, d := range r.Docs {
			switch {
			case docKind(d) == "ConfigMap" && docName(d) == "ui-nginx":
				data, _ := d["data"].(map[string]any)
				f.Conf, _ = data["default.conf.template"].(string)
				f.Resolver, _ = data["resolver-from-resolvconf.envsh"].(string)
				f.Reload, _ = data["tls-reload.sh"].(string)
			case docKind(d) == "Deployment" && docName(d) == "ui":
				cs, _ := lookup(d, "spec", "template", "spec", "containers")
				c := cs.([]any)[0].(map[string]any)
				f.Image, _ = c["image"].(string)
				for _, p := range c["ports"].([]any) {
					pm := p.(map[string]any)
					switch pm["name"] {
					case "http":
						f.Internal = toInt(pm["containerPort"])
					case "https":
						f.HTTPS = toInt(pm["containerPort"])
					case "redirect":
						f.Redirect = toInt(pm["containerPort"])
					}
				}
			}
		}
		if f.Image == "" || f.Conf == "" || f.Reload == "" || f.HTTPS == 0 || f.Redirect == 0 || f.Internal == 0 {
			t.Fatalf("рендер цепочки %s не дал раздачи целиком: %+v", r.Stack, f)
		}
		return f
	}
	t.Fatal("ни одна цепочка не рендерит внешнего входа консоли — поднимать нечего")
	return frontUnderTest{}
}

// leafFor — одноразовый удостоверяющий центр и лист для 127.0.0.1.
func leafFor(t *testing.T, dir string) *x509.CertPool {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "probe-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "probe-front"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	kder, _ := x509.MarshalECPrivateKey(key)
	write := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tls.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	write("tls.key", pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}))
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return pool
}

func dockerOut(t *testing.T, args ...string) string {
	t.Helper()
	var so, se bytes.Buffer
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Run(); err != nil {
		t.Fatalf("docker %v: %v\n%s", args, err, se.String())
	}
	return strings.TrimSpace(so.String())
}

func TestConsolePublicFrontCarriesTheFormToALegitimateClientOnly(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker не в PATH — раздачу поднять нечем, условие пробы не создано")
	}
	front := frontFromRender(t)

	var registers atomic.Int64
	run := startFront(t, front, formLaneStandIn(&registers))
	id, pool, mapped := run.ID, run.Pool, run.Mapped
	internalAddr, httpsAddr, redirectAddr := mapped(front.Internal), mapped(front.HTTPS), mapped(front.Redirect)
	// Контроль идёт на адрес контейнера в сети моста, а НЕ на петлю: петлю
	// cookiejar (как и браузер для localhost) считает защищённой и Secure по
	// ней отправляет — контроль на петле не воспроизвёл бы человека на стенде.
	containerIP := dockerOut(t, "inspect", "-f", "{{.NetworkSettings.Networks.bridge.IPAddress}}", id)
	plainAddr := net.JoinHostPort(containerIP, fmt.Sprint(front.Internal))

	tlsTransport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	newClient := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, Transport: tlsTransport, Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	// Готовность читается по точке живости, и ответ её зависит от порта: на
	// внутреннем — `200`, на внешнем входе точка служебная и отказывает
	// (kacho#3030). Поэтому на внешнем входе ждётся ЛЮБОЙ ответ HTTP (want 0) —
	// его даёт уже поднятая раздача, — а код судит assertHealthPointInsideOnly.
	waitUp := func(base string, want int) {
		c := newClient()
		deadline := time.Now().Add(30 * time.Second)
		for {
			resp, err := c.Get(base + "/healthz")
			if err == nil {
				resp.Body.Close()
				if want == 0 || resp.StatusCode == want {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("раздача не поднялась на %s за 30 с (последняя ошибка %v)", base, err)
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	waitUp("http://"+internalAddr, http.StatusOK)
	waitUp("http://"+plainAddr, http.StatusOK)
	waitUp("https://"+httpsAddr, 0)
	assertHealthPointInsideOnly(t, newClient(), "http://"+internalAddr, "https://"+httpsAddr)

	// legitimate — законный клиент консоли: сначала признак, затем форма с ним.
	legitimate := func(c *http.Client, base, token string) (int, string) {
		t.Helper()
		resp, err := c.Get(base + csrfPathAndForm)
		if err != nil {
			t.Fatalf("GET csrf %s: %v", base, err)
		}
		var got struct {
			CsrfToken string `json:"csrfToken"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		if resp.StatusCode != 200 || got.CsrfToken == "" {
			t.Fatalf("GET csrf %s: %d, признака нет", base, resp.StatusCode)
		}
		if token == "" {
			token = got.CsrfToken
		}
		body := fmt.Sprintf(`{"csrfToken":%q,"email":"probe@example.invalid","password":"probe-password-1"}`, token)
		pr, err := c.Post(base+registerPath, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST register %s: %v", base, err)
		}
		b, _ := io.ReadAll(pr.Body)
		pr.Body.Close()
		return pr.StatusCode, string(b)
	}

	t.Run("контроль: прежняя форма стенда — открытый http — законному клиенту отказ", func(t *testing.T) {
		code, body := legitimate(newClient(), "http://"+plainAddr, "")
		if code != http.StatusForbidden || !strings.Contains(body, "FORM_TOKEN_REJECTED") {
			t.Fatalf("по открытому http законный клиент получил %d %s — предпосылка пробы не держится: "+
				"клиент стал носить Secure-печенье по http", code, body)
		}
		t.Logf("http: %d %s", code, body)
	})
	t.Run("предмет: через TLS-вход законный клиент проходит форму", func(t *testing.T) {
		code, body := legitimate(newClient(), "https://"+httpsAddr, "")
		if code != http.StatusOK {
			t.Fatalf("через TLS-вход законный клиент получил %d %s", code, body)
		}
		t.Logf("https: %d %s", code, body)
	})
	t.Run("близнец: подделанный признак через TLS-вход — отказ", func(t *testing.T) {
		code, body := legitimate(newClient(), "https://"+httpsAddr, formToken("forged-context", "register"))
		if code != http.StatusForbidden || !strings.Contains(body, "FORM_TOKEN_REJECTED") {
			t.Fatalf("подделанный признак получил %d %s", code, body)
		}
		t.Logf("https, подделка: %d %s", code, body)
	})
	t.Run("близнец: признак без контекста через TLS-вход — отказ", func(t *testing.T) {
		donor := newClient()
		resp, err := donor.Get("https://" + httpsAddr + csrfPathAndForm)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			CsrfToken string `json:"csrfToken"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		// Чужой клиент — без печенья донора: признак есть, контекста нет.
		body := fmt.Sprintf(`{"csrfToken":%q}`, got.CsrfToken)
		pr, err := newClient().Post("https://"+httpsAddr+registerPath, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(pr.Body)
		pr.Body.Close()
		if pr.StatusCode != http.StatusForbidden || !strings.Contains(string(b), "FORM_TOKEN_REJECTED") {
			t.Fatalf("признак без контекста получил %d %s", pr.StatusCode, b)
		}
	})
	t.Run("http: порт 80 входа переадресует и формы не обслуживает", func(t *testing.T) {
		before := registers.Load()
		pr, err := newClient().Post("http://"+redirectAddr+registerPath, "application/json", strings.NewReader(`{"csrfToken":"x"}`))
		if err != nil {
			t.Fatal(err)
		}
		pr.Body.Close()
		if pr.StatusCode != http.StatusPermanentRedirect {
			t.Fatalf("POST по http на вход: %d, ожидалось 308", pr.StatusCode)
		}
		if loc := pr.Header.Get("Location"); loc != front.Origin+registerPath {
			t.Fatalf("Location %q, ожидалось %q", loc, front.Origin+registerPath)
		}
		if n := registers.Load() - before; n != 0 {
			t.Fatalf("POST по http дошёл до края %d раз — http обслуживает форму", n)
		}
		// Чужой заголовок Host в Location не отражается.
		req, _ := http.NewRequest(http.MethodGet, "http://"+redirectAddr+"/", nil)
		req.Host = "attacker.example"
		gr, err := newClient().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		gr.Body.Close()
		if loc := gr.Header.Get("Location"); loc != front.Origin+"/" {
			t.Fatalf("с чужим Host переадресация увела на %q", loc)
		}
	})
}

// frontRun — поднятая раздача: контейнер, якорь её листа TLS и адрес порта.
type frontRun struct {
	ID     string
	Pool   *x509.CertPool
	Mapped func(port int) string
}

// startFront — поднимает НАСТОЯЩУЮ раздачу из рендера цепочки (образ, карта
// настройки, порты пода), а за ней вместо края — edge. Дублёр края слушает на
// шлюзе сети контейнеров, чтобы раздача дошла до него по адресу из окружения.
// Контейнер снимается по окончании пробы; журнал раздачи — при её провале.
func startFront(t *testing.T, front frontUnderTest, edge http.Handler) frontRun {
	t.Helper()
	// ЗВЕНО К КРАЮ (kacho#3028, круг 5): раздача ходит к краю по TLS и
	// предъявляет лист звена. Дублёр края поэтому — TLS-сервер с листом на имя,
	// которое раздача сверяет (`proxy_ssl_name` рендера), и он ТРЕБУЕТ
	// клиентский лист якоря звеньев: раздача, не предъявившая лист, до дублёра
	// не доходит вовсе. Каталоги листа звена и удостоверяющего центра края
	// берутся из директив рендера, а не выписываются.
	linkCrt := nginxDirectiveValue(t, front.Conf, "proxy_ssl_certificate")
	edgeCA := nginxDirectiveValue(t, front.Conf, "proxy_ssl_trusted_certificate")
	serverName := nginxDirectiveValue(t, front.Conf, "proxy_ssl_name")
	linkDir, caDir := t.TempDir(), t.TempDir()
	linkPool := writeProbePKI(t, linkDir, "probe-front-link-ca", probeLinkSAN, x509.ExtKeyUsageClientAuth, "tls.crt", "tls.key", "")
	edgePair, edgeRoots := probeServerPair(t, caDir, serverName)
	_ = edgeRoots

	gw := dockerOut(t, "network", "inspect", "bridge", "-f", "{{(index .IPAM.Config 0).Gateway}}")
	ln, err := net.Listen("tcp", net.JoinHostPort(gw, "0"))
	if err != nil {
		t.Fatalf("дублёр края не слушает на шлюзе сети контейнеров %s: %v", gw, err)
	}
	stub := httptest.NewUnstartedServer(edge)
	stub.Listener = ln
	stub.TLS = &tls.Config{Certificates: []tls.Certificate{edgePair}, ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs: linkPool, MinVersion: tls.VersionTLS12}
	stub.StartTLS()
	t.Cleanup(stub.Close)
	upstream := strings.TrimPrefix(stub.URL, "https://")

	dir := t.TempDir()
	pool := leafFor(t, dir)
	for name, body := range map[string]string{
		"default.conf.template": front.Conf, "05-resolver.envsh": front.Resolver, "06-tls-reload.sh": front.Reload,
	} {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") || strings.HasSuffix(name, ".envsh") {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{dir, linkDir, caDir} {
		_ = os.Chmod(d, 0o755)
	}

	args := []string{"run", "-d", "--read-only", "--user", "101:101",
		"--tmpfs", "/tmp:uid=101,gid=101", "--tmpfs", "/var/cache/nginx:uid=101,gid=101",
		"--tmpfs", "/etc/nginx/conf.d:uid=101,gid=101",
		"-v", filepath.Join(dir, "default.conf.template") + ":/etc/nginx/templates/default.conf.template:ro",
		"-v", filepath.Join(dir, "05-resolver.envsh") + ":/docker-entrypoint.d/05-resolver-from-resolvconf.envsh:ro",
		"-v", filepath.Join(dir, "06-tls-reload.sh") + ":/docker-entrypoint.d/06-tls-reload.sh:ro",
		"-v", dir + ":/etc/console-tls:ro",
		"-v", linkDir + ":" + filepath.Dir(linkCrt) + ":ro",
		"-v", caDir + ":" + filepath.Dir(edgeCA) + ":ro",
		"-p", fmt.Sprintf("127.0.0.1::%d", front.Internal),
		"-p", fmt.Sprintf("127.0.0.1::%d", front.HTTPS),
		"-p", fmt.Sprintf("127.0.0.1::%d", front.Redirect),
		"-e", "KACHO_UI_API_GATEWAY_UPSTREAM=" + upstream,
	}
	for _, m := range []string{"DASHBOARD", "VPC", "IAM", "NLB", "REGISTRY", "SYSTEM", "COMPUTE", "STORAGE"} {
		args = append(args, "-e", "KACHO_UI_"+m+"_UPSTREAM=127.0.0.1:9")
	}
	args = append(args, front.Image)
	id := dockerOut(t, args...)
	t.Cleanup(func() {
		if t.Failed() {
			logs, _ := exec.Command("docker", "logs", id).CombinedOutput()
			t.Logf("журнал раздачи:\n%s", logs)
		}
		_ = exec.Command("docker", "rm", "-f", id).Run()
	})
	return frontRun{ID: id, Pool: pool, Mapped: func(p int) string {
		out := dockerOut(t, "port", id, fmt.Sprintf("%d/tcp", p))
		return strings.TrimSpace(strings.Split(out, "\n")[0])
	}}
}

// probeLinkSAN — имя звена в листе пробы; дублёр края видит его в
// проверенной цепочке, если раздача предъявила смонтированный лист.
const probeLinkSAN = "api-gateway-front-console.front-link.kacho.internal"

// nginxDirectiveValue — значение первой директивы name в настройке раздачи.
func nginxDirectiveValue(t *testing.T, conf, name string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\s+(\S+?)\s*;`).FindStringSubmatch(conf)
	if m == nil {
		t.Fatalf("в настройке раздачи нет директивы %s — полосы к краю не несут TLS звена", name)
	}
	return m[1]
}

// writeProbePKI — удостоверяющий центр и лист с именем dns в каталоге dir
// (файлы crtName/keyName, ca.crt — caName, если задан). Отдаёт пул центра.
func writeProbePKI(t *testing.T, dir, caCN, dns string, eku x509.ExtKeyUsage, crtName, keyName, caName string) *x509.CertPool {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: caCN},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano() + 1), Subject: pkix.Name{CommonName: dns},
		DNSNames: []string{dns}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{eku}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	kder, _ := x509.MarshalECPrivateKey(key)
	write := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if crtName != "" {
		write(crtName, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
		write(keyName, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}))
	}
	if caName != "" {
		write(caName, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return pool
}

// probeServerPair — серверный лист дублёра края на имя serverName и ca.crt
// его центра в каталоге caDir (им раздача проверяет край).
func probeServerPair(t *testing.T, caDir, serverName string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	pairDir := t.TempDir()
	pool := writeProbePKI(t, pairDir, "probe-edge-ca", serverName, x509.ExtKeyUsageServerAuth, "tls.crt", "tls.key", "ca.crt")
	raw, err := os.ReadFile(filepath.Join(pairDir, "ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caDir, "ca.crt"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(pairDir, "tls.crt"), filepath.Join(pairDir, "tls.key"))
	if err != nil {
		t.Fatal(err)
	}
	return pair, pool
}
