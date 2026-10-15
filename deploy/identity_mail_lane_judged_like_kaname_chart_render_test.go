// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// identity_mail_lane_judged_like_kaname_chart_render_test.go — почтовая полоса
// службы доступа судится в зонте по тем же правилам, что в отдельном чарте
// службы (kacho#3179).
//
// Отдельный чарт службы принимает узел в форме `host[:port]` (посадку называет
// ручка tls-mode из {starttls, implicit}, умолчание starttls) и не требует имени
// отправителя. Зонт отвергал всё, что не `smtp://`/`smtps://`, и без имени
// отправителя отказывал в рендере: значение, законное в одном чарте, ломало
// рендер в другом.
//
// Выравнивание не ослабляет стража: незашифрованной посадки у службы нет ни в
// одной форме; удостоверение в адресе, параметры и путь в голой форме, ручка
// посадки вне словаря и ручка, спорящая со схемой адреса, — отказ с причиной.
package deploy_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// renderProdMail — рендер боевого профиля с подменой узла почтовой полосы.
func renderProdMail(t *testing.T, smtp map[string]any) (string, error) {
	t.Helper()
	requireHelmForRender(t)
	base := map[string]any{"fromAddress": "kaname@mail-relay.test", "fromName": "", "connectionURI": ""}
	for k, v := range smtp {
		base[k] = v
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	return helmRenderFiles([]string{filepath.Join(umbrellaDir, "values.prod.yaml")},
		"global.kacho.identity.smtp="+string(raw))
}

// inviteMailBlock — блок invite-mail карты настроек службы доступа в рендере.
func inviteMailBlock(t *testing.T, out string) string {
	t.Helper()
	i := strings.Index(out, "\n    invite-mail:\n")
	if i < 0 {
		t.Fatalf("в рендере нет блока invite-mail службы доступа")
	}
	rest := out[i+1:]
	lines := strings.Split(rest, "\n")
	var b strings.Builder
	b.WriteString(lines[0] + "\n")
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "      ") {
			break
		}
		b.WriteString(l + "\n")
	}
	return b.String()
}

func TestMailLaneBareHostPortRendersLikeTheKanameChart(t *testing.T) {
	cases := []struct {
		name, uri, mode, wantMode string
	}{
		{"голая форма, посадка по умолчанию", "relay.mail-relay.test:587", "", "starttls"},
		{"голая форма, неявный TLS ручкой", "relay.mail-relay.test:465", "implicit", "implicit"},
		{"форма URI, ручка совпала со схемой", "smtps://relay.mail-relay.test:465/", "implicit", "implicit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := renderProdMail(t, map[string]any{"connectionURI": c.uri, "tlsMode": c.mode})
			if err != nil {
				t.Fatalf("значение, законное в отдельном чарте службы, ломает рендер зонта: %v\n%s", err, out)
			}
			blk := inviteMailBlock(t, out)
			if !strings.Contains(blk, `relay: "relay.mail-relay.test:`) {
				t.Errorf("узел не доехал до карты настроек:\n%s", blk)
			}
			if !strings.Contains(blk, `tls-mode: "`+c.wantMode+`"`) {
				t.Errorf("посадка полосы не %s:\n%s", c.wantMode, blk)
			}
		})
	}
}

func TestMailLaneWithoutSenderNameRenders(t *testing.T) {
	out, err := renderProdMail(t, map[string]any{"connectionURI": "smtp://relay.mail-relay.test:587/"})
	if err != nil {
		t.Fatalf("полоса без имени отправителя отвергнута, а отдельный чарт службы её принимает: %v\n%s", err, out)
	}
	if strings.Contains(inviteMailBlock(t, out), "from-name:") {
		t.Error("пустое имя отправителя доехало до карты настроек")
	}
}

func TestMailLaneGuardStillRefusesOnBothForms(t *testing.T) {
	cases := []struct {
		name string
		smtp map[string]any
		want string
	}{
		{"голая форма с удостоверением", map[string]any{"connectionURI": "user@relay.mail-relay.test:587"}, "host[:port]"},
		{"голая форма с параметрами", map[string]any{"connectionURI": "relay.mail-relay.test:587?skip_ssl_verify=true"}, "skip_ssl_verify"},
		{"голая форма с путём", map[string]any{"connectionURI": "relay.mail-relay.test:587/x"}, "host[:port]"},
		{"голая форма без узла", map[string]any{"connectionURI": ":587"}, "узла нет"},
		{"голая форма с портом вне TCP", map[string]any{"connectionURI": "relay.mail-relay.test:70000"}, "порт"},
		{"ручка посадки вне словаря", map[string]any{"connectionURI": "relay.mail-relay.test:587", "tlsMode": "plain"}, "tlsMode"},
		{"ручка спорит со схемой", map[string]any{"connectionURI": "smtps://relay.mail-relay.test:465/", "tlsMode": "starttls"}, "tlsMode"},
		{"форма URI без шифрования", map[string]any{"connectionURI": "smtp://relay.mail-relay.test:587/?disable_starttls=true"}, "disable_starttls"},
		{"форма URI с удостоверением", map[string]any{"connectionURI": "smtp://u:p@relay.mail-relay.test:587/"}, "УДОСТОВЕРЕНИЕ"},
		{"неизвестная схема", map[string]any{"connectionURI": "http://relay.mail-relay.test:587/"}, "схема не распознана"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := renderProdMail(t, c.smtp)
			if err == nil {
				t.Fatalf("страж почтовой полосы пропустил %v", c.smtp)
			}
			if !strings.Contains(out, c.want) {
				t.Fatalf("отказ не называет причину %q:\n%s", c.want, out)
			}
		})
	}
}
