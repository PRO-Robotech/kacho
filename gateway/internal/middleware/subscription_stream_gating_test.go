// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// subscription_stream_gating_test.go — гейт запрета #6 для проекции потока.
//
// # Что здесь утверждается
//
// Край выставил СВОЮ ручку, дозванивающуюся до `Internal`-метода владельца.
// Отличие этого от ПУБЛИКАЦИИ метода наружу проверяемо, а не декларативно, и
// проверяется тремя утверждениями сразу: у метода нет внешнего пути, ручка края
// гейтится настоящим правом, и это право не подменено пропуском.
//
// # Почему все три вместе
//
// Каждое по отдельности защитимо и ни одно не достаточно. Метод без внешнего
// пути, но ручка без права — открытая поверхность под видом закрытой. Ручка с
// правом, но метод с внешним путём — обход через второй адрес. Право, объявленное
// пропуском, — форма проверки без содержания.

// TestContractDeclaresNoExternalPathForTheSubscriptionVerb — контракт не
// объявляет глаголу подписки ни одного HTTP-пути.
//
// Объяви он его — `runtime.ServeMux` построил бы маршрут, и метод стал бы
// доступен снаружи как всякий публичный. Именно этим отвергнут кандидат
// «chunked JSON от grpc-gateway»: он требует такого объявления by construction.
func TestContractDeclaresNoExternalPathForTheSubscriptionVerb(t *testing.T) {
	offenders := make([]string, 0, 1)
	for _, route := range generatedRestRoutes {
		if strings.Contains(route.FQN, "InternalSubscriptionService") {
			offenders = append(offenders, route.Method+" "+route.Template+" → "+route.FQN)
		}
	}
	t.Logf("перепись: маршрутов контракта осмотрено %d · собственных ручек края %d",
		len(generatedRestRoutes), len(edgeRestRoutes))
	if len(generatedRestRoutes) == 0 {
		t.Fatal("таблица маршрутов контракта пуста — гейт ничего не читал")
	}
	if len(offenders) > 0 {
		t.Errorf("контракт объявил глаголу подписки внешний путь: %v. Тогда его строит "+
			"grpc-gateway, и Internal-метод оказывается на внешнем слушателе (запрет #6)", offenders)
	}
}

// TestSubscriptionStreamPathResolvesToTheVerbItPerforms — путь ручки резолвится
// в ИМЯ ТОГО ГЛАГОЛА, который она исполняет от имени вызывающего.
//
// Промах мимо каталога — отказ (fail-closed), поэтому без этой строки ручка была
// бы не «открытой», а мёртвой. Имя настоящее, а не выдуманное для края: вторая
// запись каталога объявляла бы право, которого не спрашивает ни один вызов, и
// разошлась бы с настоящей молча.
func TestSubscriptionStreamPathResolvesToTheVerbItPerforms(t *testing.T) {
	fqn, ok := NewRestRouter().Resolve("GET", subscriptionstream.Path)
	if !ok {
		t.Fatalf("путь %s не резолвится в имя метода — промах мимо каталога есть ОТКАЗ, "+
			"то есть ручка была бы мёртвой, а не открытой", subscriptionstream.Path)
	}
	if fqn != subscriptionstream.MethodFQN {
		t.Errorf("путь резолвится в %q, а исполняет %q", fqn, subscriptionstream.MethodFQN)
	}
}

// TestSubscriptionStreamIsGatedByItsCatalogEntry — ручка гейтится записью
// каталога, а не самодельной проверкой.
//
// `scope_filtered` означает: край пообъектного вопроса не задаёт (единого
// объекта у потока нет — он несёт события разных предметов), но ТРЕБУЕТ
// названного принципала и применяет пол уровня подтверждения. Дальше сужает
// владелец, на каждой отдаваемой строке.
func TestSubscriptionStreamIsGatedByItsCatalogEntry(t *testing.T) {
	catalog := NewPermissionCatalog()
	if err := catalog.LoadFromBytes(EmbeddedPermissionCatalogJSON()); err != nil {
		t.Fatalf("чтение встроенного каталога: %v", err)
	}
	t.Logf("перепись: записей каталога %d", catalog.Size())
	if catalog.Size() == 0 {
		t.Fatal("каталог пуст — гейт ничего не читал")
	}

	entry, ok := catalog.Lookup(subscriptionstream.MethodFQN)
	if !ok {
		t.Fatalf("записи каталога для %q нет — промах есть отказ, ручка мертва",
			subscriptionstream.MethodFQN)
	}
	if entry.Permission == "" {
		t.Error("запись не называет права")
	}
	if entry.IsExempt() {
		t.Error("право объявлено пропуском: пропуск дополнительно пускает БЕЗ принципала " +
			"по сетевой позиции, а под безымянным вызывающим владелец сузил бы поток по правам КРАЯ")
	}
	if !entry.ScopeFiltered {
		t.Error("запись не объявлена scope_filtered: тогда край задал бы пообъектный вопрос " +
			"о потоке, у которого единого объекта нет вовсе")
	}
	if entry.RequiredACRMin == "" {
		t.Error("запись не называет пола уровня подтверждения")
	}
}

// TestSubscriptionStreamIsNotOnThePreAuthAllowList — путь не освобождён от
// полосы прав.
//
// Список до-аутентификационных путей пускает БЕЗ вопроса о правах и без вопроса
// об отзыве. Попади ручка туда — она отвечала бы кому угодно, и все три
// утверждения выше стали бы бессодержательными.
func TestSubscriptionStreamIsNotOnThePreAuthAllowList(t *testing.T) {
	if isPublicHTTPPath(subscriptionstream.Path) {
		t.Errorf("%s стоит в списке до-аутентификационных путей — тогда ни право, "+
			"ни отзыв у него не спрашиваются вовсе", subscriptionstream.Path)
	}
	// Положительный контроль: список не пуст и работает. Без него отрицание
	// зеленело бы на предикате, отвечающем «нет» на всё.
	if !isPublicHTTPPath("/healthz") {
		t.Error("положительный контроль: /healthz обязан быть в списке")
	}
}
