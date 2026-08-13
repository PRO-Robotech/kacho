// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cephrbd

import (
	"strings"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
)

// classify переводит ответ инструмента в полосу исхода.
//
// # Почему по ТЕКСТУ, и почему это не хрупкость
//
// Инструмент возвращает один и тот же код выхода на разные причины, поэтому различить
// «объекта нет» и «места нет» можно только по сообщению. Признаки ниже — это имена
// системных ошибок, а не проза: они приходят из одного набора и не переписываются с
// косметическими правками. Список закрыт, и неопознанный ответ даёт
// [blockbackend.OutcomeUnclassified] — состояние, а не догадку: он терминален и
// наружу выходит фиксированной внутренней ошибкой.
//
// # Почему неопознанное НЕ считается временным
//
// Соблазн трактовать незнакомый отказ как временный велик — «вдруг пройдёт». Но
// повтор того, чего мы не поняли, либо не пройдёт никогда и вечно держит голову
// очереди, либо пройдёт по причине, которой мы не знаем. Первый исход в этом продукте
// уже случался и стоил очереди, где за всю её жизнь не доехало ни одной строки.
func classify(stderr []byte, exitCode int) blockbackend.Outcome {
	if exitCode == 0 {
		return blockbackend.OutcomeUnclassified // вызывающий сюда не заходит
	}
	msg := strings.ToLower(string(stderr))

	switch {
	// Объекта нет. Для удаления это успех — решает вызывающий, не классификатор.
	case contains(msg, "no such file or directory", "does not exist", "(2) no such"):
		return blockbackend.OutcomeNotFound

	// Объект уже есть либо занят другим состоянием.
	case contains(msg, "file exists", "(17) file exists", "already exists"):
		return blockbackend.OutcomeConflict

	// Место кончилось. Отдельная полоса: арендатору нечего чинить в запросе.
	case contains(msg, "no space left on device", "(28) no space", "quota exceeded",
		"full", "osd full"):
		return blockbackend.OutcomeCapacityExhausted

	// Права. НЕ временно: повтор идентичного запроса не пройдёт.
	case contains(msg, "operation not permitted", "(1) operation not permitted",
		"permission denied", "(13) permission denied", "auth", "unauthorized"):
		return blockbackend.OutcomeDenied

	// Кластер не отвечает либо деградировал. Единственная повторяемая полоса.
	case contains(msg, "connection timed out", "(110) connection timed out",
		"connection refused", "unable to connect", "timed out", "no route to host",
		"monitor", "osd_map", "clock skew"):
		return blockbackend.OutcomeUnavailable

	// Мы обращаемся не туда либо не тем: конфигурация, а не сбой. Громко.
	case contains(msg, "unrecognized command", "unknown option", "invalid command",
		"error parsing", "missing required", "unable to read conf",
		"keyring not found", "did not load config file"):
		return blockbackend.OutcomeMisconfigured

	// Объект есть, но состояние не позволяет: занят наблюдателем, есть зависимые
	// клоны, снимок защищён.
	case contains(msg, "device or resource busy", "(16) device or resource busy",
		"image has snapshots", "snapshot is protected", "linked clones",
		"image still has watchers"):
		return blockbackend.OutcomeRejected
	}
	return blockbackend.OutcomeUnclassified
}

// contains — присутствует ли хоть один признак.
func contains(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
