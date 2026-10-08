// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// doc.go — объявление пакета-владельца каталога шаблонов `notifications/`
// модуля compute. Генератор постановки (`notifygen`, corelib) кладёт порождённые
// `notifications_<имя>.gen.go` в пакет каталога, которому принадлежит
// `notifications/`, и берёт имя пакета из его не-тестовых файлов: этот файл
// держит объявление пакета.
//
// Шаблон `resource-event` — форма `fanout` (NTF-3 Р3, З10): строку ленты
// на каждую строку журнала модуля ставит функция базы, которую выпускает
// `notifygen init -journal journal.yaml` в цепочку миграций модуля. Порождённая
// Go-половина шаблона — описание, с которым `notifygen -check` сверяет
// атрибуты SQL-половины.
package compute
