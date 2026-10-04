// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"strings"

	"github.com/PRO-Robotech/corelib/auth"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/operations"
)

// verifiedPrincipal — принципал записи data-plane из `sub` ПРОВЕРЕННОГО токена
// реестра (решение Д115). Его кладёт в контекст запроса ServeHTTP, и из него
// помощник записи журнала (`journaltx.Begin`) берёт инициатора транзакции
// намерения репозитория.
//
// Принципала нет (false), когда `sub` не проверен никем (режим без
// проверяющего — `bootstrap`), когда это анонимный субъект (он ничего не пишет и
// личностью не является) и когда `sub` не переводится в инициатора
// (`auth.InitiatorOf` — единственное место о форме субъекта). Подстановки нет:
// отсутствие принципала на пути записи — отказ, а не «system».
//
// Тип субъекта выводится по приставке id — у data-plane есть только `sub`, без
// типа принципала; приставки пользователя и сервисного аккаунта берутся из
// каталога `corelib/ids`, тем же правилом выводит FGA-субъект
// `domain.FGASubjectFromID`.
func (h *Handler) verifiedPrincipal(sub string) (operations.Principal, bool) {
	if h.verifier == nil || sub == "" {
		return operations.Principal{}, false
	}
	if h.anonSubjectID != "" && sub == h.anonSubjectID {
		return operations.Principal{}, false
	}
	var p operations.Principal
	switch {
	case strings.HasPrefix(sub, ids.PrefixUser):
		p = operations.Principal{Type: "user", ID: sub}
	case strings.HasPrefix(sub, ids.PrefixServiceAccount):
		p = operations.Principal{Type: "service_account", ID: sub}
	default:
		return operations.Principal{}, false
	}
	if _, err := auth.InitiatorOf(p); err != nil {
		return operations.Principal{}, false
	}
	return p, true
}
