// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// critical_backends_validation.go — СОЕДИНЕНИЕ, БЕЗ КОТОРОГО КРАЙ НЕ РАБОТАЕТ,
// ОТКАЗЫВАЕТ В СТАРТЕ, А НЕ ПРОПУСКАЕТ ПРОВЯЗКУ.
//
// # Предмет
//
// Корень провязывал читателей отзыва под условием «а вдруг соединения нет» и
// прозой рядом объявлял, что такой ветки здесь не заводится. Пока рядом стоял
// второй читатель (чужого поставщика), ветка только УЛУЧШАЛА композицию. Он
// снят — и ложная ветвь стала оставлять путь запроса БЕЗ читателя отзыва
// вовсе, молча: `else` у неё не было, а прежний доклад ушёл тем же изменением.
//
// # Почему отказ СТАРТА
//
// Служба прав фронтит и личность, и права: без неё край не обслуживает ни
// одного запроса ни при какой посадке. Тихий пропуск провязки означает «край
// поднялся готовым, отзыв не исполняется» — состояние, которое не сходится
// само и о котором никто не узнает. Отказ старта виден оператору; паника на
// пути запроса видна арендатору.
//
// # Почему это страж, а не снятое условие
//
// Снять `!= nil` и провязать безусловно значило бы построить читатель на
// пустом соединении. Условие остаётся — но ОДНО, в одном месте, до провязок, и
// его исход есть отказ, а не пропуск.
package main

import (
	"fmt"
	"sort"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// criticalBackendKeys — соединения, без которых край не обслуживает НИ ОДНОГО
// запроса: служба прав фронтит и личность, и права.
//
// Объявление ОДНО: тот же набор судит готовность реплики
// (`health.HTTPReadyz`). Вторая копия разошлась бы с первой молча — страж
// отказывал бы в старте по одному списку, а из ротации реплику выводил бы
// другой.
func criticalBackendKeys() map[string]bool {
	return map[string]bool{"iam": true, "iamInternal": true}
}

// validateCriticalBackends отказывает в старте, пока критическое соединение не
// открыто.
func validateCriticalBackends(backends proxy.Backends) error {
	var absent []string
	for key := range criticalBackendKeys() {
		if conn := backends[key]; conn == nil {
			absent = append(absent, key)
		}
	}
	if len(absent) == 0 {
		return nil
	}
	sort.Strings(absent)
	return fmt.Errorf(
		"critical backend connection(s) absent: %v — the identity service fronts both "+
			"who the caller is and what they may do, so the edge serves no request without "+
			"it. Wiring that depends on the connection would otherwise be skipped silently: "+
			"the edge would come up ready with the revocation reader off the request path, "+
			"and nothing would say so (refuse to start)",
		absent)
}
