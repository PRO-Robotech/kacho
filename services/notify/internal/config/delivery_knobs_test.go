// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// delivery_knobs_test.go — ручки цикла доставки (полоса A2 задачи #2915;
// замысел §8, З20, З21, З23, З25, З26): то, без чего композиционный корень не
// собирает исполнителей строк, путь ResolveSend и отправителя.
//
//   - `notify.workers` — число исполнителей, [1..256] (З21: размер `Claim` не
//     больше него и меньше предела ленты 500);
//   - `notify.deferFor` — отсрочка DEFER, [feed.MinDefer..feed.MaxDefer] (З23);
//   - `notify.smtp.fromName` — имя отправителя установки (заголовок From, З25);
//     значение — форма заголовка `form.HeaderText`: CR/LF в нём — отказ;
//   - `notify.kaname.addr`, `notify.kaname.san` — внутренний слушатель kaname
//     для `ResolveSend` (ребро notify → kaname, §9) и точный URI SAN его листа:
//     решение о письме принимается только от kaname, а не от любого листа УЦ;
//   - `notify.smtp.trustAnchorFile` — якорь проверки листа ретранслятора.
//     Единственная ручка с «отсутствием» как законным значением: без якоря
//     доверенный набор — корневое хранилище образа (чарт рендерит переменную
//     только при объявленном якоре узла). Заданная пустой строкой — отказ:
//     отсутствие выражается отсутствием переменной.
//
// Каждое отрицание меняет ровно одну строку фикстуры и утверждает имя СВОЕЙ
// ручки; близнецы — края границ, принятые стражем.

import (
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

const (
	workersEnv     = "KACHO_NOTIFY_WORKERS"
	deferForEnv    = "KACHO_NOTIFY_DEFER_FOR"
	fromNameEnv    = "KACHO_NOTIFY_SMTP_FROM_NAME"
	kanameAddrEnv  = "KACHO_NOTIFY_KANAME_ADDR"
	kanameSANEnv   = "KACHO_NOTIFY_KANAME_SAN"
	trustAnchorEnv = "KACHO_NOTIFY_SMTP_TRUST_ANCHOR_FILE"
)

func TestConfig_DeliveryKnobWithoutValueRefusesStart(t *testing.T) {
	for env, knob := range map[string]string{
		workersEnv:    "notify.workers",
		deferForEnv:   "notify.deferFor",
		fromNameEnv:   "notify.smtp.fromName",
		kanameAddrEnv: "notify.kaname.addr",
		kanameSANEnv:  "notify.kaname.san",
	} {
		t.Run(knob, func(t *testing.T) {
			useFixture(t, map[string]*string{env: nil})
			requireOnlyRefusal(t, start(t), knob, "не задана")
		})
	}
}

func TestConfig_DeliveryKnobOutOfFormRefusesStart(t *testing.T) {
	cases := []struct {
		name, env, value, knob, why string
	}{
		{"workers 0", workersEnv, "0", "notify.workers", "[1..256]"},
		{"workers 257", workersEnv, "257", "notify.workers", "[1..256]"},
		{"deferFor 999ms", deferForEnv, "999ms", "notify.deferFor", "[1s..15m0s]"},
		{"deferFor 15m1s", deferForEnv, "15m1s", "notify.deferFor", "[1s..15m0s]"},
		{"fromName пусто", fromNameEnv, "", "notify.smtp.fromName", "пусто"},
		{"fromName с переводом строки", fromNameEnv, "Kacho\r\nBcc: x@example.invalid", "notify.smtp.fromName", "заголов"},
		{"kaname.addr без порта", kanameAddrEnv, "kaname-internal", "notify.kaname.addr", "узел:порт"},
		{"kaname.addr порт вне границы", kanameAddrEnv, "kaname-internal:0", "notify.kaname.addr", "[1..65535]"},
		{"kaname.san не spiffe", kanameSANEnv, "https://kaname.example.invalid/x", "notify.kaname.san", "spiffe"},
		{"kaname.san без пути", kanameSANEnv, "spiffe://kacho.cloud", "notify.kaname.san", "пути"},
		{"trustAnchorFile пусто", trustAnchorEnv, "", "notify.smtp.trustAnchorFile", "отсутствием переменной"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFixture(t, map[string]*string{c.env: str(c.value)})
			requireOnlyRefusal(t, start(t), c.knob, c.why)
		})
	}
}

// TestConfig_DeliveryKnobsReachTheirReaders — близнец отрицаний: края границ
// приняты, и значение доходит до читателя композиционного корня ровно тем, что
// задано.
func TestConfig_DeliveryKnobsReachTheirReaders(t *testing.T) {
	for _, c := range []struct {
		workers  string
		deferFor string
		want     int
		wantDur  time.Duration
	}{
		{"1", "1s", 1, time.Second},
		{"256", "15m", 256, 15 * time.Minute},
	} {
		t.Run(c.workers+"/"+c.deferFor, func(t *testing.T) {
			useFixture(t, map[string]*string{workersEnv: str(c.workers), deferForEnv: str(c.deferFor)})
			cfg := mustStart(t)
			if cfg.Workers != c.want || cfg.DeferFor != c.wantDur {
				t.Fatalf("workers=%d deferFor=%v, задано %s и %s", cfg.Workers, cfg.DeferFor, c.workers, c.deferFor)
			}
		})
	}

	useFixture(t, map[string]*string{fromNameEnv: str("Kacho Cloud")})
	cfg := mustStart(t)
	name, err := cfg.FromName().Value()
	if err != nil || name != "Kacho Cloud" {
		t.Fatalf("имя отправителя = %q (%v), задано «Kacho Cloud»", name, err)
	}
	if cfg.KanameAddr == "" || !strings.HasPrefix(cfg.KanameSAN, "spiffe://") {
		t.Fatalf("адрес kaname %q, SAN %q — фикстура не дошла до читателя", cfg.KanameAddr, cfg.KanameSAN)
	}
}

// TestConfig_TrustAnchorAbsenceIsLawful — якорь ретранслятора: переменной
// нет — старт, и читатель отвечает «якоря нет»; задан путь — читатель
// отдаёт ровно его.
func TestConfig_TrustAnchorAbsenceIsLawful(t *testing.T) {
	useFixture(t, map[string]*string{trustAnchorEnv: nil})
	cfg := mustStart(t)
	if p, ok := cfg.TrustAnchorFile(); ok || p != "" {
		t.Fatalf("переменной якоря нет, а читатель отдал (%q, %v)", p, ok)
	}

	useFixture(t, map[string]*string{trustAnchorEnv: str("/var/run/kacho/notify/smtp-anchor/ca.crt")})
	cfg = mustStart(t)
	if p, ok := cfg.TrustAnchorFile(); !ok || p != "/var/run/kacho/notify/smtp-anchor/ca.crt" {
		t.Fatalf("якорь задан, а читатель отдал (%q, %v)", p, ok)
	}
}

// TestConfig_TrustAnchorKnobIsInTheCensus — ручка якоря входит в перепись
// [config.Knobs]: её видит гейт выключателей транспорта и снимают пробы.
func TestConfig_TrustAnchorKnobIsInTheCensus(t *testing.T) {
	for _, k := range config.Knobs() {
		if k.Env == trustAnchorEnv {
			if k.Name != "notify.smtp.trustAnchorFile" {
				t.Fatalf("ручка %s названа %q", trustAnchorEnv, k.Name)
			}
			return
		}
	}
	t.Fatalf("ручки %s в переписи нет", trustAnchorEnv)
}

func mustStart(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("загрузка: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("старт отвергнут: %v", err)
	}
	return cfg
}
