// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

// verdict_cache.go — кеш ПОЛОЖИТЕЛЬНЫХ вердиктов на ПРЯМОМ пути опроса прав.
//
// Зачем. Списочные фильтры реестра спрашивают хранилище прав про КАЖДЫЙ элемент
// страницы, а страница контрактно бывает до тысячи. У gRPC-интерсептора такой кеш
// есть с самого начала (и своя ручка TTL), но прямые вызовы фильтра его не проходят
// — они держат сырого клиента. Соседний сервис ровно этот цикл кешем и схлопывает.
//
// Почему кешируются только «разрешено». Отрицательный вердикт кешировать нельзя:
// свежая выдача материализуется асинхронно, и закешированный отказ продлил бы окно,
// в котором владелец не видит собственный ресурс. Положительный вердикт, наоборот,
// продлевает окно ОТЗЫВА — поэтому его срок берётся из той же ручки, которой
// оператор уже управляет окном отзыва у интерсептора: у процесса одно, а не два
// расходящихся окна.
//
// Ошибка хранилища прав НЕ кешируется и НЕ превращается в «разрешено» — она
// возвращается вызывающему, который отвечает fail-closed.

import (
	"context"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// cachedAuthorizer — Authorizer с TTL-кешем положительных вердиктов.
type cachedAuthorizer struct {
	inner Authorizer
	cache *authz.Cache
}

// newCachedAuthorizer оборачивает az кешем положительных вердиктов с временем
// жизни ttl. ttl <= 0 либо az == nil → обёртки нет (вызывающий получает исходный
// порт как есть): «выключено» здесь означает отсутствие кеша, а не кеш с нулевым
// сроком, чтобы у выключенного состояния не было собственного поведения.
func newCachedAuthorizer(az Authorizer, ttl time.Duration) Authorizer {
	if az == nil || ttl <= 0 {
		return az
	}
	return &cachedAuthorizer{inner: az, cache: authz.NewCache(ttl)}
}

// Check отвечает из кеша, если ровно этот вердикт (subject, relation, object) уже
// был получен как ПОЛОЖИТЕЛЬНЫЙ и не истёк; иначе спрашивает хранилище прав.
func (c *cachedAuthorizer) Check(ctx context.Context, subject, relation, object string) (bool, error) {
	objType, objID, ok := strings.Cut(object, ":")
	if !ok {
		// Не наша форма объекта — не кешируем и ничего не решаем сами.
		return c.inner.Check(ctx, subject, relation, object)
	}
	if allowed, hit := c.cache.Get(subject, relation, objType, objID); hit && allowed {
		return true, nil
	}
	allowed, err := c.inner.Check(ctx, subject, relation, object)
	if err != nil {
		return false, err // отказ хранилища не кешируется и не становится «разрешено»
	}
	if allowed {
		c.cache.SetAllowed(subject, relation, objType, objID)
	}
	return allowed, nil
}

// CheckMany отвечает из кеша по тем объектам, чей ПОЛОЖИТЕЛЬНЫЙ вердикт уже получен
// и не истёк, а про остальные задаёт ОДИН пакетный вопрос.
//
// Промах по кешу стоит элемент партии, а не отдельный запрос — в этом и разница с
// поштучным опросом, который здесь стоял. Отрицательный вердикт не кешируется
// никогда: иначе свежая выдача не была бы видна до истечения окна.
//
// Порядок входа сохраняется: страница уже упорядочена, и переупорядочивание сломало
// бы пагинацию.
func (c *cachedAuthorizer) CheckMany(
	ctx context.Context, subject, relation, objectType string, objectIDs []string,
) ([]string, error) {
	cached := make(map[string]bool, len(objectIDs))
	pending := make([]string, 0, len(objectIDs))
	seen := make(map[string]struct{}, len(objectIDs))
	for _, id := range objectIDs {
		if _, dup := seen[id]; dup {
			continue // одна страница не платит дважды за повтор
		}
		seen[id] = struct{}{}
		if allowed, hit := c.cache.Get(subject, relation, objectType, id); hit && allowed {
			cached[id] = true
			continue
		}
		pending = append(pending, id)
	}

	if len(pending) > 0 {
		fresh, err := c.inner.CheckMany(ctx, subject, relation, objectType, pending)
		if err != nil {
			return nil, err // отказ хранилища не кешируется и не становится «разрешено»
		}
		for _, id := range fresh {
			cached[id] = true
			c.cache.SetAllowed(subject, relation, objectType, id)
		}
	}

	out := make([]string, 0, len(cached))
	for _, id := range objectIDs {
		if cached[id] {
			out = append(out, id)
			delete(cached, id) // повтор во входе не удваивает выход
		}
	}
	return out, nil
}
