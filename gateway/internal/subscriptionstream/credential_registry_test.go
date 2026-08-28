// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

// credential_registry_test.go — реестр открытых потоков ВТОРЫМ ключом
// (kacho#1410).
//
// # Что здесь утверждается и что — в другом месте
//
// Здесь: поток учитывается под тем УДОСТОВЕРЕНИЕМ, которым его назовёт отзыв, и
// снимается с этого учёта, выходя. Оба свойства принадлежат реестру и от того,
// КАК приехал отзыв, не зависят.
//
// Сквозной вопрос — «отзыв удостоверения, приехавший от авторитета, закрывает
// открытый поток» — ставит `gateway/internal/credentialrevocation`.

import (
	"testing"
	"time"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// heldHandler — ручка со стендовым владельцем, держащим поток открытым.
func heldHandler(t *testing.T) *subscriptionstream.Handler {
	t.Helper()
	held := &ownerStub{
		script:  []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)},
		hold:    true,
		started: make(chan struct{}),
	}
	return newHandler(t, held, func(c *subscriptionstream.Config) {
		c.StreamBudget = 60 * time.Second
		c.Heartbeat = 20 * time.Second
	})
}

// openWith открывает поток с названными заголовками удостоверения.
func openWith(t *testing.T, h *subscriptionstream.Handler, headers ...string) <-chan struct{} {
	t.Helper()
	r := request("owner=probe", headers...)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		serve(t, h, r)
	}()
	return finished
}

// TestStreamIsBookedUnderTheCredentialTheRevocationNames — ОБЕ формы заголовка.
//
// Полоса аутентификации ставит идентификатор удостоверения в голой и мостовой
// формах. Прочитай край одну — и поток, пришедший со второй, оказался бы учтён
// под ПУСТЫМ удостоверением: закрыть его по отзыву было бы нельзя ни при каких
// условиях, а выглядело бы это как исправная работа.
func TestStreamIsBookedUnderTheCredentialTheRevocationNames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header string
	}{
		{name: "голая форма", header: principalmeta.HeaderTokenJti},
		{name: "мостовая форма", header: principalmeta.HeaderGRPCMetaTokenJti},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := heldHandler(t)
			done := openWith(t, h, tc.header, "jti-booked")
			waitStreams(t, h, 1)

			creds := h.OpenCredentials()
			if len(creds) != 1 {
				t.Fatalf("учтено удостоверений %d, ожидалось 1", len(creds))
			}
			if creds[0].JTI != "jti-booked" {
				t.Fatalf("поток учтён под удостоверением %q — отзыв назовёт его иначе, "+
					"и закрыть его будет нельзя ни при каких условиях", creds[0].JTI)
			}
			if n := h.CloseCredential(creds[0]); n != 1 {
				t.Fatalf("закрыто потоков %d, ожидался 1", n)
			}
			<-done
		})
	}
}

// TestOneCredentialManyTabsIsAskedOnceAndClosedTogether — консоль открывает
// поток НА ВКЛАДКУ, и все вкладки предъявляют ОДНО удостоверение.
//
// Две половины и обе несущие: спрашивается оно один раз (иначе десяток вкладок
// оплачивался бы десятком вопросов соседу — притом в обычном, а не в редком
// случае), а закрывается вместе (иначе выход закрыл бы одну вкладку из десяти,
// и человек продолжал бы читать поток по отозванному удостоверению).
func TestOneCredentialManyTabsIsAskedOnceAndClosedTogether(t *testing.T) {
	h := heldHandler(t)
	first := openWith(t, h, principalmeta.HeaderTokenJti, "jti-tabs")
	waitStreams(t, h, 1)
	second := openWith(t, h, principalmeta.HeaderTokenJti, "jti-tabs")
	waitStreams(t, h, 2)

	creds := h.OpenCredentials()
	if len(creds) != 1 {
		t.Fatalf("учтено различных удостоверений %d, ожидалось 1 — вкладки одного человека "+
			"предъявляют одно удостоверение, и спрашивать про него дважды значит платить "+
			"соседу за собственную небрежность", len(creds))
	}
	if n := h.CloseCredential(creds[0]); n != 2 {
		t.Fatalf("закрыто потоков %d, ожидалось 2 — вкладки одного удостоверения закрываются вместе", n)
	}
	<-first
	<-second
}

// TestCredentialLeavesTheRegistryWithItsLastStream — учёт снимается вместе с
// последним потоком.
//
// Иначе реестр растёт по числу когда-либо предъявленных удостоверений, а обход
// его — по числу давно ушедших: каждый обход платил бы соседу вопросами про
// потоки, которых нет.
func TestCredentialLeavesTheRegistryWithItsLastStream(t *testing.T) {
	h := heldHandler(t)
	done := openWith(t, h, principalmeta.HeaderTokenJti, "jti-transient")
	waitStreams(t, h, 1)
	if len(h.OpenCredentials()) != 1 {
		t.Fatal("удостоверение открытого потока не учтено")
	}

	h.CloseAll()
	<-done
	waitStreams(t, h, 0)

	if creds := h.OpenCredentials(); len(creds) != 0 {
		t.Fatalf("после ухода потока учтено удостоверений %d — обход платил бы вопросами "+
			"про потоки, которых нет", len(creds))
	}
}

// TestCredentialWithoutAQuestionIsStillBookedButNotAskable — ОСТАТОК учтён, а не
// потерян.
//
// Поток, чьё удостоверение не несёт вопроса (базовый секрет: ни идентификатора,
// ни человека до потока не доезжает), на учёт встаёт — иначе fail-closed не смог
// бы закрыть и его, — но спрашивать про него нечего, и это свойство названо
// здесь, у самого удостоверения, а не выведено на стороне сметателя.
func TestCredentialWithoutAQuestionIsStillBookedButNotAskable(t *testing.T) {
	h := heldHandler(t)
	r := request("owner=probe")
	r.Header.Set(principalmeta.HeaderPrincipalType, "service_account")
	r.Header.Set(principalmeta.HeaderPrincipalID, "sva-probe")
	done := make(chan struct{})
	go func() {
		defer close(done)
		serve(t, h, r)
	}()
	waitStreams(t, h, 1)

	creds := h.OpenCredentials()
	if len(creds) != 1 {
		t.Fatalf("учтено удостоверений %d, ожидалось 1 — иначе fail-closed не закрыл бы и его", len(creds))
	}
	if creds[0].Askable() {
		t.Fatal("удостоверение без идентификатора и без человека объявлено спрашиваемым — " +
			"вопрос ни о ком вернул бы «не отозвано» на всё, о чём его не задавали")
	}
	// Положительный контроль: у человека с идентификатором удостоверения вопрос
	// есть. Без него утверждение выше зеленело бы на реализации, объявляющей
	// неспрашиваемым всё подряд.
	askable := subscriptionstream.Credential{JTI: "jti-any"}
	if !askable.Askable() {
		t.Fatal("удостоверение с идентификатором объявлено неспрашиваемым")
	}

	h.CloseAll()
	<-done
}

// TestClosingByAnUnaskableCredentialClosesNobody — ВЫРОЖДЕННЫЙ ключ.
//
// У потоков, чьё удостоверение не несёт вопроса, ключ учёта ОДИН И ТОТ ЖЕ —
// пустой. Закрытие по нему выглядело бы поимённым, а закрыло бы всех, кого не о
// чем было спросить. Положительный контроль стоит рядом: тот же реестр закрывает
// спрашиваемое удостоверение — иначе отрицание зеленело бы на устройстве, не
// закрывающем никого.
func TestClosingByAnUnaskableCredentialClosesNobody(t *testing.T) {
	h := heldHandler(t)

	r := request("owner=probe")
	r.Header.Set(principalmeta.HeaderPrincipalType, "service_account")
	r.Header.Set(principalmeta.HeaderPrincipalID, "sva-one")
	unaskable := make(chan struct{})
	go func() {
		defer close(unaskable)
		serve(t, h, r)
	}()
	waitStreams(t, h, 1)

	askable := openWith(t, h, principalmeta.HeaderTokenJti, "jti-askable")
	waitStreams(t, h, 2)

	if n := h.CloseCredential(subscriptionstream.Credential{}); n != 0 {
		t.Fatalf("по вырожденному ключу закрыто потоков %d — вызов, выглядящий поимённым, "+
			"закрыл бы всех, кого не о чем было спросить", n)
	}
	select {
	case <-unaskable:
		t.Fatal("поток закрыт по удостоверению, про которое вопроса не задавали")
	case <-time.After(200 * time.Millisecond):
	}

	// Ключ берётся У РЕЕСТРА, а не собирается здесь: собранный тут был бы вторым
	// кодеком, и разошёлся бы он молча — обе строки непусты, обе выглядят
	// удостоверением, а закрыть по второй нельзя ничего. (Первая редакция этой
	// пробы собрала его сама и упала — положительный контроль отработал.)
	var target subscriptionstream.Credential
	for _, c := range h.OpenCredentials() {
		if c.JTI == "jti-askable" {
			target = c
		}
	}
	if n := h.CloseCredential(target); n != 1 {
		t.Fatalf("закрыто потоков %d, ожидался 1 — реестр обязан закрывать спрашиваемое", n)
	}
	<-askable

	h.CloseAll()
	<-unaskable
}
