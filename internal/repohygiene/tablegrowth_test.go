// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tablegrowth_test.go — У ЖИВОЙ ТАБЛИЦЫ НАЗВАН МЕХАНИЗМ ОГРАНИЧЕНИЯ РОСТА
// (задача #1356; вторая половина предиката задачи #1292).
//
// # Что этот гейт держит, чего не держит соседний
//
// `TestDeclaredRetentionSweepersHaveAProductionCaller` требует прод-вызывающего
// у ОБЪЯВЛЕННОГО уборщика. Таблица, у которой уборщика нет ВОВСЕ, ему невидима
// by construction: он ищет функцию, чьё тело удаляет строки со сравнением
// колонки-времени, а такой функции не существует. Здесь обходятся ТАБЛИЦЫ, а не
// функции, и перечень их выводится из применённых миграций.
//
// # Три исхода, четвёртого нет
//
//	уборка   — есть оператор снятия строк в прод-коде либо в теле триггера;
//	предел   — есть каскадный внешний ключ на самой таблице: строка умирает
//	           вместе с родителем;
//	объявлен — запись реестра `tableGrowthRegistry` с темпом, вердиктом и
//	           причиной.
//
// Ни одного — находка. «Ровно один» держится тем, что запись реестра у таблицы,
// получившей уборку или каскад, объявляется ПОТЕРЯВШЕЙ ПРЕДМЕТ: послабление,
// которое не истекает само, унаследует следующая слепая зона.
//
// # Про «внешний темп» — почему гейт его не выводит
//
// Решение и его причина — в шапке `tablegrowth.go`, §«ЧЕМ ГЕЙТ РАЗЛИЧАЕТ
// „ВНЕШНИЙ ТЕМП“». Коротко: темп есть свойство ВЫЗЫВАЮЩЕГО, которого в дереве
// нет, и все три машинных признака, которые просятся, проверены и оказались
// ложными на конкретных таблицах. Гейт выводит НАКОПЛЕНИЕ (структурное
// свойство) и требует, чтобы темп был ОБЪЯВЛЕН записью; здесь он не
// пересказывается, чтобы два места об одном предмете не разошлись.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// tableGrowthRoots — где ищутся миграции и операторы снятия строк.
var tableGrowthRoots = []string{"services", "gateway", "pkg", "internal"}

// growthTempo — кто задаёт темп роста. Словарь ЗАКРЫТ: корзины «прочее» нет,
// потому что «не знаю, кто пишет строки» есть незакрытый вопрос, а не третий
// вид темпа.
type growthTempo string

const (
	// tempoExternal — темп задаёт внешний: арендатор, поток запросов, чужая
	// система. Строк столько, сколько он захочет.
	tempoExternal growthTempo = "внешний"
	// tempoOurs — темп задаём мы: посев миграцией, действие администратора,
	// выкатка. Величина известна нам и меняется нашими руками.
	tempoOurs growthTempo = "наш"
)

// growthVerdict — какой механизм назван. Словарь ЗАКРЫТ по той же причине.
type growthVerdict string

const (
	// verdictBound — рост ОГРАНИЧЕН, и причина названа: закрытый словарь
	// значений ключа, одна строка на вид, ключ поверх конечного множества.
	// Это тот же исход «предел», что и каскад, только выраженный замыслом, а не
	// схемой, — поэтому выводиться он не может и объявляется.
	verdictBound growthVerdict = "предел"
	// verdictRetained — рост НЕ ограничен, и это решение: журнал, свидетельство,
	// история. Удержание намеренно, срок хранения — политика.
	verdictRetained growthVerdict = "удержание"
	// verdictDebt — механизма нет, решения тоже нет, предмет заведён задачей.
	// Единственный вердикт, требующий номера: без него запись не истекает и
	// становится слепой зоной.
	verdictDebt growthVerdict = "долг"
)

// growthFamily — К КАКОЙ СЕМЬЕ принадлежит таблица. Третья ось реестра, и
// единственная ПРОВЕРЯЕМАЯ ПО СХЕМЕ: темп и вердикт суть суждения человека, а
// семья имеет машинный признак — колонку-признак доставки.
//
// Словарь ЗАКРЫТ, и пустое значение здесь законно: классифицированы те семьи, у
// которых признак есть. Требовать объявления от КАЖДОЙ таблицы дерева значило бы
// заводить записи «на будущее» — ровно то, что реестр запрещает себе сам.
type growthFamily string

const (
	// familyUnclassified — семья не объявлена. Законное значение: гейт судит
	// только объявленное, а не требует объявления от всех.
	familyUnclassified growthFamily = ""
	// familyDrainerQueue — ОЧЕРЕДЬ ДРЕНАЖА: у строки есть адресат, дренаж
	// применяет её и помечает доставленной колонкой DeliveryMarkerColumn.
	// Признак ОБЯЗАН быть в схеме — это и проверяется.
	familyDrainerQueue growthFamily = "очередь дренажа"
	// familyJournal — ЖУРНАЛ: у строки нет адресата, который её применяет;
	// её читают ПО ПОЗИЦИИ (курсором подписки, курсором края, триггером,
	// сворачивающим строки в факт) либо не читают вовсе. Признака доставки в
	// схеме НЕТ, и его отсутствие проверяется так же строго, как у очереди —
	// наличие.
	//
	// Значение ОДНО на все виды журналов намеренно: ось называет то, что гейт
	// умеет проверить, — состояние схемы, а не механизм чтения. Кто именно
	// читает журнал, говорит ПРИЧИНА записи, и она у каждого своя.
	familyJournal growthFamily = "журнал"
)

// TableGrowthDecl — объявленный исход по одной таблице.
type TableGrowthDecl struct {
	// Owner, Table — та же единица счёта, что у переписи: владелец плюс имя.
	Owner string
	Table string
	// Tempo — кто задаёт темп. Суждение человека; гейт проверяет, что оно
	// вынесено и взято из закрытого словаря, а не что оно верно.
	Tempo growthTempo
	// Verdict — какой механизм назван.
	Verdict growthVerdict
	// Reason — почему. Обязателен у КАЖДОЙ записи: запись без причины — это
	// «подразумевается», ради снятия которого гейт и написан.
	Reason string
	// Issue — номер задачи. Обязателен у вердикта «долг».
	Issue string
	// Family — семья таблицы. Необязательна; объявленная — сверяется со схемой
	// гейтом TestGrowthRegistryFamilyMatchesTheSchema.
	Family growthFamily
}

// tableGrowthRegistry — объявленные исходы.
//
// # Это НЕ ведомость прощений
//
// Записи трёх видов, и только третий есть послабление. «Предел» и «удержание» —
// РЕШЕНИЯ, у каждого причина; «долг» — признание, что механизма нет, с номером
// задачи, где он заводится. Запись, у которой предмет исчез (таблица снята) или
// закрылся (появилась уборка либо каскад), объявляется гейтом потерявшей предмет
// и подлежит снятию.
//
// # Записи заведены ПО ФАКТУ, а не с запасом
//
// Каждая отвечает таблице, существующей на ревизии заведения (`534de0c4f`).
// Запись «на будущее» никогда не истечёт и станет слепой зоной шириной в одну
// таблицу — поэтому таблицы, которой в дереве нет, здесь нет тоже.
var tableGrowthRegistry = []TableGrowthDecl{
	{
		Owner: "services/iam", Table: "clusters",
		Tempo: tempoOurs, Verdict: verdictBound,
		Reason: "одна строка на установку: CHECK (id = 'cluster_kacho_root') второй не " +
			"допускает. Кластер — синглтон, и это закреплено ограничением, а не " +
			"соглашением",
	},
	{
		Owner: "services/compute", Table: "quota_sync_cursor",
		Tempo: tempoOurs, Verdict: verdictBound,
		Reason: "одна строка на ВИД синхронизации, а не на событие: ключ id перечисляет виды. " +
			"Курсор общий для всех реплик владельца намеренно — второй означал бы второй " +
			"порядок применения одних и тех же изменений (шапка миграции)",
	},
	{
		Owner: "services/nlb", Table: "quota_sync_cursor",
		Tempo: tempoOurs, Verdict: verdictBound,
		Reason: "одна строка на ВИД синхронизации, а не на событие: ключ id перечисляет виды. " +
			"Курсор общий для всех реплик владельца намеренно — второй означал бы второй " +
			"порядок применения одних и тех же изменений (шапка миграции)",
	},
	{
		Owner: "services/registry", Table: "quota_sync_cursor",
		Tempo: tempoOurs, Verdict: verdictBound,
		Reason: "одна строка на ВИД синхронизации, а не на событие: ключ id перечисляет виды. " +
			"Курсор общий для всех реплик владельца намеренно — второй означал бы второй " +
			"порядок применения одних и тех же изменений (шапка миграции)",
	},
	{
		Owner: "services/storage", Table: "quota_sync_cursor",
		Tempo: tempoOurs, Verdict: verdictBound,
		Reason: "одна строка на ВИД синхронизации, а не на событие: ключ id перечисляет виды. " +
			"Курсор общий для всех реплик владельца намеренно — второй означал бы второй " +
			"порядок применения одних и тех же изменений (шапка миграции)",
	},
	{
		Owner: "services/vpc", Table: "quota_sync_cursor",
		Tempo: tempoOurs, Verdict: verdictBound,
		Reason: "одна строка на ВИД синхронизации, а не на событие: ключ id перечисляет виды. " +
			"Курсор общий для всех реплик владельца намеренно — второй означал бы второй " +
			"порядок применения одних и тех же изменений (шапка миграции)",
	},
	{
		Owner: "services/iam", Table: "audit_outbox",
		Tempo: tempoExternal, Verdict: verdictRetained,
		Reason: "журнал аудита: свидетельство «кто это сделал», пишется в той же транзакции, " +
			"что и мутация. Удержание намеренно, срок хранения — политика, а не гигиена; " +
			"решение принято приёмкой #1292 " +
			"(services/iam/docs/engineering/acceptance/retention-sweep-has-a-caller.md, " +
			"§9)",
	},
	{
		Owner: "services/compute", Table: "audit_outbox",
		Tempo: tempoExternal, Verdict: verdictRetained,
		Reason: "тот же журнал аудита у compute (миграция 0028): актор и глагол мутации, " +
			"общая транзакция. Удержание намеренно по тому же решению, что и у iam, — " +
			"срок хранения есть политика",
	},
	{
		Owner: "services/iam", Table: "identity_journal",
		Tempo: tempoExternal, Verdict: verdictRetained,
		Reason: "журнал первого появления личности: строка на личность, first_seen_at не " +
			"обновляется никогда. По нему считается прирост, поэтому снятие строки " +
			"исказило бы ту самую величину, ради которой журнал заведён: вернувшаяся " +
			"личность стала бы новой",
	},
	{
		Owner: "services/iam", Table: "limits",
		Tempo: tempoOurs, Verdict: verdictRetained,
		Reason: "величины пределов, назначаемые администратором. Отзыв — надгробием " +
			"(withdrawn_at), а не удалением строки: снятую по ошибке величину надо уметь " +
			"назначить заново, а ревизия обязана двигаться монотонно, иначе догоняющий " +
			"читатель не заметит отзыва",
	},
	{
		Owner: "services/iam", Table: "account_admission_rate_limits",
		Tempo: tempoOurs, Verdict: verdictRetained,
		Reason: "та же форма и та же причина, что у limits: величина темпа заведения, отзыв " +
			"надгробием. Строку пишет администратор, не арендатор",
	},
	{
		Owner: "services/iam", Table: "token_signing_keys",
		Tempo: tempoOurs, Verdict: verdictRetained,
		Reason: "строка на ротацию ключа подписи; RETIRED/REMOVED — состояния, а не удаление. " +
			"Заголовок токена называет kid, и запись обязана пережить сам ключ: иначе " +
			"предъявленный токен нечем объяснить. Темп задаёт расписание ротации",
	},
	{
		Owner: "services/iam", Table: "fga_model_version",
		Tempo: tempoOurs, Verdict: verdictRetained,
		Reason: "история применённых моделей прав: строка на выкатку модели. Темп задаёт " +
			"выкатка, не арендатор; запись — свидетельство того, какая модель действовала",
	},
	{
		Owner: "services/storage", Table: "disk_type_bindings",
		Tempo: tempoOurs, Verdict: verdictRetained,
		Reason: "каталог привязок вида диска к носителю: прежняя привязка не удаляется, а " +
			"помечается SUPERSEDED — ревизия обязана оставаться восстановимой. Строку " +
			"заводит администратор",
	},
	{
		Owner: "services/compute", Table: "compute_outbox",
		Tempo: tempoExternal, Verdict: verdictBound,
		Reason: "журнал подписки: строка пишется на каждой мутации ресурса владельца, а рост " +
			"ограничен УБОРКОЙ ПО СРОКУ — `pkg/subscription.JournalRetention` (7 суток), " +
			"провязана в композиционном корне владельца " +
			"(`subscription.StartJournalRetentionSweep`, #1735). Владелец объявляет " +
			"`Retention: subscription.RetainsFromEarliestRow` и колонку срока `created_at` " +
			"(`DEFAULT now()`, 0001_initial.sql), поэтому подписчик, отставший дальше окна, " +
			"получает явный отказ с названной возобновимой позицией, а не неполное молча. " +
			"Имени таблицы в операторе снятия нет — оно приезжает значением, — поэтому " +
			"запись остаётся, а не снимается",
		Family: familyJournal,
	},
	{
		Owner: "services/compute", Table: "compute_fga_register_outbox",
		Tempo: tempoExternal, Verdict: verdictBound,
		Reason: "очередь дренажа: строка заводится в writer-транзакции мутации, а рост " +
			"ограничен УБОРКОЙ ДОСТАВЛЕННЫХ СТРОК — `pkg/outbox.DeliveredRetention` " +
			"(7 суток), провязана в композиционном корне владельца " +
			"(`outbox.StartQueueRetentionSweep`, #1361). Предикат снимает только строки с " +
			"отметкой доставки, поэтому отравленная строка не снимается ни при каком " +
			"возрасте, и ЩАДИТ доставленную строку, у которой в партиции есть " +
			"недоставленный предшественник: она одна не даёт реконсайлеру оживить " +
			"отравленного предшественника и тем отменить уже применённое снятие доступа. " +
			"Имени таблицы в операторе снятия нет — оно приезжает значением, — поэтому " +
			"запись остаётся, а не снимается",
		Family: familyDrainerQueue,
	},
	{
		Owner: "services/geo", Table: "geo_outbox",
		Tempo: tempoOurs, Verdict: verdictRetained,
		Reason: "журнал аудита admin-мутаций Region/Zone: строка пишется репозиторием в ТОЙ ЖЕ " +
			"writer-транзакции, что и мутация, и несёт АКТОРА в нагрузке (`actorFromCtx`, " +
			"сентинел `unknown` вместо пустой строки — CWE-778, закреплено пробой). Это тот " +
			"же род, что `audit_outbox` у iam и compute, и удержание принято тем же решением " +
			"(#1292): срок хранения аудита есть ПОЛИТИКА, а не гигиена. " +
			"Читателя в прод-коде у него нет, и это НЕ признак мёртвой таблицы: у журнала " +
			"аудита читатель — оператор, разбирающий «кто это сделал», а не процесс. " +
			"Темп задаём МЫ: Region/Zone правит только администратор через " +
			"InternalRegionService/InternalZoneService, каталог размещения меняется " +
			"месяцами. Снятие таблицы рассматривалось и отвергнуто: оно необратимо " +
			"уничтожает свидетельство атрибуции и ломает защиту, которую называет " +
			"`known-divergences.md` §5",
		Family: familyJournal,
	},
	{
		Owner: "services/iam", Table: "fga_outbox",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "журнал намерений, ПЕРЕСТАВШИЙ быть очередью: колонки доставки сняты " +
			"применённой миграцией вместе с дренажом, которому принадлежали (#917). " +
			"Строки читает ТРИГГЕР (`relation_fact_from_journal`, миграция 0098), " +
			"складывающий из них прямой факт о доступе. Перемерено #1712: триггер стоит " +
			"AFTER INSERT FOR EACH ROW и применяет строку В ТОЙ ЖЕ транзакции, а прямой " +
			"факт несёт СВОЮ отметку версии (`source_version`), с которой и сравниваются " +
			"последующие строки, — значит НИ ОДИН запрос не читает ПРОШЛЫХ строк журнала, " +
			"и для правильности удержание равно нулю. Порог, стало быть, задаёт не " +
			"механизм, а политика: чем платит арендатор за возможность восстановить " +
			"прямой факт из журнала и объяснить историю выдачи. Величина не выбрана, и " +
			"выбор этот продуктовый, а не инженерный — уборка тут возможна и безопасна, " +
			"но необратимо уничтожает обе возможности сразу",
		Issue:  "#1712",
		Family: familyJournal,
	},
	{
		Owner: "services/iam", Table: "subject_change_outbox",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "журнал смены субъекта, ПЕРЕСТАВШИЙ быть очередью: колонки доставки сняты " +
			"применённой миграцией вместе со своим дренажом. Строки читает КРАЙ курсором " +
			"по позиции (#1024), поэтому порог выводится из самого отставшего курсора. " +
			"ПЕРЕМЕРЕНО #1712, и это меняет исход: уборка по возрасту здесь СЕГОДНЯ " +
			"НЕБЕЗОПАСНА. Чтение идёт `WHERE id > since AND id <= settled` и пропуска не " +
			"обнаруживает никак — ни пола, ни отказа «позиция утрачена» у него нет, — а " +
			"пропущенная строка означает, что кэш вердиктов края НЕ ПОГАС, то есть отзыв " +
			"доступа не применён. Полоса fail-open by design (см. " +
			"docs/architecture/subject-change-journal-is-not-a-resource-stream.md), " +
			"поэтому потеря была бы МОЛЧАЛИВОЙ. Прежде уборки журнал обязан получить пол " +
			"и явный отказ возобновления — как у журналов подписки; у края при этом есть " +
			"дешёвый и безопасный ответ на такой отказ, которого нет у подписчика " +
			"ресурсов: погасить весь кэш вердиктов и продолжить",
		Issue:  "#1712",
		Family: familyJournal,
	},
	{
		Owner: "services/iam", Table: "resource_reconcile_outbox",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "очередь дренажа: строка заводится в writer-транзакции мутации, дренаж " +
			"помечает доставленную sent_at и не удаляет её никогда. Общий уборщик " +
			"платформы (`outbox.StartQueueRetentionSweep`, #1361) провязан у ПЯТИ очередей " +
			"регистрации, а эту НЕ покрывает by construction: он требует ключ партиции " +
			"порядка, потому что обязан щадить доставленную строку, защищающую " +
			"отравленного предшественника от оживления реконсайлером, — а у этой очереди " +
			"ключа партиции нет. Предикат ей нужен СВОЙ, и он проще: реконсайлера " +
			"(`RedrivePoisoned`) над очередями iam нет ни одного, значит защищать нечего; " +
			"но «сегодня реконсайлера нет» есть факт о дереве, и уборка без ключа обязана " +
			"нести гейт, который покраснеет, когда реконсайлер появится",
		Issue:  "#1361",
		Family: familyDrainerQueue,
	},
	{
		Owner: "services/iam", Table: "provider_compensation_outbox",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "очередь дренажа: строка заводится в writer-транзакции мутации, дренаж " +
			"помечает доставленную sent_at и не удаляет её никогда. Общий уборщик " +
			"платформы (#1361) её не покрывает by construction: ключ партиции у неё пуст " +
			"НАМЕРЕННО (поток коммутативен), а уборщик требует ключа, потому что обязан " +
			"щадить доставленную строку, защищающую отравленного предшественника от " +
			"оживления реконсайлером. Здесь защищать нечего — реконсайлера над очередями " +
			"iam нет ни одного, — поэтому предикат ей нужен СВОЙ и более простой; " +
			"послабление обязано нести гейт, который покраснеет, когда реконсайлер " +
			"появится",
		Issue:  "#1361",
		Family: familyDrainerQueue,
	},
	{
		Owner: "services/nlb", Table: "nlb_outbox",
		Tempo: tempoExternal, Verdict: verdictBound,
		Reason: "журнал подписки: строка пишется на каждой мутации ресурса владельца, а рост " +
			"ограничен УБОРКОЙ ПО СРОКУ — `pkg/subscription.JournalRetention` (7 суток), " +
			"провязана в композиционном корне владельца " +
			"(`subscription.StartJournalRetentionSweep`, #1735). Владелец объявляет " +
			"`Retention: subscription.RetainsFromEarliestRow` и колонку срока `emitted_at` " +
			"(`DEFAULT now()`, 0001_initial.sql), поэтому подписчик, отставший дальше окна, " +
			"получает явный отказ с названной возобновимой позицией, а не неполное молча. " +
			"Имени таблицы в операторе снятия нет — оно приезжает значением, — поэтому " +
			"запись остаётся, а не снимается",
		Family: familyJournal,
	},
	{
		Owner: "services/nlb", Table: "fga_register_outbox",
		Tempo: tempoExternal, Verdict: verdictBound,
		Reason: "очередь дренажа: строка заводится в writer-транзакции мутации, а рост " +
			"ограничен УБОРКОЙ ДОСТАВЛЕННЫХ СТРОК — `pkg/outbox.DeliveredRetention` " +
			"(7 суток), провязана в композиционном корне владельца " +
			"(`outbox.StartQueueRetentionSweep`, #1361). Предикат снимает только строки с " +
			"отметкой доставки, поэтому отравленная строка не снимается ни при каком " +
			"возрасте, и ЩАДИТ доставленную строку, у которой в партиции есть " +
			"недоставленный предшественник: она одна не даёт реконсайлеру оживить " +
			"отравленного предшественника и тем отменить уже применённое снятие доступа. " +
			"Имени таблицы в операторе снятия нет — оно приезжает значением, — поэтому " +
			"запись остаётся, а не снимается",
		Family: familyDrainerQueue,
	},
	{
		// Журнал подписки, заведённый эпиком единого потока изменений (#1016).
		// Дренажа у него НЕТ — читают курсором по номеру позиции, `sent_at` в
		// форме отсутствует, — поэтому #1361 его не покрывает: тот предмет чинится
		// в `pkg/outbox`, а журнал через `pkg/outbox` не проходит вовсе.
		//
		// ЗАПИСЬ ОСТАЛАСЬ ПРИ ЖИВОМ МЕХАНИЗМЕ, и причина названа в вердикте: имя
		// таблицы приезжает в оператор снятия ЗНАЧЕНИЕМ (`Storage.Table`), поэтому
		// текста имени в исходнике нет и разбор этого гейта его не резолвит — он
		// уходит в НАЗВАННУЮ ЭТИМ ЖЕ ГЕЙТОМ слепую зону `RemovalsUnresolved`
		// (шапка `tablegrowth.go`, §«Чего разбор не видит», п. 1). Машинную
		// сторону держит другой гейт — `TestSubscriptionJournalLanesAgreeOnRetention`.
		Owner: "services/registry", Table: "registry_resource_journal",
		Tempo: tempoExternal, Verdict: verdictBound,
		Reason: "журнал подписки: строка пишется триггером на каждой мутации реестра, а рост " +
			"ограничен УБОРКОЙ ПО СРОКУ — `pkg/subscription.JournalRetention` (7 суток), " +
			"провязана в композиционном корне владельца " +
			"(`startJournalRetentionSweep`, #1666). Владелец объявляет " +
			"`Retention: subscription.RetainsFromEarliestRow`, и подписчик, отставший " +
			"дальше окна, получает явный отказ с названной возобновимой позицией, а не " +
			"неполное молча. Имени таблицы в операторе снятия нет — оно приезжает " +
			"значением, — поэтому запись остаётся, а не снимается",
		Family: familyJournal,
	},
	{
		// Тот же журнал у storage. Имя совпадает со снятой 0011 очередью, но форма
		// другая: ушли `processed_at` и индекс по виду, пришёл `project_id` —
		// авторизуемый якорь. Пишет ТРИГГЕР, потому что писателей ресурсных строк
		// здесь больше, чем репозиториев (сверщик мутирует пул напрямую).
		//
		// Запись осталась при живом механизме по той же причине, что у соседа:
		// имя таблицы приезжает в оператор снятия значением, разбор его не
		// резолвит. См. запись `registry_resource_journal` выше.
		Owner: "services/storage", Table: "storage_outbox",
		Tempo: tempoExternal, Verdict: verdictBound,
		Reason: "журнал подписки: строка пишется триггером на каждой мутации тома, снимка и " +
			"образа (включая мутации сверщика мимо репозиториев), а рост ограничен " +
			"УБОРКОЙ ПО СРОКУ — `pkg/subscription.JournalRetention` (7 суток), провязана " +
			"в композиционном корне владельца (#1666). Владелец объявляет " +
			"`Retention: subscription.RetainsFromEarliestRow`, и подписчик, отставший " +
			"дальше окна, получает явный отказ с названной возобновимой позицией, а не " +
			"неполное молча. Имени таблицы в операторе снятия нет — оно приезжает " +
			"значением, — поэтому запись остаётся, а не снимается",
		Family: familyJournal,
	},
	{
		Owner: "services/registry", Table: "registry_outbox",
		Tempo: tempoExternal, Verdict: verdictBound,
		Reason: "очередь дренажа: строка заводится в writer-транзакции мутации, а рост " +
			"ограничен УБОРКОЙ ДОСТАВЛЕННЫХ СТРОК — `pkg/outbox.DeliveredRetention` " +
			"(7 суток), провязана в композиционном корне владельца " +
			"(`outbox.StartQueueRetentionSweep`, #1361). Предикат снимает только строки с " +
			"отметкой доставки, поэтому отравленная строка не снимается ни при каком " +
			"возрасте, и ЩАДИТ доставленную строку, у которой в партиции есть " +
			"недоставленный предшественник: она одна не даёт реконсайлеру оживить " +
			"отравленного предшественника и тем отменить уже применённое снятие доступа. " +
			"Имени таблицы в операторе снятия нет — оно приезжает значением, — поэтому " +
			"запись остаётся, а не снимается",
		Family: familyDrainerQueue,
	},
	{
		Owner: "services/storage", Table: "fga_register_outbox",
		Tempo: tempoExternal, Verdict: verdictBound,
		Reason: "очередь дренажа: строка заводится в writer-транзакции мутации, а рост " +
			"ограничен УБОРКОЙ ДОСТАВЛЕННЫХ СТРОК — `pkg/outbox.DeliveredRetention` " +
			"(7 суток), провязана в композиционном корне владельца " +
			"(`outbox.StartQueueRetentionSweep`, #1361). Предикат снимает только строки с " +
			"отметкой доставки, поэтому отравленная строка не снимается ни при каком " +
			"возрасте, и ЩАДИТ доставленную строку, у которой в партиции есть " +
			"недоставленный предшественник: она одна не даёт реконсайлеру оживить " +
			"отравленного предшественника и тем отменить уже применённое снятие доступа. " +
			"Имени таблицы в операторе снятия нет — оно приезжает значением, — поэтому " +
			"запись остаётся, а не снимается",
		Family: familyDrainerQueue,
	},
	{
		Owner: "services/vpc", Table: "vpc_outbox",
		Tempo: tempoExternal, Verdict: verdictBound,
		Reason: "журнал подписки: строка пишется на каждой мутации ресурса владельца, а рост " +
			"ограничен УБОРКОЙ ПО СРОКУ — `pkg/subscription.JournalRetention` (7 суток), " +
			"провязана в композиционном корне владельца " +
			"(`subscription.StartJournalRetentionSweep`, #1735). Владелец объявляет " +
			"`Retention: subscription.RetainsFromEarliestRow` и колонку срока `created_at` " +
			"(`DEFAULT now()`, 0001_initial.sql), поэтому подписчик, отставший дальше окна, " +
			"получает явный отказ с названной возобновимой позицией, а не неполное молча. " +
			"Имени таблицы в операторе снятия нет — оно приезжает значением, — поэтому " +
			"запись остаётся, а не снимается",
		Family: familyJournal,
	},
	{
		Owner: "services/vpc", Table: "fga_register_outbox",
		Tempo: tempoExternal, Verdict: verdictBound,
		Reason: "очередь дренажа: строка заводится в writer-транзакции мутации, а рост " +
			"ограничен УБОРКОЙ ДОСТАВЛЕННЫХ СТРОК — `pkg/outbox.DeliveredRetention` " +
			"(7 суток), провязана в композиционном корне владельца " +
			"(`outbox.StartQueueRetentionSweep`, #1361). Предикат снимает только строки с " +
			"отметкой доставки, поэтому отравленная строка не снимается ни при каком " +
			"возрасте, и ЩАДИТ доставленную строку, у которой в партиции есть " +
			"недоставленный предшественник: она одна не даёт реконсайлеру оживить " +
			"отравленного предшественника и тем отменить уже применённое снятие доступа. " +
			"Имени таблицы в операторе снятия нет — оно приезжает значением, — поэтому " +
			"запись остаётся, а не снимается",
		Family: familyDrainerQueue,
	},
	{
		Owner: "services/vpc", Table: "nested_quota_defaults",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "зеркало вложенных пределов, ключ «проект, вид»: слияние по ключу есть, " +
			"ограничения роста нет. Проекты заводит и снимает арендатор, внешнего ключа " +
			"на проект быть не может (ban #4), и строка снятого проекта остаётся навсегда",
		Issue: "#1363",
	},
	{
		Owner: "services/nlb", Table: "nested_quota_defaults",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "зеркало вложенных пределов, ключ «проект, вид»: слияние по ключу есть, " +
			"ограничения роста нет. Проекты заводит и снимает арендатор, внешнего ключа " +
			"на проект быть не может (ban #4), и строка снятого проекта остаётся навсегда",
		Issue: "#1363",
	},
	{
		Owner: "services/registry", Table: "nested_quota_defaults",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "зеркало вложенных пределов, ключ «проект, вид»: слияние по ключу есть, " +
			"ограничения роста нет. Проекты заводит и снимает арендатор, внешнего ключа " +
			"на проект быть не может (ban #4), и строка снятого проекта остаётся навсегда",
		Issue: "#1363",
	},
	{
		Owner: "services/iam", Table: "recovery_completions",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "леджер однократности завершения восстановления: строка на каждое завершение, " +
			"колонки срока нет вовсе. Назван приёмкой #1292 (§9) и вынесен из её объёма",
		Issue: "#1364",
	},
	{
		Owner: "services/iam", Table: "identity_admission_windows",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "счётчик допущенных заведений в действующем окне, ключ «носитель, вид»: окно " +
			"двигается ВНУТРИ строки, поэтому строка живёт вечно ради счётчика, который " +
			"каждое окно начинается заново",
		Issue: "#1364",
	},
}

// tableGrowthState — прочитанное дерево, отделённое от суждения о нём.
type tableGrowthState struct {
	// Tables — живые таблицы: созданные и не снятые применёнными миграциями.
	Tables []TableDecl
	// Removals — операторы снятия строк всех полос.
	Removals []SQLRemoval
	// Cascade — число ЖИВЫХ каскадных внешних ключей на таблице.
	Cascade map[TableRef]int
}

// tableGrowthCounts — числа вердикта.
type tableGrowthCounts struct {
	Tables   int
	Swept    int
	Cascaded int
	Declared int
	Bound    int
	Retained int
	Debt     int
}

// removalCoversTable отвечает, снимает ли оператор строки ИМЕННО этой таблицы.
//
// Схема сверяется, только когда её назвали ОБЕ стороны: оператор общей
// библиотеки подставляет схему в рантайме (`DELETE FROM %s.registry_push_grant`),
// а часть таблиц объявлена без схемы, полагаясь на `search_path`. Требовать
// совпадения там, где одна сторона молчит, значило бы объявить находкой живую
// уборку.
func removalCoversTable(r SQLRemoval, t TableDecl) bool {
	if r.Table != t.Name {
		return false
	}
	if r.Schema == "" || t.Schema == "" {
		return true
	}
	return r.Schema == t.Schema
}

// tableGrowthVerdict — ЧИСТОЕ суждение по уже прочитанному дереву.
//
// Отделено от обхода намеренно: инъекция подаёт сюда синтетическое состояние и
// проверяет, что суждение способно упасть И способно смолчать. На настоящем
// дереве ни того ни другого не показать, не сломав его.
func tableGrowthVerdict(st tableGrowthState, registry []TableGrowthDecl) (findings, stale []string, counts tableGrowthCounts) {
	byRef := map[TableRef]TableGrowthDecl{}
	for _, e := range registry {
		ref := TableRef{Owner: e.Owner, Name: e.Table}
		if _, dup := byRef[ref]; dup {
			findings = append(findings, ref.String()+
				" — в реестре ДВЕ записи об одной таблице. Исход обязан быть ровно один: "+
				"две записи означают два решения, и какое из них действует, не сказано нигде")
			continue
		}
		byRef[ref] = e
		if e.Tempo != tempoExternal && e.Tempo != tempoOurs {
			findings = append(findings, ref.String()+
				" — запись реестра не называет темп ("+string(e.Tempo)+"). Словарь закрыт: "+
				"«"+string(tempoExternal)+"» либо «"+string(tempoOurs)+"». Именно здесь и объявляется то, "+
				"чего гейт не выводит, — поэтому пустое значение означает, что решение не вынесено")
		}
		switch e.Verdict {
		case verdictBound, verdictRetained, verdictDebt:
		default:
			findings = append(findings, ref.String()+
				" — запись реестра не называет вердикт ("+string(e.Verdict)+"). Словарь закрыт: "+
				"«"+string(verdictBound)+"» · «"+string(verdictRetained)+"» · «"+string(verdictDebt)+"»")
		}
		if strings.TrimSpace(e.Reason) == "" {
			findings = append(findings, ref.String()+
				" — запись реестра без ПРИЧИНЫ. Причина обязательна у каждой записи: без неё "+
				"запись есть то самое «подразумевается», ради снятия которого гейт написан")
		}
		if e.Verdict == verdictDebt && strings.TrimSpace(e.Issue) == "" {
			findings = append(findings, ref.String()+
				" — вердикт «"+string(verdictDebt)+"» без НОМЕРА ЗАДАЧИ. Долг без предмета не истекает "+
				"никогда и становится слепой зоной шириной в одну таблицу")
		}
	}

	live := map[TableRef]bool{}
	for _, t := range st.Tables {
		counts.Tables++
		live[t.TableRef] = true

		swept := false
		for _, r := range st.Removals {
			if r.Lane == RemovalLaneOneShot {
				continue
			}
			if removalCoversTable(r, t) {
				swept = true
				break
			}
		}
		cascaded := st.Cascade[t.TableRef] > 0
		entry, listed := byRef[t.TableRef]

		switch {
		case swept:
			counts.Swept++
			if listed {
				stale = append(stale, t.TableRef.String()+
					" — запись реестра ПОТЕРЯЛА ПРЕДМЕТ: у таблицы появился оператор снятия строк. "+
					"Снимите запись: исход у таблицы ровно один, а две записи об одном предмете "+
					"расходятся молча")
			}
		case cascaded:
			counts.Cascaded++
			if listed {
				stale = append(stale, t.TableRef.String()+
					" — запись реестра ПОТЕРЯЛА ПРЕДМЕТ: у таблицы объявлен каскадный внешний ключ, "+
					"то есть предел выражен схемой и объявлять его записью больше не нужно")
			}
		case listed:
			counts.Declared++
			switch entry.Verdict {
			case verdictBound:
				counts.Bound++
			case verdictRetained:
				counts.Retained++
			case verdictDebt:
				counts.Debt++
			}
		default:
			findings = append(findings, t.File+":"+strconv.Itoa(t.Line)+" "+t.TableRef.String()+
				" — живая таблица БЕЗ названного механизма ограничения роста: ни одного оператора "+
				"снятия строк (ни в прод-коде, ни в теле триггера применённой миграции), ни одного "+
				"каскадного внешнего ключа на ней самой. Исходов ровно три: завести уборщик (и он "+
				"попадёт под гейт вызывающего); объявить ПРЕДЕЛ записью в tableGrowthRegistry, если "+
				"рост ограничен замыслом — закрытым словарём ключа, одной строкой на вид, посевом; "+
				"либо завести ПРЕДМЕТ (задачу) и запись с вердиктом «"+string(verdictDebt)+"». Молчание здесь "+
				"означает, что рост таблицы не решён никем, а первое, что о нём скажут, — место на диске")
		}
	}

	for _, e := range registry {
		ref := TableRef{Owner: e.Owner, Name: e.Table}
		if !live[ref] {
			stale = append(stale, ref.String()+
				" — запись реестра ПОТЕРЯЛА ПРЕДМЕТ: живой таблицы с таким именем у этого владельца "+
				"в применённых миграциях нет. Снимите запись")
		}
	}

	sort.Strings(findings)
	sort.Strings(stale)
	return findings, stale, counts
}

// foldMigrationScans сводит разборы миграций, прочитанные В ПОРЯДКЕ ПРИМЕНЕНИЯ,
// в состояние дерева.
//
// # Порядок несущий, и это не педантство
//
// Таблица бывает создана, снята и создана заново; каскадное ограничение —
// объявлено и снято (`ALTER TABLE … DROP CONSTRAINT` в этом дереве встречается
// 55 раз, и среди снятых имён есть внешние ключи). Свернув разборы вразнобой,
// разбор объявил бы живой снятую таблицу либо оставил бы предел, которого уже
// нет, — и второе хуже: это МОЛЧАНИЕ там, где положена находка.
//
// Отделено от чтения файлов намеренно: инъекция подаёт сюда синтетическую
// последовательность и проверяет порядок, не заводя файлов.
func foldMigrationScans(scans []MigrationScan) tableGrowthState {
	state := tableGrowthState{Cascade: map[TableRef]int{}}
	created := map[TableRef]TableDecl{}
	constraints := map[ConstraintKey]bool{}
	for _, scan := range scans {
		for _, d := range scan.Created {
			created[d.TableRef] = d
		}
		for _, k := range scan.CascadeAdded {
			constraints[k] = true
		}
		for _, k := range scan.CascadeDropped {
			delete(constraints, k)
		}
		for _, d := range scan.Dropped {
			delete(created, d)
			for k := range constraints {
				if k.Owner == d.Owner && k.Table == d.Name {
					delete(constraints, k)
				}
			}
		}
		state.Removals = append(state.Removals, scan.Removals...)
	}
	for _, d := range created {
		state.Tables = append(state.Tables, d)
	}
	sort.Slice(state.Tables, func(i, j int) bool {
		return state.Tables[i].TableRef.String() < state.Tables[j].TableRef.String()
	})
	for k := range constraints {
		state.Cascade[TableRef{Owner: k.Owner, Name: k.Table}]++
	}
	return state
}

// readTableGrowthTree читает дерево: миграции в порядке применения и прод-код Go.
func readTableGrowthTree(t *testing.T, root string) (tableGrowthState, TableGrowthCensus) {
	t.Helper()

	var (
		census TableGrowthCensus
		state  = tableGrowthState{Cascade: map[TableRef]int{}}
	)

	// Миграции — В ПОРЯДКЕ ПРИМЕНЕНИЯ. Порядок несущий: таблица бывает создана,
	// снята и создана заново, а каскадное ограничение — объявлено и снято. Читая
	// вразнобой, разбор объявил бы живой снятую таблицу либо оставил бы каскад,
	// которого уже нет.
	var migrations []string
	for _, sub := range tableGrowthRoots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		files, err := treecorpus.UnderWithSuffix(base, ".sql")
		if err != nil {
			t.Fatalf("состав дерева под %s: %v", base, err)
		}
		for _, path := range files {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				t.Fatalf("относительный путь %s: %v", path, rerr)
			}
			rel = filepath.ToSlash(rel)
			if !strings.Contains(rel, "/migrations/") {
				continue
			}
			migrations = append(migrations, rel)
		}
	}
	sort.Slice(migrations, func(i, j int) bool {
		di, dj := filepath.Dir(migrations[i]), filepath.Dir(migrations[j])
		if di != dj {
			return di < dj
		}
		return filepath.Base(migrations[i]) < filepath.Base(migrations[j])
	})

	var scans []MigrationScan
	for _, rel := range migrations {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		scan := ScanMigrationSQL(MigrationOwnerOf(filepath.Dir(rel)), rel, body)
		census.Add(scan.Census)
		scans = append(scans, scan)
	}
	state = foldMigrationScans(scans)

	walkOwnerRegisterGoFiles(t, root, tableGrowthRoots, func(rel string, body []byte) {
		removals, c, err := ScanGoSQLRemovals(rel, body)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		census.Add(c)
		state.Removals = append(state.Removals, removals...)
	})

	return state, census
}

// TestLiveTablesNameTheirGrowthLimit — сам гейт.
func TestLiveTablesNameTheirGrowthLimit(t *testing.T) {
	root := repoRoot(t)
	state, census := readTableGrowthTree(t, root)
	findings, stale, counts := tableGrowthVerdict(state, tableGrowthRegistry)

	// Перепись печатает ОБЕ величины — сколько осмотрено и сколько с механизмом,
	// — потому что одно число не отличает «ноль находок» от «ноль прочитанного».
	t.Logf("перепись: таблиц осмотрено %d, из них с названным механизмом %d "+
		"(уборка %d · предел каскадом %d · объявлено %d: предел %d, удержание %d, долг %d); находок %d",
		counts.Tables, counts.Swept+counts.Cascaded+counts.Declared,
		counts.Swept, counts.Cascaded, counts.Declared,
		counts.Bound, counts.Retained, counts.Debt, len(findings))
	t.Logf("перепись обхода: миграций прочитано %d (с секциями goose %d), файлов Go %d, "+
		"строковых значений Go %d; операторов создания таблиц %d, снятия таблиц %d; "+
		"каскадных ключей объявлено %d именованных и %d безымянных, снятий ограничений %d",
		census.MigrationFiles, census.SectionedMigrations, census.GoFiles, census.GoStrings,
		census.Creates, census.Drops, census.CascadesNamed, census.CascadesUnnamed,
		census.ConstraintDrops)
	t.Logf("перепись полос снятия строк: прод %d, тело триггера %d, разовая правка миграции %d "+
		"(механизмом не считается), с неразрешимым именем таблицы %d (слепая зона, названная числом)",
		census.RemovalsProduction, census.RemovalsTrigger, census.RemovalsOneShot,
		census.RemovalsUnresolved)

	// ПРЕДПОСЫЛКИ РАЗБОРА. Каждая — факт о дереве; факт меняется, и тогда запрет
	// становится ложью. Пусть гейт заявляет их сам, а не полагается на то, что
	// читатель помнит, чем он обоснован.
	if census.MigrationFiles == 0 {
		t.Fatal("прочитано ноль миграций — гейт не читал дерева, и его молчание ничего не значит")
	}
	if census.SectionedMigrations == 0 {
		t.Fatal("ни одна миграция не несёт секций `-- +goose Up`/`-- +goose Down` — предпосылка " +
			"разбора («применяется секция Up, Down есть откат») перестала быть верной, а вместе " +
			"с ней и различение созданного от откаченного")
	}
	if census.GoFiles == 0 {
		t.Fatal("прочитано ноль файлов Go — полоса прод-уборки не читалась вовсе, и всякая " +
			"убираемая таблица была бы объявлена находкой")
	}
	if counts.Tables == 0 {
		t.Fatal("живых таблиц не найдено ни одной — предикат разбора разъехался с миграциями. " +
			"Таблицы в этом дереве есть (их объявляют семь служб и шлюз), значит молчание гейта " +
			"означает слепоту, а не отсутствие предмета")
	}
	if census.RemovalsProduction == 0 {
		t.Fatal("операторов снятия строк в прод-коде не найдено ни одного — разбор строковых " +
			"значений Go разъехался с кодом. Удаление в этом дереве есть у каждого глагола Delete, " +
			"значит ноль здесь означает слепоту")
	}
	if census.CascadesNamed+census.CascadesUnnamed == 0 {
		t.Fatal("каскадных внешних ключей не найдено ни одного — полоса «предел выражен схемой» " +
			"не читалась, и таблица, чьи строки умирают вместе с родителем, стала бы находкой")
	}

	for _, s := range stale {
		t.Errorf("реестр: %s", s)
	}
	for _, f := range findings {
		t.Errorf("таблица без названного механизма: %s", f)
	}
}
