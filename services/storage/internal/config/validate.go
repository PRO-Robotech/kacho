// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"sort"
	"strings"

	// Дескрипторы обслуживаемых RPC обязаны быть в бинаре ЭТОГО пакета: полоса
	// `scope_filtered` выводится из их аннотаций, и пустой реестр дал бы пустую
	// полосу — то есть стражу, которой нечего охранять, неотличимую от стражи,
	// у которой всё в порядке.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/api"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
)

// scopeFilteredRPCs — методы карты прав, с которых снят per-RPC Check: их
// авторизация целиком переехала на данные (пообъектное сужение прочитанной
// страницы). Список отсортирован — текст отказа наблюдаем оператором и не должен
// перетасовываться от прогона к прогону (обход карты не детерминирован).
//
// Карта выводится ЗДЕСЬ из аннотаций дескрипторов — из того же источника, из
// которого её выводит носитель на старте, — а не передаётся вызывающим: страж,
// которому список нужно вручить, no-op'ится на пустом аргументе, и «забыли
// передать» внешне неотличимо от «нечего защищать».
func scopeFilteredRPCs() []string {
	m, err := catalogderive.Derive(storageProtoPackages...)
	if err != nil {
		// Карта не вывелась — это свойство собранного бинаря, одинаковое на
		// каждом старте. Молчать нельзя: пустой список погасил бы стражу
		// fail-open, поэтому отказ называется прямо и роняет старт вместе с
		// прочими находками Validate.
		return []string{"<карта прав не выводится: " + err.Error() + ">"}
	}
	var out []string
	for fullMethod, e := range m {
		if e.ScopeFiltered {
			out = append(out, fullMethod)
		}
	}
	sort.Strings(out)
	return out
}

// storageProtoPackages — proto-пакеты, чьи gRPC-службы поднимает storage. Тот же
// набор, из которого носитель выводит карту прав по служимому набору RPC.
var storageProtoPackages = []string{
	"kacho.cloud.storage.v1",
	// LRO-конверт: Operation.Get/Cancel поднимает каждый сервис.
	"kacho.cloud.operation",
}

// Validate — остаток собственного стража старта: измерения, которых НОСИТЕЛЬ НЕ
// ЗНАЕТ.
//
// # Что отсюда ушло и куда
//
// Разбор режима, круг отправителей чужой личности, sslmode до своей БД, транспорт
// обоих слушателей и ребро решения о доступе судит теперь конструктор дескриптора
// (`pkg/servicecontract`) — один отказ на все сервисы вместо семи собственных.
// Переезд не ослабил ни одного из них, а два усилил: транспорт спрашивается у
// САМОГО ТРАНСПОРТА (`Info().SecurityProtocol`), а не у ручки `Enable`, и ребро
// решения о доступе обязано быть объявлено на ЛЮБОЙ посадке, а не только в боевой.
//
// # Что осталось и почему это не дубль
//
// Носитель не знает про:
//   - транспорт ИСХОДЯЩИХ рёбер storage→geo и storage→iam (кроме authz-половины
//     последнего): у него нет ни адресов, ни ручек этих клиентов;
//   - включённость пообъектного сужателя (`ListFilterEnabled`);
//   - его degraded-ручку (`ListFilterFailOpen`).
//
// Перечень обязан оставаться полным: измерение, которое композиционный корень
// настраивает, а ни носитель, ни этот страж не проверяют, — ровно тот класс,
// которым сюда попал круг доверенных отправителей (проводка была, ручки и стражи
// не было).
func (c Config) Validate() error {
	mode, err := servicecontract.ParseMode(c.AuthMode)
	if err != nil {
		return fmt.Errorf("KACHO_STORAGE_AUTH_MODE: %w", err)
	}

	// Объявление домена величин судится ДО развилки по режиму: незаданное
	// объявление означает, что оператор не выбрал между «потолки действуют» и
	// «потолков нет», и подставить за него разумное умолчание нельзя ни в каком
	// режиме. Это не свойство посадки, а отсутствие решения.
	if qerr := c.ValidateQuotaAuthority(); qerr != nil {
		return qerr
	}

	if !mode.IsProduction() {
		// dev — insecure-дефолты допустимы (WARN в serve.go, не fatal): локальные
		// фикстуры и dev-профиль стенда. Круг отправителей здесь больше не
		// считается: его стража переехала в конструктор дескриптора и там
		// срабатывает на ЛЮБОМ старте, а не только в боевом.
		return nil
	}

	var problems []string
	// В боевом режиме отказ не возвращается сразу: оператор обязан увидеть ВЕСЬ
	// список проблем за один прогон, а не чинить их по одной.

	// ── пообъектный сужатель обязан быть включён ────────────────────────────
	// Per-RPC Check гейтит публичный List лишь на project-tier `viewer`: он
	// отвечает «этот caller вправе листать ЭТОТ проект», но НЕ сужает страницу до
	// объектов, на которые есть грант. Сужение делает ТОЛЬКО фильтр (пообъектный
	// `viewer` батчем по прочитанной странице — то же отношение, что энфорсит
	// Get). Выключенный фильтр = любой член проекта видит КАЖДЫЙ том/снимок/образ
	// проекта (over-show / BOLA-lite, CWE-862 / OWASP A01).
	//
	// Для InternalVolumeService/ListAttachments (:9091) фильтр — не второй слой, а
	// ЕДИНСТВЕННЫЙ: этот RPC помечен `scope_filtered` (единичного объекта, про
	// который можно спросить заранее, у него нет — инстансы называет вызывающий),
	// поэтому per-RPC Check за него не задаётся вовсе.
	//
	// Адрес authorize-эндпоинта отдельно не проверяем: он и есть AuthZIAMGRPCAddr,
	// который требует конструктор дескриптора как ребро решения о доступе.
	if !c.ListFilterEnabled {
		problems = append(problems,
			"per-object List filter required: set KACHO_STORAGE_LIST_FILTER_ENABLED=true "+
				"(false → public List bypasses the per-object FGA filter, so a project-tier viewer sees "+
				"every volume/snapshot/image; and the internal attachment listing, which has no per-RPC "+
				"check at all, would lose its only gate)")
	}

	// ── degraded-ручка того же фильтра: fail-open ───────────────────────────
	// Включённый фильтр, который на ошибке iam отдаёт страницу целиком, защищает
	// не больше выключенного — он просто выключается позже и по чужой аварии.
	// Пока в карте прав есть хоть одна запись `scope_filtered`, за этой ручкой не
	// остаётся НИЧЕГО, и деградация означает выдачу привязок любых названных
	// инстансов — из чужих проектов и чужих аккаунтов.
	//
	// Гейт условный намеренно: он сам снимется, если полоса `scope_filtered` уйдёт
	// из каталога, и не требует помнить о себе при этом.
	//
	// Осознанный размен: стенд, намеренно выставивший fail-open, перестанет
	// подниматься. Это и есть цель — иначе защиту снимает одна ручка, молча.
	if c.ListFilterFailOpen {
		if scopeFiltered := scopeFilteredRPCs(); len(scopeFiltered) > 0 {
			problems = append(problems, fmt.Sprintf(
				"fail-closed List filter required: set KACHO_STORAGE_LIST_FILTER_FAIL_OPEN=false "+
					"(true → on ANY iam error the page is returned UNFILTERED; %d RPC(s) carry no per-RPC "+
					"Check at all and rely on this filter as their only gate: %s)",
				len(scopeFiltered), strings.Join(scopeFiltered, ", ")))
		}
	}

	// ── транспорт ИСХОДЯЩИХ рёбер ───────────────────────────────────────────
	// Транспорт слушателей — то, КАК с нами говорят, — судит конструктор
	// дескриптора. Здесь — то, как говорим МЫ: клиентская сторона рёбер
	// storage→iam и storage→geo.
	//
	// Почему это отдельное измерение, а не следствие проверки слушателей.
	// Невзведённая ручка клиента не даёт ошибки сама по себе:
	// grpcclient.TLSClientCreds на Enable=false возвращает insecure-creds БЕЗ
	// ошибки, поэтому процесс поднимается, печатает «peer edge configured», а
	// каждый вызов уходит по открытому каналу. Контроль, от которого зависит
	// решение о доступе, при этом присутствует и не отказывает ни разу за свою
	// жизнь.
	//
	// Предикат активности — ТОТ ЖЕ, что читает проводка: composition root зовёт
	// dialPeer(addr, creds, …), и dialPeer поднимает соединение ровно при
	// непустом адресе (cmd/storage/serve.go). Поэтому «страж увидел ребро» ⟺
	// «ребро дилится»: незаданный адрес не порождает требования к транспорту, а
	// заданный — порождает всегда. Связь стража с проводкой заперта
	// cmd/storage/peer_transport_wiring_test.go.
	//
	// Ручка на все три ребра к iam одна (IAMClientMTLS) — это записанное
	// отступление storage от соседей, см. docs/architecture/known-divergences.md
	// §1. Страж от него не зависит: он требует того же предиката, который читает
	// проводка, каким бы ни было число ручек.
	if c.AuthZIAMGRPCAddr != "" || c.IAMGRPCAddr != "" {
		if !c.IAMClientMTLS.Enable {
			problems = append(problems,
				"verified transport required on the storage→iam edges: set KACHO_STORAGE_IAM_CLIENT_MTLS_ENABLE=true "+
					"(with cert/key/CA) — the per-RPC authorization Check, the per-object List filter, the owner-tuple "+
					"registration and the project existence lookup all travel over this connection, and unarmed client "+
					"credentials degrade to cleartext silently, so the process starts and reports authorization as enabled")
		}
	}
	if c.GeoGRPCAddr != "" && !c.GeoClientMTLS.Enable {
		problems = append(problems,
			"verified transport required on the storage→geo edge: set KACHO_STORAGE_GEO_CLIENT_MTLS_ENABLE=true "+
				"(with cert/key/CA) — zone existence and the volume/image placement coherence check are decided on "+
				"this connection, and unarmed client credentials degrade to cleartext silently")
	}

	// ── плоскость данных блочного хранения ──────────────────────────────────
	//
	// Предикат активности — тот же, что читает проводка: композиционный корень
	// поднимает адаптер и сверщик ровно при непустом виде бэкенда. Поэтому
	// «страж увидел плоскость данных» ⟺ «она провязана»: незаданный вид не
	// порождает требований, заданный — порождает всегда.
	//
	// Чего здесь НЕТ и почему. Соотношения со сроком исполнителя операций здесь
	// нет намеренно: обращение к бэкенду происходит ВНЕ функции операции —
	// операция фиксирует намерение и завершается, а доводит сверщик. Именно это
	// и снимает класс «ложное готово», при котором длинное обращение убивалось
	// потолком исполнителя, а разрешитель осиротевших операций затем признавал
	// строку завершённой, читая нашу БД. Читателю, помнящему прежнюю схему,
	// связь между этими величинами покажется пропущенной — её нет by design.
	// ПЛОСКОСТЬ ДАННЫХ НЕ ОБЯЗАТЕЛЬНА — И ЭТО НЕ ПОСЛАБЛЕНИЕ, А ОПРЕДЕЛЕНИЕ ПРОДУКТА.
	//
	// Здесь стояло обратное требование: в боевом режиме вид плоскости данных
	// обязателен, иначе отказ в старте. Рассуждение было такое — сверщик не
	// запустится, ничто не переведёт ресурс из намерения в пригодность, тома
	// останутся создаваемыми навсегда. Каждое звено верно, а вывод неверен.
	//
	// Kachō — платформа ТОЛЬКО управляющей плоскости. Плоскости данных у неё нет
	// by construction, и требовать её объявления значит требовать того, чего в
	// продукте не бывает. Цена ошибки измерена, а не предположена: сквозные
	// прогоны поднимаются без кластера хранения, и с этим требованием НИ ОДИН том
	// не становился пригодным — падали чужие наборы, чья фикстура ждала готового
	// источника.
	//
	// Действующее правило простое и держится ниже: вид ОБЪЯВЛЕН — набор его ручек
	// обязан быть полным (иначе сверщик берёт работу и не двигает её); вид НЕ
	// объявлен — сверять не с чем, и фиксация записи сама есть готовность.
	if c.BlockBackendKind != "" {
		if err := blockbackend.ValidateInstallPrefix(c.BlockBackendInstallPrefix); err != nil {
			problems = append(problems, fmt.Sprintf(
				"install prefix required with a block backend: set KACHO_STORAGE_BLOCK_BACKEND_INSTALL_PREFIX (%v) "+
					"— object names are derived from immutable resource ids, so two deployments pointed at one "+
					"backend cluster would derive the SAME names and adopt each other's objects: each reconciler "+
					"would count the other's objects as its own leak, and a delete in one cloud would destroy data "+
					"in the other", err))
		}
		if c.BlockBackendCredentialsDir == "" {
			problems = append(problems,
				"credentials directory required with a block backend: set KACHO_STORAGE_BLOCK_BACKEND_CREDENTIALS_DIR "+
					"— StorageBackend carries a REFERENCE to credential material, never the material itself, and an "+
					"unresolvable reference makes every provisioning call fail in a way that looks like backend "+
					"unavailability rather than misconfiguration")
		}
		if c.BlockBackendCallTimeout <= 0 {
			problems = append(problems,
				"per-call timeout required with a block backend: set KACHO_STORAGE_BLOCK_BACKEND_CALL_TIMEOUT > 0 "+
					"— an unbounded call to an unresponsive backend parks the reconciler slot forever, and the "+
					"backlog it holds is invisible")
		}
		if c.BlockBackendReconcileInterval <= 0 || c.BlockBackendReconcileBatch <= 0 {
			problems = append(problems,
				"reconciler cadence required with a block backend: set "+
					"KACHO_STORAGE_BLOCK_BACKEND_RECONCILE_INTERVAL > 0 and "+
					"KACHO_STORAGE_BLOCK_BACKEND_RECONCILE_BATCH > 0 — without the reconciler nothing ever moves "+
					"a resource from CREATING to READY, and every volume stays pending forever while the service "+
					"reports itself healthy")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s mode refuses insecure config: %s", mode, strings.Join(problems, "; "))
	}
	return nil
}
