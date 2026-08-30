// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"net/netip"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// Текст отказа и его машинный признак живут ОДНИМ производителем —
// `serviceerr.ReservedCIDROverlap`. Второй копии здесь нет намеренно: тон
// сообщения есть контракт, и разойдясь с производителем, копия разошлась бы
// молча — ровно там, где деталь читают машиной, а не глазом. Почему у этого
// отказа свой признак и чего он клиенту НЕ даёт — в godoc производителя.

// validateNotReserved — ни один объявляемый диапазон подсети не пересекается с
// адресным пространством, которое платформа держит за собой.
//
// # Где эта проверка стоит и почему именно там
//
// На пути ЗАПРОСА, синхронно, до создания операции и до вызова к владельцу
// Geography: ввод, пересекающийся со служебным диапазоном, не станет законным ни
// при каком ответе соседа, поэтому платить за него сетевым вызовом нечем. Локальные
// проверки формата стоят РАНЬШЕ (их предпосылку — разбираемое каноническое
// значение — эта проверка использует), а перекрёстные требования — позже.
//
// # Что делает НЕобъявленный перечень
//
// Ничего: пустое множество не пересекается ни с чем, и каждый ввод проходит. Это
// названо прямо и здесь, и в доке доменного типа — «не сужаем», а не «нечего
// сужать». Закрывает это страж старта (`config.Config.ValidateReservedPrefixes`):
// боевая посадка с необъявленным перечнем не поднимается. Раннего выхода по
// перечню здесь нет и заводить его нельзя — он сделал бы проверку
// тождественно-истинной ровно тем же способом, только незаметнее.
//
// Неразбираемое значение отвергается, а не пропускается: формат проверен выше по
// стеку, но проверка, молча пропускающая то, чего не поняла, перестала бы отвечать
// за свой предмет при первой же перестановке шагов. Тот же приём — у
// `checkSubnetCIDROverlap`.
func validateNotReserved(slotOf func(int) string, reserved domain.ReservedPrefixes, cidrs []string) error {
	for i, c := range cidrs {
		slot := slotOf(i)
		prefix, err := netip.ParsePrefix(c)
		if err != nil {
			return serviceerr.InvalidArg(slot, slot+" must be a valid CIDR (e.g. 10.0.0.0/24)")
		}
		if reserved.Overlaps(prefix) {
			return serviceerr.ReservedCIDROverlap(slot, c)
		}
	}
	return nil
}

// validateSubnetNotReserved — обе семьи одним вызовом: у глаголов, объявляющих
// диапазоны подсети (`Create`, `:addCidrBlocks`), предмет один, и разное число
// проверок у них означало бы, что одну из семей где-то забыли.
func validateSubnetNotReserved(fields cidrFields, reserved domain.ReservedPrefixes, v4, v6 []string) error {
	if err := validateNotReserved(fields.V4Slot, reserved, v4); err != nil {
		return err
	}
	return validateNotReserved(fields.V6Slot, reserved, v6)
}
