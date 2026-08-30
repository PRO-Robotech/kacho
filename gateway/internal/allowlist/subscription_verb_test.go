// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package allowlist

import "testing"

// subscription_verb_test.go — глагол подписки НЕ маршрутизируется наружу.
//
// Край стал потребителем этого метода (проекция потока в браузер, kacho#1020), и
// именно поэтому утверждение записано отдельно: потребитель у метода появился, а
// внешнего пути у него нет и не заводится. Два состояния легко спутать, и
// спутать их можно ровно один раз.

const subscribeMethod = "/kacho.cloud.subscription.InternalSubscriptionService/Subscribe"

// TestSubscriptionVerbIsNotExternallyRoutable — оба независимых механизма
// отсекают метод, и проверяются они ОБА.
//
// Одного мало by construction: список ловит «не перечислен», признак имени ловит
// «перечислен по ошибке». Дыра появляется, когда держит только один, — и тогда
// снятие второго выглядит уборкой.
func TestSubscriptionVerbIsNotExternallyRoutable(t *testing.T) {
	if IsAllowed(subscribeMethod) {
		t.Errorf("%s стоит в перечне внешне маршрутизируемых методов (запрет #6)", subscribeMethod)
	}
	if !HasInternalSuffix(subscribeMethod) {
		t.Errorf("%s не распознан как Internal — тогда попади он в перечень, второго "+
			"механизма, который его отсечёт, не осталось бы", subscribeMethod)
	}

	// Положительный контроль: и перечень, и признак имени работают на соседях.
	// Без него оба отрицания зеленели бы на функциях, отвечающих «нет» на всё.
	const publicMethod = "/kacho.cloud.vpc.v1.NetworkService/Get"
	if !IsAllowed(publicMethod) {
		t.Errorf("положительный контроль: %s обязан быть разрешён", publicMethod)
	}
	if HasInternalSuffix(publicMethod) {
		t.Errorf("положительный контроль: %s не Internal", publicMethod)
	}
}
