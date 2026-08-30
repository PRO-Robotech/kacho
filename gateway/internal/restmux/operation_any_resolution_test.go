// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"strings"
	"testing"

	operationv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/anypb"
)

// operation_any_resolution_test.go — край обязан ОТДАТЬ клиенту `Operation`,
// чей `response` владелец упаковал в `Any`. Проверяется ИСХОД (тело ответа), а
// не наличие импорта.
//
// ПРЕДМЕТ. `Operation.response` — это `Any`, и его отображение в JSON требует
// РАЗРЕШЕНИЯ типа по адресу: protojson идёт в реестр типов ПРОЦЕССА
// (`protoregistry.GlobalTypes`), а туда тип попадает только если пакет с ним
// ВЛИНКОВАН в бинарь. Владелец кладёт `google.protobuf.Empty` (единственная
// форма ответа у всякого удаления), у себя тип линкует и упаковывает успешно —
// упаковка реестра не спрашивает. Спрашивает его РАСПАКОВКА, и она происходит в
// ДРУГОМ процессе: на крае. Тип, которого нет в реестре края, роняет
// маршаллинг, grpc-gateway отвечает `HTTPError`, и вызывающий получает 500 на
// штатном пути.
//
// ПОЧЕМУ ПРОБА НЕ ИМПОРТИРУЕТ `emptypb`. Импорт зарегистрировал бы тип в
// ТЕСТОВОМ бинаре, и проба зеленела бы независимо от прод-кода — фикстура,
// снисходительнее продукта. Поэтому `Any` собирается здесь ВРУЧНУЮ: адрес
// типа строкой, тело — ноль байт (кодирование `google.protobuf.Empty` пусто
// by construction). Это ровно то, что приезжает по проводу от владельца.
//
// ГРАНИЦА. Проба судит реестр ТОГО пакета, что строит маршаллеры края. Она не
// утверждает, что тип линкует какой-то другой пакет бинаря: ровно такая
// косвенная связь и отказала — тип держался импортом снятого файла соседнего
// пакета, и его снятие вынесло разрешение вместе с собой, ничего не сломав на
// сборке.

// emptyTypeURL — адрес типа ответа всякого удаления. Записан СТРОКОЙ намеренно:
// ссылка на Go-тип втянула бы пакет в тестовый бинарь и обессмыслила пробу.
const emptyTypeURL = "type.googleapis.com/google.protobuf.Empty"

// operationWithPackedEmpty собирает то, что владелец кладёт на провод: готовую
// операцию удаления с непустым `response`.
func operationWithPackedEmpty() *operationv1.Operation {
	return &operationv1.Operation{
		Id:     "opsdeadbeefdeadbeef0",
		Done:   true,
		Result: &operationv1.Operation_Response{Response: &anypb.Any{TypeUrl: emptyTypeURL}},
	}
}

// TestEdgeRendersOperationResponsePackedByOwner — ИСХОД: оба боевых маршаллера
// края отдают тело, а не отказ.
func TestEdgeRendersOperationResponsePackedByOwner(t *testing.T) {
	marshalers := map[string]*strictEnumMarshaler{
		"public":   newStrictEnumMarshaler(newPublicJSONPb()),
		"internal": newStrictEnumMarshaler(newInternalJSONPb()),
	}
	for name, m := range marshalers {
		t.Run(name, func(t *testing.T) {
			body, err := m.Marshal(operationWithPackedEmpty())
			if err != nil {
				t.Fatalf("край не отдал операцию с упакованным владельцем ответом: %v\n"+
					"вызывающий получает 500 на штатном пути удаления", err)
			}
			if !strings.Contains(string(body), emptyTypeURL) {
				t.Fatalf("в теле нет адреса типа ответа %q; тело: %s", emptyTypeURL, body)
			}
		})
	}
}

// TestEdgeResolvesTypesOwnersPackIntoOperationResponse — реестр края обязан
// знать каждый адрес, который владельцы кладут в `Operation.response`.
//
// Печатает объём осмотренного: «ноль находок» обязано быть отличимо от «ноль
// прочитанного», а пустой перечень — от полного.
func TestEdgeResolvesTypesOwnersPackIntoOperationResponse(t *testing.T) {
	required := requiredOperationResponseTypeURLs()
	if len(required) == 0 {
		t.Fatal("перечень обязательных адресов ПУСТ — проверять нечего, гейт беспредметен")
	}
	for _, url := range required {
		if _, err := protoregistry.GlobalTypes.FindMessageByURL(url); err != nil {
			t.Errorf("реестр края не разрешает %q: %v\n"+
				"владелец такой ответ кладёт, край его отдать не сможет", url, err)
		}
	}
	t.Logf("перепись: адресов осмотрено %d, все обязаны разрешаться реестром края", len(required))
}

// TestOperationResponseResolutionPredicateAnswersBothWays — контроль в обе
// стороны: предикат обязан УМЕТЬ сказать «нет». Без него зелёное первой пробы
// не отличимо от предиката, отвечающего «да» на что угодно.
func TestOperationResponseResolutionPredicateAnswersBothWays(t *testing.T) {
	// Отрицательный: адреса не существует — реестр обязан отказать.
	const absent = "type.googleapis.com/kacho.cloud.notatype.v1.NoSuchMessage"
	if _, err := protoregistry.GlobalTypes.FindMessageByURL(absent); err == nil {
		t.Fatalf("реестр разрешил несуществующий адрес %q — предикат ничего не измеряет", absent)
	}
	// Положительный: тип, который край линкует, обслуживая свои маршруты.
	const present = "type.googleapis.com/kacho.cloud.operation.Operation"
	if _, err := protoregistry.GlobalTypes.FindMessageByURL(present); err != nil {
		t.Fatalf("реестр не разрешил заведомо влинкованный %q: %v — проба красна не по предмету", present, err)
	}
}

// TestIntentAndAnchorsAreTheSameSet — намерение и механизм обязаны совпадать
// КАК МНОЖЕСТВА, и проверяется это сравнением перечней, а не разрешимостью.
//
// ПРЕДПОСЫЛКА ГЕЙТА, названная вслух и ИЗМЕРЕННАЯ. Разрешимость адреса в
// реестре НЕ доказывает, что его вносит НАШ якорь: реестр процесса наполняют и
// посторонние пакеты, влинкованные по своим причинам. Померено инъекцией:
// адрес `google.protobuf.Duration`, внесённый в намерение БЕЗ якоря,
// разрешается — его линкует чужая зависимость, — и проверка «намерение
// разрешимо» осталась зелёной. Именно такая косвенная связь и отказала в
// исходном дефекте, поэтому здесь сверяются ПЕРЕЧНИ: они расходятся ровно
// тогда, когда одну половину правят без другой.
//
// Ловятся обе ошибки: намерение без якоря (край обещает то, что держится чужой
// случайностью) и якорь без намерения (мёртвый импорт, который снимут как
// непонятный — и разрешение уедет с ним ровно так, как уже уезжало).
func TestIntentAndAnchorsAreTheSameSet(t *testing.T) {
	required := map[string]bool{}
	for _, u := range requiredOperationResponseTypeURLs() {
		required[u] = true
	}
	anchored := map[string]bool{}
	for _, u := range anchoredTypeURLs() {
		anchored[u] = true
	}
	if len(required) == 0 || len(anchored) == 0 {
		t.Fatalf("перечень пуст — сверять нечего: намерений %d, якорей %d", len(required), len(anchored))
	}
	for u := range required {
		if !anchored[u] {
			t.Errorf("намерение %q не обеспечено якорем: край обещает отдать тип, "+
				"чьё разрешение здесь не объявлено", u)
		}
	}
	for u := range anchored {
		if !required[u] {
			t.Errorf("якорь %q не отвечает ни одному объявленному намерению: "+
				"либо внеси его в перечень, либо сними якорь", u)
		}
	}
	t.Logf("перепись: намерений %d, якорей %d", len(required), len(anchored))
}
