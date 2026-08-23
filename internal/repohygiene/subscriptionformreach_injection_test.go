// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionformreach_injection_test.go — способность гейта «есть кому взять
// форму» упасть, доказанная в обе стороны и БЕЗ сети.
//
// Гейт, написанный после контракта, зелен по построению: он проверяет то, что
// уже написано. Здесь дефект вносится по-настоящему, законный близнец ставится
// рядом, и обе стороны предъявляются до того, как гейт признан работающим.
//
// # Изоляция
//
// Дерево строится в `t.TempDir()` обычной записью файлов (`subscriptionStand`).
// Ни `git init`, ни `git add`, ни `git config`: запись в индекс репозитория, из
// которого идёт прогон, делает лживыми ВСЕ гейты, читающие дерево.
package repohygiene

import (
	"strings"
	"testing"
)

// ── содержимое стенда ───────────────────────────────────────────────────────

// reachCommonForm — общий пакет с ТРЕМЯ типами: ровно та форма, чью досягаемость
// гейт и наблюдает. Слово `message` в прозе стоит намеренно.
const reachCommonForm = `syntax = "proto3";
package kacho.cloud.subscription;
option go_package = "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription;subscriptionv1";
// Здесь в прозе стоит слово message { — и оно обязано быть невидимо.
message SubscriptionRequest {
  repeated string kinds = 1;
  message NestedNeverAddressedAlone {
    string ignored = 1;
  }
}
message SubscriptionOpened {
  string position = 1;
}
message SubscriptionEvent {
  string position = 1;
}
`

// reachFiller — доменный контракт, не имеющий к подписке отношения. Нужен, чтобы
// обход был непуст там, где ссылок нет вовсе.
const reachFiller = `syntax = "proto3";
package kacho.cloud.other.v1;
message Thing {
  string id = 1;
}
`

// reachProtoReferrer — ЗАКОННАЯ ссылка из другого контракта: домен взял общий тип.
const reachProtoReferrer = `syntax = "proto3";
package kacho.cloud.demo.v1;
import "kacho/cloud/subscription/subscription.proto";
message DemoWiring {
  kacho.cloud.subscription.SubscriptionRequest request = 1;
  kacho.cloud.subscription.SubscriptionOpened opened = 2;
  kacho.cloud.subscription.SubscriptionEvent event = 3;
}
`

// reachGoReferrer — ЗАКОННАЯ ссылка из прод-кода: сервер употребляет типы.
const reachGoReferrer = `package server

import (
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
)

func Serve(req *subscriptionv1.SubscriptionRequest) (*subscriptionv1.SubscriptionOpened, error) {
	_ = req
	var e subscriptionv1.SubscriptionEvent
	_ = e
	return &subscriptionv1.SubscriptionOpened{}, nil
}
`

// reachGoBlankImport — НЕ ссылка: пустой импорт наполняет реестр дескрипторов и
// об употреблении типа не говорит ничего. Лежит в ПРОД-файле намеренно: правило
// «пустой импорт не считается» обязано действовать не только в пробах.
const reachGoBlankImport = `package wiring

import (
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
)
`

// reachGoTestReferrer — НЕ ссылка: проба живёт ради гейта, а гейт ради этой
// формы. Засчитать её значило бы замкнуть наблюдение на само себя.
//
// Близнец НЕ побайтовый: имя пакета другое, псевдоним импорта задан явно и
// отличается от умолчания, употребление стоит внутри функции пробы. Гейт,
// ключующийся на форме текста, а не на суффиксе имени файла, здесь ошибётся.
const reachGoTestReferrer = `package server_test

import (
	"testing"

	subs "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
)

func TestSomething(t *testing.T) {
	var r subs.SubscriptionRequest
	var o subs.SubscriptionOpened
	var e subs.SubscriptionEvent
	_, _, _ = r, o, e
}
`

// reachGoGeneratedStub — НЕ ссылка: стаб существует ОТТОГО, что объявление есть,
// и потому свидетельством его нужности не является.
const reachGoGeneratedStub = `package subscriptionv1

import (
	other "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
)

var _ = other.SubscriptionRequest{}
`

// reachGoNearMissName — НЕ ссылка, и это отдельная ось: сосед употребляет тип с
// ПОХОЖИМ именем. Гейт, ищущий подстроку без границ слова, засчитает его за
// ссылку на снятый тип и промолчит там, где обязан говорить.
const reachGoNearMissName = `package neighbour

import (
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
)

var _ = subscriptionv1.SubscriptionRequestV2{}
`

// reachOptions — вход анализатора на стенде. Каталоги кода перечисляются явно:
// стенд мелкий, и обход всего корня скрыл бы промах в отборе файлов.
func reachOptions(root string, goRoots ...string) SubscriptionReachOptions {
	if len(goRoots) == 0 {
		goRoots = []string{"services"}
	}
	return SubscriptionReachOptions{Root: root, ProtoRoot: "proto", GoRoots: goRoots}
}

func reachAudit(
	t *testing.T, files map[string]string, goRoots ...string,
) ([]SubscriptionFormType, SubscriptionReachCensus) {
	t.Helper()
	root := subscriptionStand(t, files)
	var log strings.Builder
	types, census, err := AuditSubscriptionFormReach(reachOptions(root, goRoots...), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v\n%s", err, log.String())
	}
	t.Log(strings.TrimSpace(log.String()))
	return types, census
}

// baseStand — минимальное дерево: общая форма, наполнитель и пустой каталог кода.
func baseStand() map[string]string {
	return map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": reachCommonForm,
		"proto/kacho/cloud/other/v1/thing.proto":            reachFiller,
		"services/demo/doc.go":                              "package demo\n",
	}
}

// TestSubscriptionReachDecisionCanFail — РЕШЕНИЕ, четыре стороны, без сети.
func TestSubscriptionReachDecisionCanFail(t *testing.T) {
	t.Run("ноль ссылок + задача ЗАКРЫТА — краснеет и называет каждый тип", func(t *testing.T) {
		types, census := reachAudit(t, baseStand())
		if census.CommonTypes != 3 || census.Unreferenced != 3 {
			t.Fatalf("стенд не тот: типов %d, без ссылок %d — ждали 3 и 3",
				census.CommonTypes, census.Unreferenced)
		}
		got := SubscriptionReachFindings(types, "CLOSED")
		if len(got) != 3 {
			t.Fatalf("находок %d, а ждали три (по одной на тип): %v", len(got), got)
		}
		joined := strings.Join(got, "\n")
		for _, want := range []string{"SubscriptionRequest", "SubscriptionOpened", "SubscriptionEvent", "#1018"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("находка не называет %q:\n%s", want, joined)
			}
		}
		t.Logf("%s", got[0])
	})

	t.Run("ноль ссылок + задача ОТКРЫТА — молчит, а номер называет перепись", func(t *testing.T) {
		types, _ := reachAudit(t, baseStand())
		if got := SubscriptionReachFindings(types, "OPEN"); len(got) != 0 {
			t.Fatalf("живое послабление объявлено истёкшим: %v", got)
		}
		if SubscriptionFormTaker != 1018 {
			t.Fatalf("номер задачи-берущего %d — находка обязана называть предмет, "+
				"иначе послабление некому истечь", SubscriptionFormTaker)
		}
	})

	t.Run("состояние НЕ ВЫЯСНЕНО — НЕ находка", func(t *testing.T) {
		// Несостоявшееся измерение не превращается в вердикт: иначе временная
		// недоступность трекера роняла бы прогон и его научились бы обходить.
		types, _ := reachAudit(t, baseStand())
		if got := SubscriptionReachFindings(types, ""); len(got) != 0 {
			t.Fatalf("отсутствие ответа трекера засчитано закрытой задачей: %v", got)
		}
	})

	t.Run("ссылка ЕСТЬ — молчит при ЛЮБОМ состоянии задачи", func(t *testing.T) {
		files := baseStand()
		files["proto/kacho/cloud/demo/v1/wiring.proto"] = reachProtoReferrer
		types, census := reachAudit(t, files)
		if census.Unreferenced != 0 {
			t.Fatalf("без ссылок осталось %d типов при взятой форме", census.Unreferenced)
		}
		for _, state := range []string{"CLOSED", "OPEN", ""} {
			if got := SubscriptionReachFindings(types, state); len(got) != 0 {
				t.Fatalf("взятая форма объявлена брошенной при состоянии %q: %v", state, got)
			}
		}
	})
}

// TestSubscriptionReachCountsOnlyRealReferrers — что считается ссылкой, а что
// нет. Каждая строка таблицы — отдельная ось, и у каждой свой законный близнец.
func TestSubscriptionReachCountsOnlyRealReferrers(t *testing.T) {
	t.Run("прод-код Go, употребляющий типы, — ССЫЛКА", func(t *testing.T) {
		files := baseStand()
		files["services/demo/server/serve.go"] = reachGoReferrer
		types, census := reachAudit(t, files)
		if census.Unreferenced != 0 {
			t.Fatalf("прод-употребление не засчитано ссылкой: без ссылок %d", census.Unreferenced)
		}
		if got := SubscriptionReachFindings(types, "CLOSED"); len(got) != 0 {
			t.Fatalf("взятая форма объявлена брошенной: %v", got)
		}
	})

	t.Run("пустой импорт в ПРОД-файле — НЕ ссылка", func(t *testing.T) {
		files := baseStand()
		files["services/demo/wiring/registry.go"] = reachGoBlankImport
		types, census := reachAudit(t, files)
		if census.BlankImports != 1 {
			t.Fatalf("пустых импортов насчитано %d, а стенд несёт один — "+
				"перепись обязана их ВИДЕТЬ, иначе их отсутствие в счёте неотличимо "+
				"от их отсутствия в дереве", census.BlankImports)
		}
		if census.Unreferenced != 3 {
			t.Fatalf("пустой импорт засчитан ссылкой: без ссылок %d из 3", census.Unreferenced)
		}
		if got := SubscriptionReachFindings(types, "CLOSED"); len(got) != 3 {
			t.Fatalf("гейт замолчал на пустом импорте — находок %d вместо трёх", len(got))
		}
	})

	t.Run("употребление в пробе — НЕ ссылка", func(t *testing.T) {
		files := baseStand()
		files["services/demo/server/serve_test.go"] = reachGoTestReferrer
		types, census := reachAudit(t, files)
		if census.TestReferrers != 3 {
			t.Fatalf("употреблений в пробах насчитано %d, а стенд несёт три", census.TestReferrers)
		}
		if census.Unreferenced != 3 {
			t.Fatalf("проба засчитана ссылкой: без ссылок %d из 3", census.Unreferenced)
		}
		if got := SubscriptionReachFindings(types, "CLOSED"); len(got) != 3 {
			t.Fatalf("гейт замолчал на пробе — находок %d вместо трёх", len(got))
		}
	})

	t.Run("сгенерённый стаб — НЕ ссылка", func(t *testing.T) {
		files := baseStand()
		files["pkg/api/kacho/cloud/subscription/subscription.pb.go"] = reachGoGeneratedStub
		types, census := reachAudit(t, files, "pkg")
		if census.Unreferenced != 3 {
			t.Fatalf("стаб засчитан ссылкой: без ссылок %d из 3", census.Unreferenced)
		}
		if got := SubscriptionReachFindings(types, "CLOSED"); len(got) != 3 {
			t.Fatalf("гейт замолчал на стабе — находок %d вместо трёх", len(got))
		}
	})

	t.Run("похожее имя — НЕ ссылка", func(t *testing.T) {
		files := baseStand()
		files["services/demo/neighbour/near.go"] = reachGoNearMissName
		types, census := reachAudit(t, files)
		if census.Unreferenced != 3 {
			t.Fatalf("`SubscriptionRequestV2` засчитан ссылкой на `SubscriptionRequest`: "+
				"без ссылок %d из 3", census.Unreferenced)
		}
		if got := SubscriptionReachFindings(types, "CLOSED"); len(got) != 3 {
			t.Fatalf("гейт замолчал на похожем имени — находок %d вместо трёх", len(got))
		}
	})
}

// TestSubscriptionReachRefusesAnEmptyWalk — премиса анализатора: пустой обход
// есть ОТКАЗ, а не чистота.
func TestSubscriptionReachRefusesAnEmptyWalk(t *testing.T) {
	t.Run("контрактов ноль", func(t *testing.T) {
		root := subscriptionStand(t, map[string]string{"proto/.keep": ""})
		if _, _, err := AuditSubscriptionFormReach(reachOptions(root), nil); err == nil {
			t.Fatal("обход без единого контракта прошёл молча — «ноль находок» " +
				"стало неотличимо от «ноль прочитанного»")
		}
	})

	t.Run("контракты есть, общего пакета нет", func(t *testing.T) {
		root := subscriptionStand(t, map[string]string{
			"proto/kacho/cloud/other/v1/thing.proto": reachFiller,
		})
		_, _, err := AuditSubscriptionFormReach(reachOptions(root), nil)
		if err == nil {
			t.Fatal("дерево без общего пакета прошло молча — дискриминатор мог " +
				"сломаться, и это было бы прочитано как чистота")
		}
		if !strings.Contains(err.Error(), SubscriptionCommonPackage) {
			t.Fatalf("отказ не называет предмета: %v", err)
		}
	})
}
