// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package streamrevocation_test

// basic_credential_lane_test.go — ТРЕТЬЯ полоса отзыва на открытом соединении:
// базовый секрет (kacho#1450).
//
// # Предмет
//
// Две полосы спрашиваются с открытого потока после kacho#1410. Третья — базовый
// секрет — не спрашивалась НИ ПО ЧЕМУ: вопрос о ней требовал самой предъявленной
// строки, а держать её живой весь срок соединения значило бы завести поверхность
// хранения ради контроля. Такие потоки считались неспрашиваемыми, и их окно
// отзыва равнялось СРОКУ ЖИЗНИ СОЕДИНЕНИЯ — то есть контроль стоял на выдаче и
// не стоял на предъявлении.
//
// Владелец завёл вопрос ПО ИДЕНТИФИКАТОРУ строки удостоверения; здесь
// проверяется, что спрашивающий появился.
//
// # Каждое отрицание — в паре с положительным контролем
//
// «Поток закрыт» зеленело бы на устройстве, закрывающем всё подряд; «поток жив»
// — на устройстве, не закрывающем ничего. Поэтому обе стороны стоят рядом, а
// сверх них утверждается, что у авторитета РЕАЛЬНО спросили: без этого «поток
// пережил отзыв» неотличимо от «про поток не спрашивали вовсе».

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/streamrevocation"
)

// basicHeaders — заголовки, какие ставит полоса базового секрета: личность
// служебной учётки и идентификатор строки удостоверения. `jti` не ставится —
// базовый секрет не подписанное утверждение и его не несёт.
func basicHeaders(credentialID string) map[string]string {
	return map[string]string{
		principalmeta.HeaderPrincipalType:          "service_account",
		principalmeta.HeaderPrincipalID:            "sva00000000000000001",
		principalmeta.HeaderTokenBasicCredentialID: credentialID,
	}
}

// TestBasicCredentialStreamIsAskedByItsIdentifier — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ:
// поток базового секрета спрашивается, и спрашивается ПО ИДЕНТИФИКАТОРУ.
//
// Без него отрицание ниже зеленело бы на перепросе, который никого не
// спрашивает: «жив» и «не исполнялся» — одно наблюдение.
func TestBasicCredentialStreamIsAskedByItsIdentifier(t *testing.T) {
	s := newStand(t, nil)
	done := s.openStream(t, basicHeaders("bcr00000000000000009"))

	runCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go s.sweeper.Run(runCtx)

	aliveFor(t, done, time.Second,
		"поток закрыт при живом базовом удостоверении — перепрос закрывает не по отзыву, "+
			"и тогда «закрыт по отзыву» ничего не означает")

	asked := s.basic.askedIDs()
	if len(asked) == 0 {
		t.Fatal("у авторитета не спросили про базовое удостоверение НИ РАЗУ: окно отзыва " +
			"этой полосы остаётся равным сроку жизни соединения")
	}
	for _, id := range asked {
		if id != "bcr00000000000000009" {
			t.Fatalf("спрошено про %q вместо идентификатора строки удостоверения — "+
				"вопрос адресован не тем", id)
		}
	}
}

// TestRevokedBasicCredentialClosesTheOpenStream — НЕСУЩЕЕ утверждение задачи.
func TestRevokedBasicCredentialClosesTheOpenStream(t *testing.T) {
	s := newStand(t, nil)
	const cid = "bcr00000000000000010"
	done := s.openStream(t, basicHeaders(cid))

	runCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go s.sweeper.Run(runCtx)

	// Отзыв вносится ПОСЛЕ открытия: иначе «поток закрыт отзывом» и «поток не
	// открылся» — одно и то же наблюдение.
	s.basic.setDead(cid)

	closedWithin(t, done, 10*time.Second,
		"поток пережил отзыв СВОЕГО базового удостоверения: контроль стоит на выдаче и "+
			"не стоит на предъявлении, а такое состояние само не сходится")
}

// TestBasicLaneOutranksTheBrowserQuestionForTheSameStream — у потока, открытого
// базовым секретом ЧЕЛОВЕКА, спрашивается ЖИВОСТЬ, а не браузерная отсечка.
//
// # Почему это отдельное утверждение, а не деталь порядка веток
//
// Владельцем базового удостоверения бывает человек, и тогда полоса ставит его
// личность в заголовки. Спроси перепрос по ней браузерную отсечку — он задал бы
// вопрос ЧУЖОЙ полосы: путь запроса базовому секрету отсечку не задаёт вовсе.
// Расхождение двух полос одного механизма никто бы не решал, а наблюдалось бы
// оно как закрытие потоков, которые запрос пропускает.
func TestBasicLaneOutranksTheBrowserQuestionForTheSameStream(t *testing.T) {
	s := newStand(t, nil)
	const cid = "bcr00000000000000011"
	const uid = "usr00000000000000011"
	done := s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType:          "user",
		principalmeta.HeaderPrincipalID:            uid,
		principalmeta.HeaderTokenBasicCredentialID: cid,
	})

	// Отсечка субъекта СТОИТ, и по браузерной полосе поток был бы закрыт
	// (момента аутентификации у базового секрета нет — эта ветка закрывает).
	s.authority.setCutoff(uid, time.Now().Add(-time.Minute))

	ctx := context.Background()
	s.sweeper.Sweep(ctx)

	aliveFor(t, done, 500*time.Millisecond,
		"поток базового секрета закрыт БРАУЗЕРНОЙ отсечкой — это вопрос чужой полосы, "+
			"и путь запроса его этому удостоверению не задаёт")

	if got := s.basic.askedIDs(); len(got) != 1 || got[0] != cid {
		t.Fatalf("спрошено о живости: %v; ждали ровно один вопрос про %q", got, cid)
	}
	if _, users := s.authority.asked(); len(users) != 0 {
		t.Fatalf("про субъекта %v спрошена браузерная отсечка — полоса выбрана неверно", users)
	}
}

// TestBasicLivenessRolloutWindowPassesLoudly — «метода нет» у авторитета есть
// ОКНО РАСКАТА, а не отказ и не разрешение.
//
// Считать его отказом значило бы закрывать потоки всего флота на всё окно;
// считать разрешением молча — потерять единственный признак того, что полоса
// сейчас без отзыва.
func TestBasicLivenessRolloutWindowPassesLoudly(t *testing.T) {
	sink := &logSink{}
	s := newStand(t, func(c *streamrevocation.Config) { c.Logger = sink.logger() })
	done := s.openStream(t, basicHeaders("bcr00000000000000012"))

	s.basic.goUnsupported()
	s.sweeper.Sweep(context.Background())

	aliveFor(t, done, 300*time.Millisecond,
		"поток закрыт окном раската: служба прав ещё не докатилась, а состояние сходится само")

	if got := sink.text(); !strings.Contains(got, "streams_liveness_unsupported=1") {
		t.Fatalf("окно раската полосы базового секрета не названо числом в журнале:\n%s", got)
	}
}
