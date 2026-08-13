// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// loadUsedBy заполняет «кем используется» у прочитанных групп — ОДНИМ запросом
// на весь набор, а не по запросу на строку.
//
// Поле объявлено в контракте как output-only, выводимое на чтении, и до этой
// правки у него НЕ БЫЛО производителя вовсе: сервер его не заполнял, а карточка
// показывала прочерк при живых потребителях — то есть утверждала о ресурсе
// неправду. Отношение при этом уже выражено базой (набор групп на интерфейсе и
// группа по умолчанию у сети), поэтому обратная ссылка — запрос, а не новая
// таблица.
//
// # Почему отказ здесь НЕ мягкий, в отличие от одноимённого поля адреса
//
// У адреса рядом с `used_by` едет независимый признак занятости (`used`):
// пустой список при `used=true` читается как «подробностей нет», и мягкий проход
// там защитим. У группы правил такого спутника НЕТ — пустой список означает
// ровно «потребителей нет». Проглотить здесь ошибку чтения значило бы вернуть
// то самое утверждение, ради снятия которого поле и заводится, только теперь с
// производителем, который иногда молчит. Поэтому ошибка едет наверх и запрос
// отвечает отказом.
//
// Вызывающий обязан звать это ТОЛЬКО на путях чтения (Get / List) и только для
// строк, которые действительно уедут в ответ.
func loadUsedBy(ctx context.Context, reader SecurityGroupReaderIface, recs []*kacho.SecurityGroupRecord) error {
	if len(recs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(recs))
	for _, rec := range recs {
		if rec != nil {
			ids = append(ids, rec.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	refs, err := reader.ReferrersFor(ctx, ids)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec == nil {
			continue
		}
		rec.UsedBy = refs[rec.ID]
	}
	return nil
}
