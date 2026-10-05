// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dnscheck_test

// zone_test.go — зона DNS испытания для проб стража (полоса N14, §12а):
// тонкая обёртка над двойником `dnscheck/dnstest`, которым пользуется и
// каждый in-process старт notify. Второй реализации зоны в дереве нет: пробы
// стража и пробы старта судят один и тот же двойник.
//
// Сеть настоящая: UDP-сервер на петле, ответы — настоящие сообщения DNS.
// Резолвер пробы — `*net.Resolver` с `PreferGo` и `Dial` на адрес зоны:
// стандартный разбор ответа, склейка строк TXT и род ошибки — те же, что в
// бою. Зона не снисходительнее настоящей: имени без записей она отвечает
// NXDOMAIN, а не пустым «успехом».

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dnscheck/dnstest"
)

// zoneMode — как зона отвечает.
type zoneMode = dnstest.Mode

const (
	zoneAnswer   = dnstest.Answer   // записи зоны
	zoneServfail = dnstest.Servfail // SERVFAIL на каждый запрос
	zoneSilent   = dnstest.Silent   // запрос принят, ответа нет
)

// arrival — запрос, пришедший в зону.
type arrival struct {
	name string
	at   time.Time
}

type zone struct{ z *dnstest.Zone }

func startZone(t *testing.T) *zone {
	t.Helper()
	return &zone{z: dnstest.Start(t)}
}

// set заменяет записи имени; без записей снимает имя из зоны.
func (z *zone) set(name string, records ...[]string) { z.z.Set(name, records...) }
func (z *zone) setMode(m zoneMode)                   { z.z.SetMode(m) }
func (z *zone) setFailing(f func() bool)             { z.z.SetFailing(f) }
func (z *zone) seen() []arrival {
	var out []arrival
	for _, a := range z.z.Seen() {
		out = append(out, arrival{name: a.Name, at: a.At})
	}
	return out
}
func (z *zone) resolver() *net.Resolver { return z.z.Resolver() }

// split255 режет значение записи на строки TXT: запись DKIM приходит
// несколькими строками, и проверка обязана их склеить (§12а).
func split255(s string, size int) []string { return dnstest.Split(s, size) }

// TestZoneFixtureAnswersLikeARealServer — фикстура доказывается раньше, чем ею
// судят: зона отдаёт склеенную запись TXT, на имя без записей — «записи нет»,
// в режиме SERVFAIL — временный отказ, в режиме молчания — истечение срока.
// Испытуемого проба не зовёт: её красное — сломанная фикстура, а не предмет.
func TestZoneFixtureAnswersLikeARealServer(t *testing.T) {
	z := startZone(t)
	z.set("rec.example.test.", []string{"v=DKIM1; ", "p=AAAA"})
	res := z.resolver()
	ctx := context.Background()

	got, err := res.LookupTXT(ctx, "rec.example.test.")
	if err != nil || len(got) != 1 || got[0] != "v=DKIM1; p=AAAA" {
		t.Fatalf("зона не отдала склеенную запись: %q, %v", got, err)
	}
	var dnsErr *net.DNSError
	if _, err := res.LookupTXT(ctx, "absent.example.test."); !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
		t.Fatalf("имя без записей не дало IsNotFound: %v", err)
	}
	z.setMode(zoneServfail)
	if _, err := res.LookupTXT(ctx, "rec.example.test."); !errors.As(err, &dnsErr) || dnsErr.IsNotFound || !(dnsErr.IsTemporary || dnsErr.IsTimeout) {
		t.Fatalf("SERVFAIL не дал временного отказа: %v", err)
	}
	z.setMode(zoneSilent)
	sctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if _, err := res.LookupTXT(sctx, "rec.example.test."); err == nil {
		t.Fatal("молчащая зона дала ответ")
	}
	if n := len(z.seen()); n < 4 {
		t.Fatalf("зона записала %d запросов, ожидалось не меньше 4", n)
	}
}
