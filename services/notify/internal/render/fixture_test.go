// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// fixture_test.go — условия проб рендера notify (NTF-1 З25): шаблоны
// замороженного корпуса corelib по пину, origin, отправитель установки,
// управляемые часы.
//
// Поверхность рендера, которую утверждают пробы (полоса N5):
//
//	type Sender struct { Name form.HeaderText; Address address.Normalized }
//	type Config struct { Origin string; From Sender }
//	func New(cfg Config) (*Renderer, error)
//	type Letter struct {
//		Namespace string                // пространство ленты (Message-ID)
//		RowID     string                // идентификатор строки (Message-ID)
//		Template  spec.Template
//		Locale    string
//		Attrs     map[string]form.Value // проверенный набор: только заданные
//		To        address.Normalized
//		Date      time.Time             // управляемые часы
//	}
//	func (r *Renderer) Render(l Letter) ([]byte, error)
//	func PathLink(origin string, p form.Path) (string, error)
//	func TokenLink(origin string, path form.Path, tok form.Token) (string, error)
//	func HeaderValue(h form.HeaderText) (string, error)
package render

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/form"
	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/render/emlprobe"
)

const (
	testOrigin      = "https://console.example.invalid"
	testFromAddress = "noreply@example.invalid"
	testFromName    = "Облако Kacho"
	testRowID       = "0192a6c0-7a00-7000-8000-000000000001"
	testToken       = "0123456789abcdefghijklmnopqrstuv"
	testInviter     = "Ёлка Иванова"
	inviteLiteral   = "/iam/invitations/accept"
)

// testNow — управляемые часы: Date письма не зависит от прогона.
var testNow = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

// goldenRoot — эталоны сборки notify, от каталога пакета.
const goldenRoot = "../../testdata/golden"

func corpusTemplate(t *testing.T, fixture string) spec.Template {
	t.Helper()
	dir, err := emlprobe.CorelibCorpusDir(fixture)
	if err != nil {
		t.Fatalf("условие не создано: %v", err)
	}
	cat, _, err := spec.LoadFS(os.DirFS(dir), ".")
	if err != nil || len(cat.Templates) != 1 {
		t.Fatalf("условие не создано: фикстура %s: шаблонов %d, %v", fixture, len(cat.Templates), err)
	}
	return cat.Templates[0]
}

func mustNormalize(t *testing.T, s string) address.Normalized {
	t.Helper()
	n, err := address.Normalize(s)
	if err != nil {
		t.Fatalf("условие не создано: address.Normalize(%q): %v", s, err)
	}
	return n
}

func mustHeader(t *testing.T, s string) form.HeaderText {
	t.Helper()
	h, err := form.ParseHeaderText(s)
	if err != nil {
		t.Fatalf("условие не создано: form.ParseHeaderText(%q): %v", s, err)
	}
	return h
}

func mustPath(t *testing.T, s string) form.Path {
	t.Helper()
	p, err := form.ParsePath(s)
	if err != nil {
		t.Fatalf("условие не создано: form.ParsePath(%q): %v", s, err)
	}
	return p
}

func mustToken(t *testing.T, s string) form.Token {
	t.Helper()
	tok, err := form.ParseToken(s)
	if err != nil {
		t.Fatalf("условие не создано: form.ParseToken(%q): %v", s, err)
	}
	return tok
}

// verified — проверенный набор: значение строится только form.Require, как в
// notify после клетки 6 (З22).
func verified(t *testing.T, tpl spec.Template, raw map[string]any) map[string]form.Value {
	t.Helper()
	kinds := map[string]form.Kind{}
	for _, a := range tpl.Attrs {
		kinds[a.Name] = a.Kind
	}
	out := make(map[string]form.Value, len(raw))
	for name, v := range raw {
		k, ok := kinds[name]
		if !ok {
			t.Fatalf("условие не создано: атрибута %s нет в шаблоне %s", name, tpl.Name)
		}
		val, err := form.Require(k, v)
		if err != nil {
			t.Fatalf("условие не создано: form.Require(%s, %s): %v", k, name, err)
		}
		out[name] = val
	}
	return out
}

func testSender(t *testing.T) Sender {
	t.Helper()
	return Sender{Name: mustHeader(t, testFromName), Address: mustNormalize(t, testFromAddress)}
}

func newRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := New(Config{Origin: testOrigin, From: testSender(t)})
	if err != nil {
		t.Fatalf("New с отправителем и origin по форме: %v", err)
	}
	return r
}

// inviteLetter — условие G03: kaname/invite, атрибуты с не-ASCII именем.
func inviteLetter(t *testing.T, inviter string) Letter {
	t.Helper()
	tpl := corpusTemplate(t, "invite")
	return Letter{
		Namespace: "kaname",
		RowID:     testRowID,
		Template:  tpl,
		Locale:    "ru",
		Attrs:     verified(t, tpl, map[string]any{"inviter_name": inviter, "token": testToken}),
		To:        mustNormalize(t, "user@example.invalid"),
		Date:      testNow,
	}
}

// expectedMessageID — Р14: <hex(sha256(namespace ‖ 0x00 ‖ id))[:32]@домен отправителя>.
func expectedMessageID(namespace, id string) string {
	sum := sha256.Sum256([]byte(namespace + "\x00" + id))
	return "<" + hex.EncodeToString(sum[:])[:32] + "@example.invalid>"
}
