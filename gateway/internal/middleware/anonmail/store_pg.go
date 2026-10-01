// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import "time"

// Пределы звена (замысел З8, §8 «Константы кода»). Каждое число — одно место;
// второго литерала этих значений в пакете нет (УК53, гейт
// TestDecisionLimitLiteralsLiveOnlyInTheirDeclaration).
const (
	// anonMailStoreWait — предел ОДНОГО ожидания хранилища: блокировки
	// (`SET LOCAL lock_timeout`) и захвата соединения пула (срок контекста на
	// `pool.Acquire`) — одно число. Решение ключа — единицы операторов к базе,
	// и ожидание дольше означает очередь, а не конкуренцию.
	anonMailStoreWait = 250 * time.Millisecond
	// anonMailDecisionWaits — число ограниченных ожиданий на самом длинном
	// пути решения: захват соединения, три ключа IPv6 (/64, /56, /48), вставка
	// пометки, ждущая соседнюю незафиксированную вставку того же вызова, строка
	// ведра. У IPv4 их пять.
	anonMailDecisionWaits = 6
	// anonMailDecisionBudget — срок решения: захват соединения и все операторы
	// транзакции до COMMIT; тот же предел — `statement_timeout` и
	// `idle_in_transaction_session_timeout` сервера (decisionLimitsSQL).
	anonMailDecisionBudget = anonMailDecisionWaits * anonMailStoreWait
	// anonMailCommitWait — предел COMMIT решения, на контексте без отмены.
	anonMailCommitWait = anonMailStoreWait
	// anonMailCancelGrace — запасной дедлайн обработчика отмены пула
	// ограничителя: ответ на запрос отмены не пришёл за этот срок — сокет
	// получает дедлайн, соединение закрывается клиентом (З8 (3а)).
	anonMailCancelGrace = anonMailStoreWait
	// anonMailResolveBudget — срок разрешения исхода фиксации: исходная
	// транзакция при любом состоянии держателя кончается на сервере не позже
	// двух сроков решения от прихода её последнего оператора (И37).
	anonMailResolveBudget = 2 * anonMailDecisionBudget
	// anonMailPoolConns — соединений пула ограничителя на реплику: все решения
	// флота проходят строку ведра по одному, и соединений больше, чем «одно
	// держит ведро, три готовят свои блокировки», пропускной способности не
	// прибавляют.
	anonMailPoolConns = 4
	// anonMailConnLifetime и anonMailConnLifetimeJitter — срок жизни соединений
	// пула и его разброс (УК52 (2)): при разбросе 0 соединения одного возраста
	// истекали бы разом, и новое строилось бы внутри срока захвата.
	anonMailConnLifetime       = time.Hour
	anonMailConnLifetimeJitter = 10 * time.Minute
)
