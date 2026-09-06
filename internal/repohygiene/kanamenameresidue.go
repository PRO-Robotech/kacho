// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// kanamenameresidue.go — имя ПЛАТФОРМЫ на поверхности, которой продукт Kaname
// называет СЕБЯ: перепись по шести осям с двумя величинами на каждую.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Пункт 3 предиката готовности линии (эпик #2119) требует: «ноль чужого бренда
// там, где продукт называет себя» — контракт, схема, таблицы, ручки, клеймы
// удостоверения, витрина оператора. Шесть поверхностей названы владельцем
// поимённо (П3 решения 2026-09-06, замысел перехода §0.1).
//
// У этого условия НЕ БЫЛО ПРОИЗВОДИТЕЛЯ. Держатель `sev-name-residue` пакета
// контура `standalone-iam` объявлен без исполнителя, поэтому исход шести
// сценариев приёмки был «не выполнилось», а не зелёный. Разовая перепись
// производителем не является: она стареет молча, а «ноль находок» неотличимо
// от «ноль прочитанного».
//
// Норма разделения, из которой выведена каждая ось: Kaname наследует от Kachō
// КОД, но не ИМЯ. Проверочный вопрос — «это имя, которым продукт себя называет,
// или код, который он исполняет?»; первое своё, второе берётся как есть.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАСПОЗНАВАТЕЛЬ ЗНАЕТ ОБЕ ЛАТИНСКИЕ ФОРМЫ ИМЕНИ, И ЭТО НЕСУЩЕЕ
//
// Имя платформы записывается в дереве ДВУМЯ латинскими формами: обычной и с
// диакритическим знаком. Предикат, знающий одну, недобирает МОЛЧА:
//
//	printf 'Kachō\n' | grep -ci kacho     # → 0
//
// Замер поверхности на вершине линии a36563df96 (2026-09-06): файлов, несущих
// диакритическую форму, — 78; из них ШЕСТНАДЦАТЬ не несут обычной формы вовсе,
// то есть перепись на ASCII-предикате объявила бы их чистыми. Пять из
// шестнадцати — страницы клиентской документации службы, где бренд платформы
// стоит как СВОЁ имя продукта.
//
// Поэтому форма ищется посимвольно (platformNameAt), последняя позиция
// принимает обе записи, и перепись печатает ОБА числа врозь плюс число файлов,
// невидимых ASCII-предикату. Доказано инъекцией по каждой форме отдельно.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПО ОСЯМ, И ПО КАЖДОЙ ДВЕ ВЕЛИЧИНЫ
//
// Одно число скрывает ровно тот случай, ради которого держатель заведён.
// Поэтому у каждой полосы печатаются ОБЕ: «предложено распознавателю» —
// сколько вхождений дошло до её правила, и «признано её предметом» — сколько
// она забрала. Правила упорядочены, поэтому «предложено 0» означает, что
// правило полосы не исполнялось НИ РАЗУ: ноль находок по ней читается как «не
// искали», и это ОТКАЗ, а не тихий успех.
//
// «Признано 0» при «предложено больше нуля» — законный ноль, и он тоже
// печатается: полоса читалась и ничего не нашла.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦЫ — ВНЕ ШЕСТИ ОСЕЙ, НО С ЧИСЛОМ У КАЖДОЙ
//
// Держатель судит ШЕСТЬ осей П3. Вхождение, к ним не относящееся, не
// замалчивается, а попадает в названную границу со своим числом:
//
//	Б1 путь модуля фундамента              предмет П4 (вынос фундамента), не П3
//	Б2 координата контракта фундамента     предмет П4: граница фундамента
//	Б3 имя чужого модуля платформы         решено остаться (эпик #2076)
//	Б4 одобренная приёмка                  правка отзывает вердикт по отпечатку
//	Б5 функция общего фундамента в схеме   решено остаться: один шаблон на шесть
//	                                       владельцев, заведена ПРИМЕНЁННОЙ
//	                                       миграцией (ban #5)
//	Б6 форма неизвестна распознавателю     СЛЕПАЯ ЗОНА; число точное, его рост
//	                                       означает, что появилась форма записи,
//	                                       которой держатель не судит
//
// Число Б6 стоит в ведомости долга наравне с осями именно поэтому: молчание по
// неизвестной форме неотличимо от молчания мёртвой проверки.
//
// Полоса называет ФОРМУ записи, а не владельца. Owner в строке остатка — тот,
// кому принадлежит ПОДАВЛЯЮЩАЯ часть полосы; отдельные вхождения внутри полосы
// могут принадлежать другому предмету, и это не дефект классификации, а её
// объявленная точность: держатель считает, а адъюдикацию по одному ведут
// записями ведомости решённого остаться.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ДЕРЖАТЕЛЬ НЕ ВИДИТ — НАЗВАНО ЧИСЛОМ, А НЕ ОГОВОРКОЙ
//
// Он судит ДЕРЕВО, спрошенное у индекса git, и молчит о том, что рождается
// позже:
//
//  1. имя, СОБРАННОЕ при рендере шаблона (подстановка `{{ .Values… }}`,
//     форматная строка, склейка) — образцу не видно by construction. Кандидаты
//     такой формы считаются отдельным числом (Assembled) и печатаются: рост
//     означает, что предмет уезжает в невидимую держателю форму;
//  2. имя внутри РАСКОДИРОВАННОГО удостоверения и внутри значения, закодированного
//     в дереве (base64, процентное кодирование, `\uXXXX`). Замер по поверхности:
//     таких форм ПО НУЛЮ у каждой из трёх; предикаты названы в пробе
//     TestKanameNameResidueEncodedFormsAreAbsentFromTheSurface — она и есть
//     наблюдатель этой границы, а не эта строка;
//  3. имя платформы в самом ПУТИ файла: держатель читает СОДЕРЖИМОЕ, а путь
//     кладёт в отдельное число (FilesWithNameInPath). Путь — предмет полос
//     раскладки и переезда контракта (#2133), и судить его здесь значило бы
//     завести второе место об одном предмете; но молчать о нём нельзя, иначе
//     «ноль находок» по содержимому читалось бы как «имени нет вовсе»;
//  4. ДВОИЧНЫЕ файлы: текста в них нет. Число печатается;
//  5. ИСТИННОСТЬ адъюдикации ведомости. Что запись названа «историческим
//     свидетельством» верно — проверяет человек. Держатель судит лишь то, что у
//     записи есть предмет и что её число сошлось с фактом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВЕ ВЕДОМОСТИ, И ОНИ О РАЗНОМ
//
// KanameNameResidueStay — РЕШЁННОЕ ОСТАТЬСЯ. Перечнем «путь + полоса + точное
// число + причина», а не признаком формы: признак («файл объявляет константу»)
// освободил бы и тех, кого держатель заводился ловить. Запись, которой нечего
// прощать, — НАХОДКА: освобождение обязано истекать вместе со своим предметом.
//
// KanameNameResidueDebt — ОСТАТОК, который ещё предстоит снять. Точное число
// вхождений и файлов на каждую полосу, а не потолок: потолок не краснеет
// никогда, поэтому не истекает и прощает вперёд ту находку, ради которой
// держатель заведён. Расхождение в ЛЮБУЮ сторону — находка: вверх означает
// рост остатка, вниз — что ведомость отстала от дерева и её обязано опустить то
// же изменение, которое остаток снизило.
//
// Полосы, чьё переименование ведут ДРУГИЕ задачи линии, из ведомости не
// изымаются: держатель обязан их видеть, а чинит их владелец полосы. У каждой
// строки долга назван Owner — чей это предмет.
//
// Четыре причины «остаться», названные задачей #2126, разложены так: имя чужого
// модуля платформы — граница Б3; функция общего фундамента внутри схемы — Б5;
// историческое свидетельство — Б3 (имена прежних репозиториев) и Б4 (записи
// замеров в одобренных приёмках). Четвёртая, УПОМИНАНИЕ ЗАВИСИМОСТИ В ПРОЗЕ,
// границей НЕ сделана намеренно: отличить «служба зависит от фундамента Kachō»
// от «страница службы называет себя чужим брендом» формой нельзя — это
// адъюдикация, и её ведут ПО ОДНОМУ, записью в KanameNameResidueStay. Сегодня
// такие вхождения лежат в остатке, то есть названы неразобранными, а не
// прощены оптом. Названо вслух, чтобы «прощено 0» по этой причине не читалось
// как «таких вхождений нет».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗЕЛЁНЫЙ ПРОГОН НЕ ОЗНАЧАЕТ, ЧТО УСЛОВИЕ П3 ВЫПОЛНЕНО
//
// Сказано первым, чтобы «PASS» не читалось шире сделанного. Зелёный означает
// ровно одно: остаток НЕ ВЫРОС против записанного. Условие «ноль чужого имени
// там, где продукт называет себя» выполнено тогда и только тогда, когда
// ведомость остатка ПУСТА, — и это отдельное, машинно проверяемое утверждение
// (KanameNameResidueOutstanding даёт его числом, а перепись печатает словами).

import (
	"fmt"
	"sort"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// поверхность

// KanameSurface — каталоги, которыми продукт Kaname называет себя: дерево
// службы, контракт доступа и словарь его аннотаций, чарт оператора.
//
// Перечень объявлен ОДИН раз: и обход, и перепись, и инъекция читают отсюда.
// Каталог, под которым индекс не даёт ни одного файла, — ОТКАЗ, а не тихий
// пропуск: иначе переезд каталога завёл бы слепую зону молча.
var KanameSurface = []string{
	"deploy/helm/umbrella/charts/kaname",
	"proto/kacho/cloud/iam",
	"proto/kacho/iam",
	"services/iam",
}

// kanameApprovedAcceptanceDir — каталог одобренных приёмок службы. Правка
// одобренного документа ОТЗЫВАЕТ его вердикт: одобрение относится к точному
// содержимому, а не к файлу по имени.
const kanameApprovedAcceptanceDir = "services/iam/docs/engineering/acceptance/"

// ─────────────────────────────────────────────────────────────────────────────
// имя платформы: обе латинские формы

// platformNameAt — длина имени платформы в рунах, начиная с позиции i, и
// признак диакритической формы. Ноль — имени здесь нет.
//
// Форма ищется ПОСИМВОЛЬНО, а не образцом с классом символов: последняя позиция
// и есть весь предмет расхождения двух записей, и она обязана читаться в коде
// глазом, а не выводиться из флага регистронезависимости.
func platformNameAt(rs []rune, i int) (length int, macron bool) {
	const stem = "kach"
	if i+len(stem)+1 > len(rs) {
		return 0, false
	}
	for k, want := range stem {
		if lowerASCII(rs[i+k]) != want {
			return 0, false
		}
	}
	switch rs[i+len(stem)] {
	case 'o', 'O':
		return len(stem) + 1, false
	case 'ō', 'Ō':
		return len(stem) + 1, true
	}
	return 0, false
}

func lowerASCII(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// isResidueTokenRune — руна, продолжающая ТОКЕН, в котором стоит имя.
//
// Только латиница, цифры и разделители координат: кириллица сюда не входит
// намеренно, иначе токен склеился бы с соседним русским словом прозы и
// классификация судила бы предложение вместо координаты.
func isResidueTokenRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '_' || r == '.' || r == ':' || r == '/' || r == '-':
		return true
	case r == 'ō' || r == 'Ō':
		return true
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// оси, полосы и правила

// NameResidueAxis — одна из шести поверхностей П3.
type NameResidueAxis string

// Шесть осей. Порядок объявления — порядок печати переписи.
const (
	AxisContract NameResidueAxis = "контракт"
	AxisSchema   NameResidueAxis = "схема"
	AxisTables   NameResidueAxis = "таблицы"
	AxisKnobs    NameResidueAxis = "ручки"
	AxisClaims   NameResidueAxis = "клеймы удостоверения"
	AxisShowcase NameResidueAxis = "витрина оператора"
	// axisBorder — не ось: вхождение вне шести поверхностей П3.
	axisBorder NameResidueAxis = "(вне шести осей)"
)

// KanameAxes — шесть осей П3 в порядке директивы владельца.
var KanameAxes = []NameResidueAxis{
	AxisContract, AxisSchema, AxisTables, AxisKnobs, AxisClaims, AxisShowcase,
}

// residueLane — полоса переписи: своя форма записи имени, свой владелец, своя
// починка.
type residueLane struct {
	// ID — имя полосы, ключ обеих ведомостей и переписи.
	ID string
	// Axis — ось П3 либо axisBorder.
	Axis NameResidueAxis
	// Why — для границы: почему полоса не судится осями.
	Why string
}

// Полосы. Имена — ключи ведомостей, поэтому объявлены константами: ведомость,
// назвавшая полосу опечаткой, прощала бы вникуда.
const (
	laneContractCoordinate = "координата пакета доступа"
	laneSchemaName         = "имя схемы"
	laneDatabaseName       = "имя базы"
	laneQualifiedTable     = "квалифицированное имя таблицы"
	laneEnvKnob            = "переменная окружения"
	laneChartKnob          = "ключ профиля и шаблона"
	// Имя намеренно НЕ содержит слова, по которому статический анализатор
	// безопасности узнаёт зашитый секрет: полоса называет РОД ЗАПИСИ, а не
	// значение, и находка «зашитые учётные данные» на имени полосы переписи
	// была бы ложной. Читаемое имя полосы стоит в значении.
	laneClaimAssertion  = "утверждение токена"
	laneIdentityHeader  = "заголовок переданной личности"
	laneClusterAnchor   = "якорь кластера"
	laneSchemaPrefixKin = "сосед по приставке схемы"
	laneDomainAddress   = "домен и адрес"
	laneObjectName      = "имя объекта"
	laneBrandInText     = "бренд в тексте"
	lanePlatformName    = "имя платформы"

	borderFoundationModule   = "Б1 путь модуля фундамента"
	borderFoundationContract = "Б2 координата контракта фундамента"
	borderForeignModule      = "Б3 имя чужого модуля платформы"
	borderApprovedAcceptance = "Б4 одобренная приёмка"
	borderFoundationFunction = "Б5 функция общего фундамента внутри схемы"
	borderUnknownForm        = "Б6 форма неизвестна распознавателю"
)

// KanameForeignPlatformModules — ЗАКРЫТЫЙ перечень имён чужих модулей
// платформы. Решено остаться (эпик #2076 §«ЧТО НЕ ТРОГАЕТСЯ НИКОГДА» плюс
// «имена чужих модулей платформы»): это имена ДРУГИХ продуктов, и Kaname их не
// переименовывает.
//
// Перечень ЗАКРЫТ намеренно. Правило вида «всякое `kacho-<слово>` — чужой
// модуль» прощало бы `kacho-migrator`, `kacho-bootstrap-admin` и
// `kacho-umbrella-pg-iam` — то есть СОБСТВЕННЫЕ объекты службы, ради которых
// ось витрины и заведена.
var KanameForeignPlatformModules = map[string]bool{
	"kacho-api-gateway":          true,
	"kacho-compute":              true,
	"kacho-corelib":              true,
	"kacho-deploy":               true,
	"kacho-geo":                  true,
	"kacho-iam":                  true,
	"kacho-iam-polyrepo-archive": true,
	"kacho-nlb":                  true,
	"kacho-proto":                true,
	"kacho-registry":             true,
	"kacho-storage":              true,
	"kacho-test":                 true,
	"kacho-ui":                   true,
	"kacho-vpc":                  true,
	"kacho-vpc-implement":        true,
	"kacho-vpc-operator":         true,
	"kacho-workspace":            true,
}

// KanameFoundationSchemaFunctions — ЗАКРЫТЫЙ перечень функций общего фундамента
// внутри схемы службы.
//
// Решено остаться, и довод не наш: они рендерятся ОДНИМ шаблоном на шесть
// владельцев, а их байт-идентичность держит отдельный гейт. Сверх того они
// заведены ПРИМЕНЁННОЙ миграцией службы, которую править нельзя (ban #5).
var KanameFoundationSchemaFunctions = map[string]bool{
	"kacho_admission_rate_count":    true,
	"kacho_labels_valid":            true,
	"kacho_quota_admit":             true,
	"kacho_quota_carrier_lifecycle": true,
	"kacho_quota_count":             true,
	"kacho_quota_refuse":            true,
	"kacho_rate_refuse":             true,
}

// residueHit — одно вхождение имени и его окрестность.
//
// Разбор идёт по СЕГМЕНТУ пути, а не по всему токену: у адреса
// `spiffe://kacho.cloud/ns/kacho/sa/kacho-vpc` три вхождения с тремя разными
// смыслами — домен доверия, пространство имён и чужой модуль, — и распознаватель
// по целому токену свёл бы их в одно.
type NameResidueHit struct {
	Path string
	Line int
	// Text — токен целиком: то, что читатель находки увидит в дереве.
	Text string
	// Seg — сегмент пути, содержащий вхождение; SegPre и SegRest — что стоит в
	// сегменте до и после имени.
	Seg, SegPre, SegRest string
	// PathPre и PathRest — сегменты токена до и после текущего.
	PathPre, PathRest string
	// Hit — имя, КАК ЗАПИСАНО в дереве; Macron — диакритическая ли форма.
	Hit    string
	Macron bool
	// LinePrefix — вся строка слева от вхождения: по ней узнаётся значение
	// ключа профиля, которое сегментом пути не выражается.
	LinePrefix string
	// InChart — файл принадлежит чарту оператора.
	InChart bool
	// Lane — полоса, признавшая вхождение своим предметом.
	Lane string
}

type residueRule struct {
	Lane  string
	Match func(h NameResidueHit) bool
}

// kanameResidueRules — правила В ПОРЯДКЕ применения; первое совпавшее забирает
// вхождение.
//
// Порядок несущий, а не косметический. Три места, где он решает исход:
//
//   - якорь кластера стоит ДО утверждения токена: `cluster_kacho_root` имеет
//     форму `kacho_<слово>` и без этого правила уехал бы в клеймы — 402
//     вхождения не на своей оси;
//   - функция фундамента стоит ДО якоря и клейм: она тоже `kacho_<слово>`, но
//     решена остаться;
//   - координата контракта СЛУЖБЫ стоит до координаты контракта ФУНДАМЕНТА:
//     обе начинаются одинаково, различает их домен.
var kanameResidueRules = []residueRule{
	{borderApprovedAcceptance, func(h NameResidueHit) bool {
		return strings.HasPrefix(h.Path, kanameApprovedAcceptanceDir)
	}},
	{borderFoundationModule, func(h NameResidueHit) bool {
		return strings.HasSuffix(h.PathPre, "PRO-Robotech/") && h.Seg == "kacho"
	}},
	{laneContractCoordinate, func(h NameResidueHit) bool {
		domain, ok := contractDomainOf(h)
		return ok && isAccessContractDomain(domain)
	}},
	{borderFoundationContract, func(h NameResidueHit) bool {
		_, ok := contractDomainOf(h)
		return ok
	}},
	{laneQualifiedTable, func(h NameResidueHit) bool {
		rest, ok := schemaPrefixRest(h)
		return ok && strings.HasPrefix(rest, ".")
	}},
	{laneDatabaseName, func(h NameResidueHit) bool {
		rest, ok := schemaPrefixRest(h)
		if !ok || rest != "" {
			return false
		}
		return strings.HasSuffix(h.SegPre, "/") || strings.HasSuffix(h.PathPre, "/") ||
			isProfileDatabaseKey(h.LinePrefix)
	}},
	{laneSchemaName, func(h NameResidueHit) bool {
		rest, ok := schemaPrefixRest(h)
		return ok && rest == ""
	}},
	{laneSchemaPrefixKin, func(h NameResidueHit) bool {
		rest, ok := schemaPrefixRest(h)
		return ok && strings.HasPrefix(rest, "_")
	}},
	{borderFoundationFunction, func(h NameResidueHit) bool {
		return KanameFoundationSchemaFunctions[h.Seg]
	}},
	{laneClusterAnchor, func(h NameResidueHit) bool {
		return strings.HasSuffix(h.SegPre, "cluster_") && strings.HasPrefix(h.SegRest, "_root")
	}},
	{laneEnvKnob, func(h NameResidueHit) bool {
		return h.Hit == "KACHO" && strings.HasPrefix(h.SegRest, "_")
	}},
	{laneClaimAssertion, func(h NameResidueHit) bool {
		return h.Hit == "kacho" && len(h.SegRest) > 1 && h.SegRest[0] == '_' &&
			isLowerWordByte(h.SegRest[1])
	}},
	{laneIdentityHeader, func(h NameResidueHit) bool {
		return strings.HasSuffix(strings.ToLower(h.SegPre), "x-") &&
			len(h.SegRest) > 1 && h.SegRest[0] == '-' && isLowerWordByte(lowerByte(h.SegRest[1]))
	}},
	{borderForeignModule, func(h NameResidueHit) bool {
		return KanameForeignPlatformModules[h.Seg]
	}},
	{laneChartKnob, func(h NameResidueHit) bool {
		if !h.InChart {
			return false
		}
		return strings.HasSuffix(h.SegPre, ".Values.") || strings.HasSuffix(h.SegPre, "global.") ||
			(len(h.SegRest) > 1 && h.SegRest[0] == '.' && isASCIILetter(h.SegRest[1]))
	}},
	{laneDomainAddress, func(h NameResidueHit) bool {
		for _, suffix := range [...]string{".cloud", ".local", ".svc"} {
			if strings.HasPrefix(h.SegRest, suffix) {
				return true
			}
		}
		return false
	}},
	{laneObjectName, func(h NameResidueHit) bool {
		return !h.Macron &&
			(strings.HasPrefix(h.SegRest, "-") || strings.HasSuffix(h.SegPre, "-"))
	}},
	{laneBrandInText, func(h NameResidueHit) bool { return h.Macron }},
	{lanePlatformName, func(h NameResidueHit) bool { return strings.EqualFold(h.Seg, "kacho") }},
	{laneObjectName, func(h NameResidueHit) bool {
		return len(h.SegRest) > 1 && (h.SegRest[0] == '.' || h.SegRest[0] == ':') &&
			isASCIILetter(h.SegRest[1])
	}},
	{borderUnknownForm, func(NameResidueHit) bool { return true }},
}

// kanameLanes — полоса → ось. Выведено из правил, а не выписано вторым местом:
// два перечня об одном предмете разошлись бы молча.
var kanameLanes = map[string]residueLane{
	laneContractCoordinate: {laneContractCoordinate, AxisContract, ""},
	laneSchemaName:         {laneSchemaName, AxisSchema, ""},
	laneDatabaseName:       {laneDatabaseName, AxisSchema, ""},
	laneQualifiedTable:     {laneQualifiedTable, AxisTables, ""},
	laneEnvKnob:            {laneEnvKnob, AxisKnobs, ""},
	laneChartKnob:          {laneChartKnob, AxisKnobs, ""},
	laneClaimAssertion:     {laneClaimAssertion, AxisClaims, ""},
	laneIdentityHeader:     {laneIdentityHeader, AxisClaims, ""},
	laneClusterAnchor:      {laneClusterAnchor, AxisShowcase, ""},
	laneSchemaPrefixKin:    {laneSchemaPrefixKin, AxisShowcase, ""},
	laneDomainAddress:      {laneDomainAddress, AxisShowcase, ""},
	laneObjectName:         {laneObjectName, AxisShowcase, ""},
	laneBrandInText:        {laneBrandInText, AxisShowcase, ""},
	lanePlatformName:       {lanePlatformName, AxisShowcase, ""},

	borderFoundationModule: {borderFoundationModule, axisBorder,
		"предмет П4 — вынос фундамента в свой модуль, а не П3"},
	borderFoundationContract: {borderFoundationContract, axisBorder,
		"предмет П4 — граница фундамента: контракт платформы, а не службы"},
	borderForeignModule: {borderForeignModule, axisBorder,
		"решено остаться: имя ЧУЖОГО продукта, Kaname его не переименовывает"},
	borderApprovedAcceptance: {borderApprovedAcceptance, axisBorder,
		"правка одобренного документа отзывает вердикт, привязанный к отпечатку"},
	borderFoundationFunction: {borderFoundationFunction, axisBorder,
		"решено остаться: один шаблон на шесть владельцев, заведена применённой миграцией"},
	borderUnknownForm: {borderUnknownForm, axisBorder,
		"СЛЕПАЯ ЗОНА: форма записи, которой держатель не судит; число точное"},
}

// contractDomainOf — координата контракта: точечная форма `kacho.cloud.<домен>`
// либо путь `kacho/cloud/<домен>`, а также словарь аннотаций `kacho.iam.<…>`.
func contractDomainOf(h NameResidueHit) (string, bool) {
	if rest, ok := trimAny(h.SegRest, ".cloud.", ".iam."); ok {
		return rest, true
	}
	if h.Seg == "kacho" {
		if rest, ok := trimAny(h.PathRest, "/cloud/", "/iam/"); ok {
			return rest, true
		}
	}
	return "", false
}

// isAccessContractDomain — домен принадлежит контракту ДОСТУПА (пакет службы
// либо словарь её аннотаций авторизации).
func isAccessContractDomain(rest string) bool {
	return hasWordPrefix(rest, "iam") || hasWordPrefix(rest, "authz")
}

// trimAny — если строка начинается с одного из образцов, вернуть остаток.
func trimAny(s string, prefixes ...string) (string, bool) {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return s[len(p):], true
		}
	}
	return "", false
}

// hasWordPrefix — строка начинается словом word, а не более длинным словом с
// таким началом.
func hasWordPrefix(s, word string) bool {
	if !strings.HasPrefix(s, word) {
		return false
	}
	if len(s) == len(word) {
		return true
	}
	c := s[len(word)]
	return !isLowerWordByte(c) && c != '_'
}

// schemaPrefixRest — вхождение есть приставка схемы `kacho_iam`; вернуть, что
// стоит следом.
func schemaPrefixRest(h NameResidueHit) (string, bool) {
	const suffix = "_iam"
	if h.Hit != "kacho" || !strings.HasPrefix(h.SegRest, suffix) {
		return "", false
	}
	return h.SegRest[len(suffix):], true
}

// isProfileDatabaseKey — слева от вхождения стоит РОВНО ключ профиля со своим
// отступом: `  name: ` либо `  database: `. Судится весь отступ, иначе под
// правило попала бы проза, где те же слова стоят посреди предложения.
func isProfileDatabaseKey(prefix string) bool {
	rest := strings.TrimLeft(prefix, " \t-")
	for _, key := range [...]string{"name:", "database:"} {
		if strings.HasPrefix(rest, key) && strings.TrimLeft(rest[len(key):], " ") == "" {
			return true
		}
	}
	return false
}

func isLowerWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func lowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// ─────────────────────────────────────────────────────────────────────────────
// ведомости

// NameResidueStay — запись ведомости РЕШЁННОГО ОСТАТЬСЯ: перечнем, а не
// признаком формы.
type NameResidueStay struct {
	// Path — отслеживаемый путь от корня дерева.
	Path string
	// Lane — полоса, на которой вхождения прощаются. Ключ пары: файл вправе
	// нести законное имя на одной полосе и долг на другой.
	Lane string
	// Count — ТОЧНОЕ число прощаемых вхождений, а не потолок.
	Count int
	// Reason — почему имя платформы здесь обязано остаться.
	Reason string
}

// NameResidueDebt — строка ведомости ОСТАТКА: сколько вхождений и файлов полоса
// несёт СЕГОДНЯ. Точное число; расхождение в любую сторону — находка.
type NameResidueDebt struct {
	Lane        string
	Occurrences int
	Files       int
	// Owner — чей это предмет: задача, которая полосу снимет.
	Owner string
}

// NameResidueLedgerFinding — запись ведомости, пережившая свой предмет либо
// разошедшаяся с фактом.
type NameResidueLedgerFinding struct {
	Ledger string
	Lane   string
	Path   string
	Want   int
	Got    int
	Why    string
}

// KanameNameResidueStay — ведомость РЕШЁННОГО ОСТАТЬСЯ.
//
// Каждая запись отвечает одному проверяемому критерию: сними имя платформы — и
// файл перестанет делать то, ради чего написан. Разбор устройства — в шапке.
var KanameNameResidueStay = []NameResidueStay{
	{
		Path: "services/iam/cmd/kaname/schema_guard.go", Lane: laneSchemaName, Count: 1,
		Reason: "страж старта, отказывающий на базе прежней установки (решение Р11): " +
			"отставленное имя схемы — его ПРЕДМЕТ, а не наследие. Перестанет " +
			"встречаться — отличить прежнюю установку от чистой будет нечем",
	},
	{
		Path: "services/iam/cmd/kaname/schema_guard_integration_test.go", Lane: laneSchemaName, Count: 1,
		Reason: "проба стража: поднимает базу с прежней схемой и требует отказа старта",
	},
	{
		Path: "services/iam/cmd/kaname/schema_raised_from_scratch_integration_test.go", Lane: laneSchemaName, Count: 1,
		Reason: "проба чистой установки: положительный близнец стража, требует ОТСУТСТВИЯ прежней схемы",
	},
	{
		Path: "services/iam/cmd/kaname/schema_guard_test.go", Lane: laneSchemaPrefixKin, Count: 5,
		Reason: "проба текста отказа: имя прежней БАЗЫ обязано стоять в жалобе поимённо, " +
			"иначе оператор не поймёт, где искать (отказ при refuse-to-start выведен " +
			"из-под запрета §«Публичные артефакты»)",
	},
	{
		Path: "services/iam/docs/content/install/deploy.mdx", Lane: laneSchemaName, Count: 4,
		Reason: "инструкция установки, к которой отсылает текст отказа: она обязана назвать " +
			"схему, которую оператор увидел в жалобе, иначе он придёт по ссылке и своего случая не найдёт",
	},
	{
		Path: "services/iam/internal/supplyhygiene/schema_name_test.go", Lane: laneSchemaName, Count: 1,
		Reason: "сама проверка имени схемы: отставленное имя — её ВХОД, без него у " +
			"распознавателя нет предмета",
	},
	{
		Path: "services/iam/internal/supplyhygiene/schema_name_test.go", Lane: laneDatabaseName, Count: 1,
		Reason: "та же проверка: различение схемы и базы требует называть обе формы записи имени базы",
	},
	{
		Path: "services/iam/internal/supplyhygiene/schema_name_test.go", Lane: laneQualifiedTable, Count: 1,
		Reason: "та же проверка: квалифицированное имя таблицы — форма, которую её распознаватель обязан знать",
	},
	{
		Path: "services/iam/internal/supplyhygiene/schema_name_test.go", Lane: laneSchemaPrefixKin, Count: 2,
		Reason: "та же проверка: соседи по приставке названы, чтобы пропуск был отличим от находки",
	},
	{
		Path: "services/iam/internal/supplyhygiene/schema_name_injection_test.go", Lane: laneSchemaName, Count: 5,
		Reason: "доказательство той проверки инъекцией: дефект вносится отставленным именем",
	},
	{
		Path: "services/iam/internal/supplyhygiene/schema_name_injection_test.go", Lane: laneDatabaseName, Count: 1,
		Reason: "то же доказательство: инъекция по форме имени базы",
	},
	{
		Path: "services/iam/internal/supplyhygiene/schema_name_injection_test.go", Lane: laneQualifiedTable, Count: 2,
		Reason: "то же доказательство: инъекция по форме квалифицированного имени таблицы",
	},
	{
		Path: "services/iam/internal/supplyhygiene/schema_name_injection_test.go", Lane: laneSchemaPrefixKin, Count: 3,
		Reason: "то же доказательство: законный близнец — сосед по приставке, на котором проверка обязана молчать",
	},
	{
		Path: "services/iam/internal/migrations/notify_channel_consumer_injection_test.go", Lane: laneSchemaPrefixKin, Count: 2,
		Reason: "синтетическая фикстура инъекции: канал прежнего имени вносится как ДЕФЕКТ, " +
			"на котором разбор обязан краснеть",
	},
	{
		Path: "services/iam/internal/migrations/notify_channel_has_a_listener_integration_test.go", Lane: laneSchemaPrefixKin, Count: 4,
		Reason: "отрицательный контроль: проба требует, чтобы канала прежнего имени в базе " +
			"НЕ БЫЛО; сними имя — и утверждение станет вакуумным",
	},
}

// KanameNameResidueDebt — ведомость ОСТАТКА по полосам.
//
// Замер снят на вершине накопительной линии a36563df96 (2026-09-06). Числа
// ТОЧНЫЕ и снимаются тем же изменением, которое снимает остаток; повторить их
// можно прогоном самой проверки — она печатает перепись и на зелёном.
var KanameNameResidueDebt = []NameResidueDebt{
	{laneContractCoordinate, 1269, 398, "#2133 — имя пакета контракта следует за продуктом (Р14)"},
	{laneSchemaName, 2, 2, "#2128 — контракт называет схему в комментариях"},
	{laneDatabaseName, 0, 0, "снято: имя базы переименовано вместе со схемой"},
	{laneQualifiedTable, 14, 4, "#2128 — контракт называет таблицы в комментариях"},
	{laneEnvKnob, 62, 39, "линия дебрендинга: ручки службы"},
	{laneChartKnob, 99, 12, "линия дебрендинга: ключи чарта оператора"},
	{laneClaimAssertion, 339, 60, "О1 эпика #2076 — межрепозиторный контракт, требует окна двух написаний"},
	{laneIdentityHeader, 83, 41, "Р10 №1 — заголовки переданной личности"},
	{laneClusterAnchor, 402, 141, "#2113 и Р10 №3 — написание якоря; держатель соседний"},
	{laneSchemaPrefixKin, 9, 7, "Р5 эпика #2076 — пространство метрик и канал уведомления"},
	{laneDomainAddress, 590, 173, "Р10 №2 — домен доверия и адреса стенда"},
	{laneObjectName, 676, 243, "Р3 эпика #2076 — витрина оператора"},
	{laneBrandInText, 100, 77, "Р3 эпика #2076 — бренд в прозе и на клиентских страницах"},
	{lanePlatformName, 627, 287, "Р3 эпика #2076 — имя платформы отдельным словом"},
	{borderUnknownForm, 73, 29, "слепая зона распознавателя: рост числа означает новую форму записи"},
}

// KanameNameResidueOutstanding — сколько вхождений имени платформы ведомость
// остатка признаёт НЕСНЯТЫМИ, и на скольких полосах.
//
// Существует ради одного утверждения: условие П3 выполнено ровно тогда, когда
// оба числа равны нулю. Зелёный прогон держателя этого НЕ утверждает.
func KanameNameResidueOutstanding() (occurrences, lanes int) {
	for _, row := range KanameNameResidueDebt {
		if row.Occurrences == 0 {
			continue
		}
		occurrences += row.Occurrences
		lanes++
	}
	return occurrences, lanes
}

// ─────────────────────────────────────────────────────────────────────────────
// перепись

// NameResidueCensus — объём осмотренного. Печатается ВСЕГДА, включая зелёный
// прогон.
type NameResidueCensus struct {
	FilesTracked     int
	FilesRead        int
	FilesBinary      int
	FilesEmpty       int
	Occurrences      int
	OccurrencesASCII int
	// OccurrencesMacron — вхождений диакритической формы: то, чего ASCII-предикат
	// не видит вовсе.
	OccurrencesMacron int
	FilesASCIIOnly    int
	// FilesMacronOnly — файлов, невидимых ASCII-предикату ЦЕЛИКОМ. Это и есть
	// величина слепой зоны односторонней переписи.
	FilesMacronOnly int
	FilesBothForms  int
	// Assembled — кандидатов «имя собирается при рендере»: образцу не видны.
	Assembled int
	// FilesWithNameInPath — файлов, чей ПУТЬ несёт имя платформы. Содержимое
	// таких файлов судится как у всех; сам путь — предмет других полос.
	FilesWithNameInPath int
	// OfferedByLane — вхождений, ДОШЕДШИХ до правила полосы. Ноль означает, что
	// правило не исполнялось ни разу.
	OfferedByLane map[string]int
	// FoundByLane / FilesByLane — признано предметом полосы, ПОСЛЕ ведомости
	// решённого остаться.
	FoundByLane    map[string]int
	FilesByLane    map[string]int
	ForgivenByLane map[string]int
	// FilesBySurface — прочитано файлов под каждым каталогом поверхности.
	FilesBySurface map[string]int
}

// AxisOccurrences — сумма находок по оси.
func (c NameResidueCensus) AxisOccurrences(axis NameResidueAxis) int {
	n := 0
	for lane, lg := range kanameLanes {
		if lg.Axis == axis {
			n += c.FoundByLane[lane]
		}
	}
	return n
}

// AxisOffered — сколько вхождений дошло хотя бы до одного правила оси. Ноль
// означает, что ось НЕ ЧИТАЛАСЬ, и «найдено 0» по ней ничего не утверждает.
func (c NameResidueCensus) AxisOffered(axis NameResidueAxis) int {
	best := 0
	for lane, lg := range kanameLanes {
		if lg.Axis == axis && c.OfferedByLane[lane] > best {
			best = c.OfferedByLane[lane]
		}
	}
	return best
}

// assembledMarkers — признаки того, что имя собирается при рендере: подстановка
// шаблона, форматная строка, склейка.
var assembledMarkers = [...]string{"{{", "${", "%s", "%v", "$(", "\" +", "' +"}

// FindKanameNameResidue разбирает ПРОИЗВОЛЬНЫЙ корпус (путь → содержимое):
// настоящее дерево и синтетический мир инъекции проходят одну функцию, поэтому
// доказанное на втором верно для первого.
func FindKanameNameResidue(
	files map[string][]byte,
	stay []NameResidueStay,
	debt []NameResidueDebt,
) ([]NameResidueHit, []NameResidueLedgerFinding, NameResidueCensus, error) {
	census := NameResidueCensus{
		FilesTracked:   len(files),
		OfferedByLane:  map[string]int{},
		FoundByLane:    map[string]int{},
		FilesByLane:    map[string]int{},
		ForgivenByLane: map[string]int{},
		FilesBySurface: map[string]int{},
	}
	if len(files) == 0 {
		return nil, nil, census, fmt.Errorf(
			"на вход подано ноль файлов — обход дерева не состоялся, и молчание " +
				"держателя ничего не утверждает")
	}
	if err := validateStayLedgerShape(stay); err != nil {
		return nil, nil, census, err
	}
	if err := validateDebtLedgerShape(debt); err != nil {
		return nil, nil, census, err
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var all []NameResidueHit
	perFileLane := map[string]map[string]int{} // путь → полоса → вхождений
	for _, path := range paths {
		body := files[path]
		switch {
		case len(body) == 0:
			census.FilesEmpty++
			continue
		case containsNUL(body):
			census.FilesBinary++
			continue
		}
		census.FilesRead++
		if containsPlatformName(path) {
			census.FilesWithNameInPath++
		}
		for _, dir := range KanameSurface {
			if path == dir || strings.HasPrefix(path, dir+"/") {
				census.FilesBySurface[dir]++
			}
		}

		inChart := strings.HasPrefix(path, "deploy/helm/")
		sawASCII, sawMacron := false, false
		for lineNo, line := range strings.Split(string(body), "\n") {
			for _, marker := range assembledMarkers {
				if strings.Contains(line, marker) && containsPlatformName(line) {
					census.Assembled++
					break
				}
			}
			for _, h := range hitsInLine(path, lineNo+1, line, inChart) {
				census.Occurrences++
				if h.Macron {
					census.OccurrencesMacron++
					sawMacron = true
				} else {
					census.OccurrencesASCII++
					sawASCII = true
				}
				h.Lane = classifyResidueHit(h, census.OfferedByLane)
				if perFileLane[path] == nil {
					perFileLane[path] = map[string]int{}
				}
				perFileLane[path][h.Lane]++
				all = append(all, h)
			}
		}
		switch {
		case sawASCII && sawMacron:
			census.FilesBothForms++
		case sawMacron:
			census.FilesMacronOnly++
		case sawASCII:
			census.FilesASCIIOnly++
		}
	}

	if census.Occurrences == 0 {
		return nil, nil, census, fmt.Errorf(
			"обход прочитал %d файлов и не нашёл НИ ОДНОГО вхождения имени платформы — "+
				"предмет не найден, поэтому молчание держателя ничего не утверждает; "+
				"либо распознаватель разошёлся с деревом, либо на вход подано не то дерево",
			census.FilesRead)
	}

	// ─── ведомость решённого остаться: прощаем, и тут же требуем предмета ───
	forgiven := map[string]map[string]int{}
	for _, e := range stay {
		if forgiven[e.Path] == nil {
			forgiven[e.Path] = map[string]int{}
		}
		forgiven[e.Path][e.Lane] = e.Count
	}

	var findings []NameResidueHit
	usedForgiveness := map[string]map[string]int{}
	for _, h := range all {
		lane := h.Lane
		if want, ok := forgiven[h.Path][lane]; ok && perFileLane[h.Path][lane] == want {
			census.ForgivenByLane[lane]++
			if usedForgiveness[h.Path] == nil {
				usedForgiveness[h.Path] = map[string]int{}
			}
			usedForgiveness[h.Path][lane]++
			continue
		}
		census.FoundByLane[lane]++
		findings = append(findings, h)
	}
	filesSeen := map[string]map[string]bool{}
	for _, h := range findings {
		if filesSeen[h.Lane] == nil {
			filesSeen[h.Lane] = map[string]bool{}
		}
		filesSeen[h.Lane][h.Path] = true
	}
	for lane, set := range filesSeen {
		census.FilesByLane[lane] = len(set)
	}

	var ledgerFindings []NameResidueLedgerFinding
	for _, e := range stay {
		got := perFileLane[e.Path][e.Lane]
		switch {
		case got == 0:
			ledgerFindings = append(ledgerFindings, NameResidueLedgerFinding{
				Ledger: "решённое остаться", Lane: e.Lane, Path: e.Path,
				Want: e.Count, Got: 0,
				Why: "имени платформы на этой полосе в файле нет — записи нечего прощать, " +
					"и она пережила свой предмет",
			})
		case got != e.Count:
			ledgerFindings = append(ledgerFindings, NameResidueLedgerFinding{
				Ledger: "решённое остаться", Lane: e.Lane, Path: e.Path,
				Want: e.Count, Got: got,
				Why: "ведомость разошлась с фактом — число обязано быть точным, а не " +
					"потолком: потолок не краснеет никогда и потому не истекает",
			})
		}
	}

	// ─── ведомость остатка: точное число на полосу, в обе стороны ───
	for _, row := range debt {
		gotOcc := census.FoundByLane[row.Lane]
		gotFiles := census.FilesByLane[row.Lane]
		if gotOcc == row.Occurrences && gotFiles == row.Files {
			continue
		}
		why := "остаток ВЫРОС: имя платформы прибавилось там, где продукт называет себя"
		if gotOcc < row.Occurrences || (gotOcc == row.Occurrences && gotFiles < row.Files) {
			why = "остаток снизился, а ведомость отстала — опустить число обязано ТО ЖЕ " +
				"изменение, которое остаток сняло, иначе ведомость перестаёт быть предикатом"
		}
		ledgerFindings = append(ledgerFindings, NameResidueLedgerFinding{
			Ledger: "остаток", Lane: row.Lane,
			Want: row.Occurrences, Got: gotOcc,
			Why: fmt.Sprintf("%s (файлов: записано %d, в дереве %d; владелец полосы: %s)",
				why, row.Files, gotFiles, row.Owner),
		})
	}
	declared := map[string]bool{}
	for _, row := range debt {
		declared[row.Lane] = true
	}
	if len(debt) > 0 {
		for lane, lg := range kanameLanes {
			if lg.Axis == axisBorder && lane != borderUnknownForm {
				continue
			}
			if declared[lane] || census.FoundByLane[lane] == 0 {
				continue
			}
			ledgerFindings = append(ledgerFindings, NameResidueLedgerFinding{
				Ledger: "остаток", Lane: lane, Want: 0, Got: census.FoundByLane[lane],
				Why: "полоса несёт остаток, а строки в ведомости у неё НЕТ — " +
					"незаписанный остаток растёт молча",
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})
	sort.Slice(ledgerFindings, func(i, j int) bool {
		if ledgerFindings[i].Ledger != ledgerFindings[j].Ledger {
			return ledgerFindings[i].Ledger < ledgerFindings[j].Ledger
		}
		if ledgerFindings[i].Lane != ledgerFindings[j].Lane {
			return ledgerFindings[i].Lane < ledgerFindings[j].Lane
		}
		return ledgerFindings[i].Path < ledgerFindings[j].Path
	})
	return findings, ledgerFindings, census, nil
}

func validateStayLedgerShape(stay []NameResidueStay) error {
	seen := map[string]bool{}
	for _, e := range stay {
		if _, ok := kanameLanes[e.Lane]; !ok {
			return fmt.Errorf("ведомость решённого остаться: запись %q называет полосу %q, "+
				"которой у распознавателя нет — запись прощала бы вникуда", e.Path, e.Lane)
		}
		if e.Count <= 0 {
			return fmt.Errorf("ведомость решённого остаться: запись %q/%q объявляет %d "+
				"прощаемых вхождений — записи нечего прощать by construction",
				e.Path, e.Lane, e.Count)
		}
		if strings.TrimSpace(e.Reason) == "" {
			return fmt.Errorf("ведомость решённого остаться: запись %q/%q без причины — "+
				"освобождение без довода снимут как непонятное", e.Path, e.Lane)
		}
		key := e.Path + "\x00" + e.Lane
		if seen[key] {
			return fmt.Errorf("ведомость решённого остаться: пара %q/%q объявлена дважды — "+
				"два места об одном предмете разойдутся молча", e.Path, e.Lane)
		}
		seen[key] = true
	}
	return nil
}

func validateDebtLedgerShape(debt []NameResidueDebt) error {
	seen := map[string]bool{}
	for _, row := range debt {
		if _, ok := kanameLanes[row.Lane]; !ok {
			return fmt.Errorf("ведомость остатка: строка называет полосу %q, которой у "+
				"распознавателя нет", row.Lane)
		}
		if row.Occurrences < 0 || row.Files < 0 {
			return fmt.Errorf("ведомость остатка: полоса %q объявляет отрицательное число", row.Lane)
		}
		if strings.TrimSpace(row.Owner) == "" {
			return fmt.Errorf("ведомость остатка: полоса %q не называет владельца — "+
				"остаток без владельца снимать некому", row.Lane)
		}
		if seen[row.Lane] {
			return fmt.Errorf("ведомость остатка: полоса %q объявлена дважды", row.Lane)
		}
		seen[row.Lane] = true
	}
	return nil
}

// classifyResidueHit — первое совпавшее правило забирает вхождение; каждой
// полосе, чьё правило ИСПОЛНЯЛОСЬ, засчитывается «предложено».
func classifyResidueHit(h NameResidueHit, offered map[string]int) string {
	counted := map[string]bool{}
	for _, rule := range kanameResidueRules {
		if !counted[rule.Lane] {
			offered[rule.Lane]++
			counted[rule.Lane] = true
		}
		if rule.Match(h) {
			return rule.Lane
		}
	}
	return borderUnknownForm
}

func containsNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

func containsPlatformName(line string) bool {
	rs := []rune(line)
	for i := range rs {
		if n, _ := platformNameAt(rs, i); n > 0 {
			return true
		}
	}
	return false
}

// hitsInLine — все вхождения имени в строке вместе с окрестностью.
func hitsInLine(path string, lineNo int, line string, inChart bool) []NameResidueHit {
	rs := []rune(line)
	var out []NameResidueHit
	for i := 0; i < len(rs); i++ {
		n, macron := platformNameAt(rs, i)
		if n == 0 {
			continue
		}
		a := i
		for a > 0 && isResidueTokenRune(rs[a-1]) {
			a--
		}
		b := i + n
		for b < len(rs) && isResidueTokenRune(rs[b]) {
			b++
		}
		segA := i
		for segA > a && rs[segA-1] != '/' {
			segA--
		}
		segB := i + n
		for segB < b && rs[segB] != '/' {
			segB++
		}
		out = append(out, NameResidueHit{
			Path:       path,
			Line:       lineNo,
			Text:       string(rs[a:b]),
			Seg:        string(rs[segA:segB]),
			SegPre:     string(rs[segA:i]),
			SegRest:    string(rs[i+n : segB]),
			PathPre:    string(rs[a:segA]),
			PathRest:   string(rs[segB:b]),
			Hit:        string(rs[i : i+n]),
			Macron:     macron,
			LinePrefix: string(rs[:i]),
			InChart:    inChart,
		})
		i += n - 1
	}
	return out
}
