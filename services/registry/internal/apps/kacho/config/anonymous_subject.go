// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"strings"

	"github.com/PRO-Robotech/corelib/ids"
)

// AnonymousSubject — объявленный анонимный субъект плоскости данных
// (`KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID`).
//
// Пустое + nil — анонимный поток выключен (умолчание). Ошибка — значение имеет
// форму идентификатора, который порождает сам продукт, и тогда исход один —
// отказ в старте: `sub` анонимного токена, совпавший с настоящим принципалом,
// отдал бы его доступ всякому без удостоверения.
//
// ЧТО ЗДЕСЬ СУДИТСЯ, А ЧТО НЕТ. Форма порождённого идентификатора берётся у
// производителя идентификаторов (`corelib/ids`): префиксы и тело — его, второй
// записи формы здесь нет. Полную закрытую форму имени (слова через дефис, до 63
// знаков) судит служба доступа при своём старте на том же значении: значение у
// двух сторон одно — `global.kacho.registry.anonymousSubjectId`, — и рендер
// отказывает, если стороны объявлены по-разному. Одиночная установка чарта
// реестра с именем незакрытой формы этот страж проходит — остаток с предметом
// PRO-Robotech/kaname#908 (форма в публичный пакет службы доступа); снимается
// тем изменением, что поднимет пин на ревизию с этим пакетом (решение
// диспетчера В2 по приёмке REG-PUB-1).
func (c Config) AnonymousSubject() (string, error) {
	v := c.AnonymousSubjectID
	if v == "" {
		return "", nil
	}
	if generatedIDShape(v) {
		return "", fmt.Errorf(
			"KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID %q has the shape of an identifier the platform "+
				"generates — it is the subject of the read-only token minted for a caller WITHOUT "+
				"credentials, and a value that can coincide with a real principal would hand that "+
				"principal's access to anyone. Declare a name such as registry-anonymous (the same "+
				"value as the access service's api-server.registry-token.anonymous-client-id), or "+
				"leave it empty to keep anonymous pull OFF", v)
	}
	return v, nil
}

// generatedIDShape — значение имеет одну из двух форм идентификатора канона:
// слитную (`<префикс><тело>`) либо через дефис (`<префикс>-<тело>`).
func generatedIDShape(v string) bool {
	if ids.HasKnownPrefix(v) {
		return true
	}
	prefix, body, ok := strings.Cut(v, "-")
	if !ok {
		return false
	}
	if _, known := ids.KnownHyphenPrefixes()[prefix]; !known {
		return false
	}
	return canonBody(body)
}

// canonBody — тело идентификатора канона: длина и алфавит судятся
// производителем, а не переписываются здесь. Тело прикладывается к известному
// слитному префиксу, и вопрос задаётся тому же `ids.IsValid`, которым продукт
// проверяет собственные идентификаторы.
func canonBody(body string) bool {
	return ids.IsValid(ids.PrefixRegistry+body, ids.PrefixRegistry)
}
