// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dkim_test

// fixture_test.go — условия проб подписи DKIM (полоса N15; приёмка NTF-1 Р19
// «Подпись», NTF1-P02, P03, P17, P18; замысел issue-2915 §12а «Подпись»,
// условие CX1-137). Испытуемого этот файл НЕ называет: фикстура и её
// самопроверка (fixture_selfcheck_test.go) собираются и исполняются без пакета
// `dkim`, поэтому сломанная фикстура краснеет своим текстом, а не выдаёт себя
// за отсутствующую подпись.
//
// Что здесь есть:
//
//   - письма — настоящий сборщик notify (`render`, З25) над шаблоном
//     замороженного корпуса corelib по пину: кириллица (`hello/ru`) и ASCII
//     (`hello/en`);
//   - зона DNS испытания — `dnscheck/dnstest` (настоящий UDP-сервер, ответы —
//     настоящие сообщения DNS); записи DKIM публикуются несколькими строками TXT;
//   - НЕЗАВИСИМАЯ проверка подписи — `github.com/emersion/go-msgauth/dkim`
//     (только `_test.go`, §12а): подпись и проверка одного кода сходились бы при
//     общей ошибке канонизации;
//   - разбор тегов `DKIM-Signature` и перепись 7bit-формы письма — собственные
//     помощники пробы, проверенные самопроверкой на письме, подписанном
//     независимой реализацией.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	msgdkim "github.com/emersion/go-msgauth/dkim"

	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/form"
	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dkimkey"
	"github.com/PRO-Robotech/kacho/services/notify/internal/dnscheck/dnstest"
	"github.com/PRO-Robotech/kacho/services/notify/internal/render"
	"github.com/PRO-Robotech/kacho/services/notify/internal/render/emlprobe"
)

const (
	// fromDomain — домен `From` установки пробы: `d=` подписи (Р19). Отличим
	// от настоящего (зона .invalid).
	fromDomain = "example.invalid"
	// fromAddress — адрес отправителя установки (`notify.smtp.fromAddress`).
	fromAddress = "noreply@" + fromDomain
	selA        = "sela"
	selB        = "selb"
	rowID       = "0192a6c0-7a00-7000-8000-000000000015"
	// listUnsubscribe — значение заголовка List-Unsubscribe письма-близнеца.
	listUnsubscribe = "<https://console.example.invalid/notify/unsubscribe/n15>"
)

// letterDate — момент письма: управляемые часы, Date не зависит от прогона.
var letterDate = time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

// r19HeaderKeys — перечень `h=` Р19 без List-Unsubscribe; `from` — дважды.
var r19HeaderKeys = []string{"from", "from", "to", "subject", "date", "message-id", "mime-version", "content-type"}

// ── ключи ────────────────────────────────────────────────────────────────────

var (
	keysOnce   sync.Once
	keyA, keyB *rsa.PrivateKey
	keysErr    error
)

// keys — две пары RSA 2048 пробы (A и B).
func keys(t testing.TB) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	keysOnce.Do(func() {
		if keyA, keysErr = rsa.GenerateKey(rand.Reader, 2048); keysErr != nil {
			return
		}
		keyB, keysErr = rsa.GenerateKey(rand.Reader, 2048)
	})
	if keysErr != nil {
		t.Fatalf("ФИКСТУРА: ключи пробы не выпущены: %v", keysErr)
	}
	return keyA, keyB
}

// pairOf — пара DKIM так, как её отдаёт чтение тома (dkimkey.Pair).
func pairOf(sel string, k *rsa.PrivateKey) dkimkey.Pair {
	return dkimkey.Pair{Selector: sel, Key: k, Generation: "..fixture-" + sel}
}

// switchablePairs — источник пары, которую можно сменить во время пробы (так
// перепроверка стража меняет пару атомарно, §12а «Смена ключа DKIM»).
type switchablePairs struct {
	mu sync.Mutex
	p  dkimkey.Pair
}

func (s *switchablePairs) Pair() dkimkey.Pair {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.p
}

func (s *switchablePairs) set(p dkimkey.Pair) {
	s.mu.Lock()
	s.p = p
	s.mu.Unlock()
}

// ── зона DNS испытания ───────────────────────────────────────────────────────

// soundZone — зона с исправными записями Р19 для селектора selA и ключа A.
func soundZone(t testing.TB) *dnstest.Zone {
	t.Helper()
	a, _ := keys(t)
	z := dnstest.Start(t)
	z.PublishSound(fromDomain, selA, &a.PublicKey)
	return z
}

// publishSelector — запись DKIM селектора sel с открытым ключом k (SPKI,
// несколькими строками TXT, как PublishSound).
func publishSelector(t testing.TB, z *dnstest.Zone, sel string, k *rsa.PrivateKey) {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatalf("ФИКСТУРА: SPKI селектора %s: %v", sel, err)
	}
	z.Set(sel+"._domainkey."+fromDomain+".",
		dnstest.Split("v=DKIM1; k=rsa; p="+base64.StdEncoding.EncodeToString(der), 200))
}

func lookup(t testing.TB, z *dnstest.Zone, name string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	recs, err := z.Resolver().LookupTXT(ctx, strings.TrimSuffix(name, ".")+".")
	if err != nil {
		return nil
	}
	return recs
}

// ── независимая проверка ─────────────────────────────────────────────────────

// verify — проверка подписей письма независимой реализацией по записям зоны.
// Ошибка разбора письма — отказ фикстуры, а не исход проверки.
func verify(t testing.TB, z *dnstest.Zone, msg []byte) []*msgdkim.Verification {
	t.Helper()
	vs, err := msgdkim.VerifyWithOptions(bytes.NewReader(msg), &msgdkim.VerifyOptions{
		LookupTXT: func(domain string) ([]string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return z.Resolver().LookupTXT(ctx, strings.TrimSuffix(domain, ".")+".")
		},
	})
	if err != nil {
		t.Fatalf("независимая проверка не разобрала письмо: %v", err)
	}
	return vs
}

// requirePass — ровно одна подпись, и она проходит; домен — fromDomain.
func requirePass(t testing.TB, z *dnstest.Zone, msg []byte) {
	t.Helper()
	vs := verify(t, z, msg)
	if len(vs) != 1 {
		t.Fatalf("подписей в письме %d, ожидалась 1:\n%s", len(vs), head(msg))
	}
	if vs[0].Err != nil {
		t.Fatalf("независимая проверка подписи: %v (ожидался pass)\n%s", vs[0].Err, head(msg))
	}
	if vs[0].Domain != fromDomain {
		t.Fatalf("SDID проверки %q, ожидался домен From %q", vs[0].Domain, fromDomain)
	}
}

// bodyHashFail — независимая проверка вернула именно «тело не сходится»:
// постоянный и временный отказы (разбор, DNS) этим исходом не являются.
func bodyHashFail(v *msgdkim.Verification) bool {
	return v.Err != nil && !msgdkim.IsPermFail(v.Err) && !msgdkim.IsTempFail(v.Err) &&
		v.Err.Error() == "dkim: body hash did not verify"
}

// ── разбор письма ────────────────────────────────────────────────────────────

// headerOf — заголовки письма (продолжения склеены), тело — остаток.
func headerOf(t testing.TB, msg []byte) (textproto.MIMEHeader, []byte) {
	t.Helper()
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(msg)))
	h, err := r.ReadMIMEHeader()
	if err != nil {
		t.Fatalf("ФИКСТУРА: заголовки письма не разбираются: %v", err)
	}
	i := bytes.Index(msg, []byte("\r\n\r\n"))
	if i < 0 {
		t.Fatalf("ФИКСТУРА: в письме нет пустой строки между заголовками и телом")
	}
	return h, msg[i+4:]
}

// sigTags — теги первого заголовка DKIM-Signature; пробельные символы в
// значениях сняты (FWS разрешён внутри значений `h=`, `b=`, `bh=`).
func sigTags(t testing.TB, msg []byte) map[string]string {
	t.Helper()
	h, _ := headerOf(t, msg)
	raw := h.Values("Dkim-Signature")
	if len(raw) == 0 {
		t.Fatalf("в письме нет заголовка DKIM-Signature:\n%s", head(msg))
	}
	out := map[string]string{}
	for _, part := range strings.Split(raw[0], ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Join(strings.Fields(v), "")
	}
	return out
}

// headerKeys — значение `h=` по именам, в нижнем регистре и по порядку.
func headerKeys(tags map[string]string) []string {
	var out []string
	for _, k := range strings.Split(tags["h"], ":") {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func count(xs []string, x string) int {
	n := 0
	for _, v := range xs {
		if v == x {
			n++
		}
	}
	return n
}

// part — текстовая часть письма: заголовки части без снятия кодировки и
// раскодированное содержимое.
type part struct {
	mediaType string
	cte       string
	decoded   []byte
}

// parts — части multipart-письма. NextRawPart: заголовок
// Content-Transfer-Encoding остаётся тем, что выпустил сборщик.
func parts(t testing.TB, msg []byte) []part {
	t.Helper()
	h, body := headerOf(t, msg)
	mt, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mt, "multipart/") {
		t.Fatalf("ФИКСТУРА: письмо не multipart (%q): %v", h.Get("Content-Type"), err)
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var out []part
	for {
		p, err := mr.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ФИКСТУРА: часть письма не разбирается: %v", err)
		}
		raw, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("ФИКСТУРА: часть письма не читается: %v", err)
		}
		cte := strings.ToLower(strings.TrimSpace(p.Header.Get("Content-Transfer-Encoding")))
		dec := raw
		if cte == "quoted-printable" {
			if dec, err = io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw))); err != nil {
				t.Fatalf("ФИКСТУРА: часть quoted-printable не раскодируется: %v", err)
			}
		}
		pmt, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		out = append(out, part{mediaType: pmt, cte: cte, decoded: dec})
	}
	if len(out) == 0 {
		t.Fatalf("ФИКСТУРА: в письме нет частей")
	}
	return out
}

// sevenBitViolations — перепись 7bit-формы окончательных байтов письма (CX1-137):
// октет ≥ 0x80 и строка длиннее 998 октетов без CRLF — по координате. Пусто —
// письмо 7bit. Пустое письмо — нарушение: «нет байтов» не 7bit-письмо, а отсутствие предмета.
func sevenBitViolations(msg []byte) []string {
	if len(msg) == 0 {
		return []string{"письмо пусто"}
	}
	var out []string
	for i, b := range msg {
		if b >= 0x80 {
			out = append(out, fmt.Sprintf("октет 0x%02x на смещении %d", b, i))
			break
		}
	}
	for n, line := range bytes.Split(msg, []byte("\r\n")) {
		if len(line) > 998 {
			out = append(out, fmt.Sprintf("строка %d длиной %d октетов", n+1, len(line)))
		}
	}
	return out
}

// isASCII — все октеты < 0x80.
func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// maxLine — длина самой длинной строки без конца строки.
func maxLine(b []byte) int {
	m := 0
	for _, l := range bytes.Split(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), []byte("\n")) {
		m = max(m, len(l))
	}
	return m
}

// flipBodyByte — копия письма, где изменён ровно один октет тела: первая
// латинская буква тела после заголовков заменена соседней.
func flipBodyByte(t testing.TB, msg []byte) []byte {
	t.Helper()
	i := bytes.Index(msg, []byte("\r\n\r\n"))
	if i < 0 {
		t.Fatalf("ФИКСТУРА: в письме нет тела")
	}
	out := append([]byte(nil), msg...)
	for j := i + 4; j < len(out); j++ {
		if c := out[j]; (c >= 'a' && c < 'z') || (c >= 'A' && c < 'Z') {
			out[j] = c + 1
			return out
		}
	}
	t.Fatalf("ФИКСТУРА: в теле нет латинской буквы для изменения")
	return nil
}

// withHeader — письмо с заголовком name: value перед первой строкой.
func withHeader(msg []byte, name, value string) []byte {
	return append([]byte(name+": "+value+"\r\n"), msg...)
}

func head(msg []byte) string {
	if i := bytes.Index(msg, []byte("\r\n\r\n")); i > 0 {
		return string(msg[:i])
	}
	return string(msg)
}

// ── письма настоящего сборщика ───────────────────────────────────────────────

func corpusTemplate(t testing.TB, fixture string) spec.Template {
	t.Helper()
	dir, err := emlprobe.CorelibCorpusDir(fixture)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: корпус corelib: %v", err)
	}
	cat, _, err := spec.LoadFS(os.DirFS(dir), ".")
	if err != nil || len(cat.Templates) != 1 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: фикстура корпуса %s: шаблонов %d, %v", fixture, len(cat.Templates), err)
	}
	return cat.Templates[0]
}

// letter — письмо `hello` локали locale от отправителя с именем fromName,
// собранное настоящим сборщиком notify.
func letter(t testing.TB, locale, fromName string) []byte {
	t.Helper()
	name, err := form.ParseHeaderText(fromName)
	if err != nil {
		t.Fatalf("ФИКСТУРА: имя отправителя %q: %v", fromName, err)
	}
	from, err := address.Normalize(fromAddress)
	if err != nil {
		t.Fatalf("ФИКСТУРА: адрес отправителя: %v", err)
	}
	to, err := address.Normalize("n15-user@" + fromDomain)
	if err != nil {
		t.Fatalf("ФИКСТУРА: адресат: %v", err)
	}
	r, err := render.New(render.Config{
		Origin: "https://console.example.invalid",
		From:   render.Sender{Name: name, Address: from},
	})
	if err != nil {
		t.Fatalf("ФИКСТУРА: render.New: %v", err)
	}
	msg, err := r.Render(render.Letter{
		Namespace: "notifyprobe",
		RowID:     rowID,
		Template:  corpusTemplate(t, "notice-plain"),
		Locale:    locale,
		To:        to,
		Date:      letterDate,
	})
	if err != nil {
		t.Fatalf("ФИКСТУРА: письмо hello/%s не собрано: %v", locale, err)
	}
	return msg
}

// cyrillicLetter — тема и тело на кириллице, имя отправителя на кириллице.
func cyrillicLetter(t testing.TB) []byte { return letter(t, "ru", "Облако Kacho") }

// asciiLetter — тема, тело и имя отправителя только ASCII.
func asciiLetter(t testing.TB) []byte { return letter(t, "en", "Kacho Cloud") }

// independentSign — подпись письма НЕЗАВИСИМОЙ реализацией с параметрами Р19:
// ею самопроверка доказывает, что проверка и разбор тегов фикстуры
// различают pass и fail, не зовя испытуемого.
func independentSign(t testing.TB, msg []byte, sel string, k *rsa.PrivateKey) []byte {
	t.Helper()
	var out bytes.Buffer
	err := msgdkim.Sign(&out, bytes.NewReader(msg), &msgdkim.SignOptions{
		Domain:                 fromDomain,
		Selector:               sel,
		Signer:                 k,
		HeaderCanonicalization: msgdkim.CanonicalizationRelaxed,
		BodyCanonicalization:   msgdkim.CanonicalizationRelaxed,
		HeaderKeys:             r19HeaderKeys,
	})
	if err != nil {
		t.Fatalf("ФИКСТУРА: независимая подпись: %v", err)
	}
	return out.Bytes()
}
