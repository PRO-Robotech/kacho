// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package reconciler_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/ids"
)

// fixtureInitiatorDSN — DSN пула ФИКСТУРЫ, на каждом соединении которого
// инициатор журнала выставлен параметром старта сессии.
//
// Колонка `initiator` журнала модуля берёт значение из настройки транзакции и
// без неё строку не принимает (NTF-3, З2). Фикстура пробы пишет строки журнала
// и журналируемых таблиц своими операторами, минуя писателей модуля, — она
// подменяет собою писателя, а не проверяет его; инициатор писателей модуля
// здесь не предмет (его держат NTF3-57, NTF3-58, NTF3-62 и гейт УК3-27).
// Сессионная установка законна ТОЛЬКО в тестовом дереве (правило (г) гейта
// УК3-27).
//
// Тем же параметром старта выставлен флаг ленты `kacho_feed.enabled=false`:
// функция базы `resource-event` на журнале модуля (NTF-3 З10) отвергает
// транзакцию записи журнала без флага, а лента фикстуре не предмет.
func fixtureInitiatorDSN(t testing.TB, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSN фикстуры не разобрался: %v", err)
	}
	q := u.Query()
	q.Set("options", strings.TrimSpace(q.Get("options")+" -c kacho_journal.initiator=user:"+ids.NewHyphenID(ids.PrefixUser)+" -c kacho_feed.enabled=false"))
	u.RawQuery = q.Encode()
	return u.String()
}
