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
//	Б4 одобренная приёмка                  вердикт назван вместе со своей ревизией
//	Б5 функция общего фундамента в схеме   решено остаться: один шаблон на шесть
//	                                       владельцев, заведена ПРИМЕНЁННОЙ
//	                                       миграцией (ban #5)
//	Б6 форма неизвестна распознавателю     СЛЕПАЯ ЗОНА; число точное, его рост
//	                                       означает, что появилась форма записи,
//	                                       которой держатель не судит
//	Б7 ссылка на задачу трекера            имя называет РЕПОЗИТОРИЙ учёта, а не
//	                                       продукт: `kacho` со знаком номера и
//	                                       цифрой. Правке не подлежит — за ней
//	                                       живой репозиторий
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
// Ссылка на задачу трекера границей (Б7) сделана ИМЕННО ПОТОМУ, что здесь
// форма решает: `kacho` со знаком номера и цифрой не может быть самоназванием
// продукта ни при каком прочтении, тогда как то же имя в прозе может быть и
// тем, и другим. Шов между границей и адъюдикацией проходит по этому вопросу,
// а не по объёму работы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗЕЛЁНЫЙ ПРОГОН НЕ ОЗНАЧАЕТ, ЧТО УСЛОВИЕ П3 ВЫПОЛНЕНО
//
// Сказано первым, чтобы «PASS» не читалось шире сделанного. Зелёный означает
// ровно одно: остаток НЕ ВЫРОС против записанного. Условие «ноль чужого имени
// там, где продукт называет себя» выполнено тогда и только тогда, когда
// ведомость остатка ПУСТА, — и это отдельное, машинно проверяемое утверждение
// (KanameNameResidueOutstanding даёт его числом, а перепись печатает словами).
//
// ─────────────────────────────────────────────────────────────────────────────
// ДЕРЖАТЕЛЬ БЫЛ СНЯТ ПРИ ЖИВОМ ПРЕДМЕТЕ — И ВОЗВРАЩЁН ЗАМЕРОМ, А НЕ МНЕНИЕМ
//
// Переезд контракта (#2133) удалил этот файл вместе с его пробой и инъекцией,
// не назвав удаления в своём сообщении. Предмет при этом был ЖИВ: перепись на
// вершине линии даёт 2417 файлов поверхности и 6242 вхождения имени платформы,
// то есть держатель снят не потому, что искать стало нечего.
//
// Причина отказа была ОДНОСТРОЧНОЙ и предсказанной этим же файлом: перечень
// поверхности называл `proto/kacho/cloud/iam`, а переезд перенёс контракт в
// `proto/kaname/cloud/iam`. Каталог без единого файла в индексе — ОТКАЗ обхода,
// а не тихий пропуск; отказ и есть то, что заставило перечень последовать за
// переездом. Держатель отработал ровно как задуман.
//
// Урок, ради которого абзац оставлен, а не удалён: гейт, отказавший на своей
// предпосылке, выглядит сломанным, а является исправным. Отличает их один
// вопрос — ЖИВ ЛИ ПРЕДМЕТ; ответ на него даёт перепись, а не впечатление от
// красного. Снятие держателя при живом предмете не истекает само никогда: оно
// не оставляет ни находки, ни записи, и следующий читатель видит зелёное дерево.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
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
	// Контракт доступа переехал вместе с продуктом: `kacho.cloud.iam.v1` →
	// `kaname.cloud.iam.v1` (#2133). Отказ обхода на прежнем каталоге и есть
	// то, что заставило перечень последовать за переездом, — молчаливой слепой
	// зоны он не завёл.
	"proto/kaname/cloud/iam",
	// Словарь аннотаций доступа остался под прежним корнем: он объявляет
	// расширения, на которые ключуется генерация всей платформы, а не одной
	// службы.
	"proto/kacho/iam",
	"services/iam",
}

// KanameOverlayGlob — образец наложений значений зонта, настраивающих подчарты.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОТДЕЛЬНО ОТ KanameSurface
//
// `KanameSurface` перечисляет КАТАЛОГИ, целиком принадлежащие продукту. Наложение
// значений так перечислить нельзя: один файл настраивает ВСЕ подчарты зонта, и
// имя платформы в блоке соседа законно. Целый файл в поверхность не входит и
// вне её остаться не может — входит его ЧАСТЬ, привязанная к подчарту.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО СТОИЛО
//
// `KanameSurface` называл каталог подчарта и не называл файлов, задающих ему
// значения. Между тем значение ПЕРЕБИВАЕТ умолчание чарта: то, что объявлено в
// наложении, и есть действующая величина. Имя платформы, живущее там, продукт о
// себе ПРОИЗНОСИТ — а перепись его не считала, и «остаток такой-то» читался у́же,
// чем есть (kacho#2260).
//
// Держатель при этом был зелен и печатал перепись по шести осям. Молчал он не
// потому, что нашёл ноль, а потому что файла НЕ ЧИТАЛ: «ноль находок» и «ноль
// прочитанного» здесь неразличимы изнутри переписи — она печатает объём
// осмотренного, но осмотренного по СВОЕЙ поверхности, а спорна как раз она.
//
// Образец, а не перечень: новое наложение попадает под наблюдение само.
const KanameOverlayGlob = "deploy/helm/umbrella/values*.y*ml"

// KanameOverlayPart — часть продукта, чьи строки наложения принадлежат Kaname.
//
// Каталог исходников, а не имя чарта: привязка строки к подчарту объявлена ОДИН
// раз и живёт у владельца имён (`productnaming.PartOfLine`), который возвращает
// именно каталог. Вторая копия разошлась бы с ним молча — и разошлась бы на
// переименовании, то есть тогда, когда сверить их некому.
const KanameOverlayPart = "iam"

// KanameLinesOfOverlay — тело наложения, в котором ОСТАВЛЕНЫ только строки,
// принадлежащие подчарту Kaname; прочие ЗАМЕНЕНЫ ПУСТЫМИ.
//
// Заменены, а не выброшены: номера строк — часть координаты находки, и обход,
// сдвинувший их, посылал бы читателя не туда.
//
// Привязка берётся у объявленного владельца, а не заводится второй копией:
// блок `kaname:` соседствует в одном файле с блоками платформенных подчартов,
// где имя платформы ЗАКОННО, поэтому расширение поверхности одним путём не
// годится — нужна привязка строки к подчарту.
// Второй возврат — сколько строк ОСТАВЛЕНО. Он и есть перепись этой полосы:
// «наложений прочитано 10» без него не отличало бы «блока kaname в них нет» от
// «привязка перестала работать».
func KanameLinesOfOverlay(rel string, body []byte) (filtered []byte, kept int) {
	lines := strings.Split(string(body), "\n")
	out := make([]string, len(lines))
	for i := range lines {
		if part, ok := productnaming.PartOfLine(rel, lines, i); ok && part == KanameOverlayPart {
			out[i] = lines[i]
			kept++
		}
	}
	return []byte(strings.Join(out, "\n")), kept
}

// kanameApprovedAcceptanceDir — каталог одобренных приёмок службы.
//
// ПОЧЕМУ ГРАНИЦА. Вердикт приёмки есть утверждение о РЕВИЗИИ, которую прочитал
// проверяющий, а не о файле по имени. Механическая правка его не переносит — и,
// что важнее для этой границы, механичность НЕ означает сохранности утверждений:
// массовое переименование дома приёмок (#2214) прошло 335 пар строк без единой
// непарной и без единой пары с изменившимся числом токенов — и всё-таки
// сфальсифицировало ДВА утверждения. Оба — записи, привязанные к прошлой
// ревизии: путь, смешавший модуль платформы с каталогом службы, и объявленный
// вывод команды, чей образец правка тронуть не могла. Ни диффу, ни резолверу
// координат, ни машинному чтению вердикта они не видны.
//
// ЧТО ГРАНИЦА ДЕЛАЕТ. Остаток имени внутри этого каталога держатель НЕ считает
// находкой: правка здесь стоит дороже остатка. Решение, замер и цена обоих
// отвергнутых исходов —
// services/iam/docs/engineering/architecture/verdict-names-a-revision-not-a-file.md
//
// ЧЕГО ОНА НЕ ЗНАЧИТ. Она не утверждает, что каталог не правился: заведена она
// 557c38d174 (эпоха 1788721825) — СУТКАМИ ПОЗЖЕ трёх массовых правок
// (1788555366, 1788619179, 1788623772), и действует вперёд, а не назад.
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
	borderTrackerReference   = "Б7 ссылка на задачу трекера"
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
	// LineRest — вся строка справа от ТОКЕНА. Знак, стоящий сразу за именем,
	// токеном не выражается (в токен он не входит), а различить по нему нужно:
	// `kacho#2260` называет репозиторий учёта, а не продукт.
	LineRest string
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
	{borderTrackerReference, isTrackerReference},
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
		"вердикт назван вместе со своей ревизией; правка его не переносит"},
	borderFoundationFunction: {borderFoundationFunction, axisBorder,
		"решено остаться: один шаблон на шесть владельцев, заведена применённой миграцией"},
	borderUnknownForm: {borderUnknownForm, axisBorder,
		"СЛЕПАЯ ЗОНА: форма записи, которой держатель не судит; число точное"},
	borderTrackerReference: {borderTrackerReference, axisBorder,
		"ссылка на задачу: имя называет РЕПОЗИТОРИЙ учёта, а не продукт"},
}

// isTrackerReference — вхождение есть КРАТКАЯ ссылка на задачу трекера вида
// `kacho#2260`. Имя платформы здесь называет РЕПОЗИТОРИЙ, в котором ведётся
// учёт, а не продукт, который себя так зовёт, — поэтому оно и не остаток.
//
// ДИСКРИМИНАТОР СТРУКТУРНЫЙ, А НЕ СЛОВАРНЫЙ, и это несущее. Требуются ОБА
// условия сразу:
//
//  1. токен есть имя платформы ЦЕЛИКОМ — ни приставки, ни продолжения. Всякая
//     форма самоназвания продукта (`kacho-<слово>`, `kacho.cloud`, `kacho_iam`,
//     `PRO-Robotech/kacho`) продолжается ВНУТРИ токена и до правила не доходит
//     by construction;
//  2. сразу за токеном стоит знак номера И ЦИФРА. Знак в токен не входит,
//     поэтому увидеть его можно только справа от токена — отсюда LineRest.
//
// ПОЧЕМУ ГРАНИЦА, А НЕ ЗАПИСЬ ВЕДОМОСТИ. Ведомость решённого остаться ведут
// перечнем «путь + полоса + точное число», и это верно для адъюдикации ПО
// ОДНОМУ. Здесь предмет другой: ссылка на задачу — принятое в дереве написание,
// она появляется в КАЖДОМ новом разборе, и перечень пришлось бы дополнять
// каждым изменением, которое сослалось на задачу. Такая запись не истекает и не
// адъюдицируется — она учитывает форму, а формы учитывают границы.
//
// ЧЕГО ГРАНИЦА НЕ ДЕЛАЕТ — и по какой причине ИМЕННО, а не по правдоподобной.
// Токен длиннее имени она не берёт: `apps/kacho` со знаком номера остаётся на
// полосе имени платформы, потому что условие 1 судит ТОКЕН, а не сегмент пути.
// А вот `PRO-Robotech/kacho` со знаком номера остаётся за границей пути модуля
// фундамента (Б1) по ДРУГОЙ причине — Б1 стоит в перечне правил РАНЬШЕ, и до
// этого правила вхождение не доходит вовсе. Две причины названы врозь
// намеренно: инъекция, подменившая токен сегментом, на форме `PRO-Robotech/…`
// молчит — её держит порядок, а не предикат, — и доказательство, опирающееся
// только на неё, было бы зелёным при сломанном условии 1.
func isTrackerReference(h NameResidueHit) bool {
	if h.Macron || !strings.EqualFold(h.Text, "kacho") {
		return false
	}
	return len(h.LineRest) > 1 && h.LineRest[0] == '#' &&
		h.LineRest[1] >= '0' && h.LineRest[1] <= '9'
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
	// ── РАЗДЕЛЯЮЩИЙ МЕХАНИЗМ: имя платформы здесь ПРЕДМЕТ, а не остаток (#2168) ──
	//
	// Полоса #2168 завела объявленный источник имён владельцев. Чтобы отличить
	// контракт платформы (`kacho.cloud.operation`, `kacho.cloud.subscription`) от
	// контракта службы, механизм обязан НАЗВАТЬ обоих: имя платформы стоит здесь
	// тем же, чем отставленное имя схемы стоит у стража старта выше — входом
	// распознавателя. Снять его значит снять само различение.
	//
	// ПОЧЕМУ ЗАПИСЬЮ, А НЕ ПРИЗНАКОМ ФОРМЫ. Признак напрашивается («файл объявляет
	// константу с именем платформы») и назван негодным в шапке этой же ведомости:
	// он освободил бы и тех, кого держатель заводился ловить. Здесь он негоден
	// буквально — `const platformOwner = "kacho"` под такой признак подпадает
	// первым.
	//
	// ПОЧЕМУ НЕ ПОДНЯТИЕМ ЧИСЛА ДОЛГА. Долг означает «остаток, который ещё
	// предстоит снять», и его пустота есть условие П3. Эти шесть вхождений снять
	// НЕЛЬЗЯ никогда — они и есть различение, — поэтому в долге они сделали бы П3
	// недостижимым by construction. Ведомость «решено остаться» ровно для этого и
	// заведена, и она истекает сама: число точное, изменится — покраснеет.
	{
		Path: "services/iam/internal/contractnaming/contractnaming.go", Lane: lanePlatformName, Count: 3,
		Reason: "объявленный источник имён владельцев: `const platformOwner` и разбор, " +
			"его объясняющий. Имя платформы — ВХОД различения контракта платформы от " +
			"контракта службы, а не имя, которым служба называет себя",
	},
	{
		Path: "services/iam/internal/contractnaming/contractnaming_test.go", Lane: lanePlatformName, Count: 1,
		Reason: "проба того же различения: имя платформы подаётся ей входом",
	},
	{
		Path: "services/iam/internal/contractnaming/contractnaming_test.go", Lane: laneObjectName, Count: 1,
		Reason: "та же проба: законный близнец — запись, похожая на контракт платформы, " +
			"но им не являющаяся; без неё различение зеленело бы на всём подряд",
	},
	{
		Path: "services/iam/internal/manifest/roleexport/contractowner_injection_test.go", Lane: laneContractCoordinate, Count: 1,
		Reason: "доказательство различения инъекцией: координата контракта платформы — " +
			"тот самый вход, на котором отбор владельца обязан промолчать",
	},
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
	{
		Path: "services/iam/internal/supplyhygiene/package_qualifier_test.go", Lane: laneObjectName, Count: 3,
		Reason: "проверка КВАЛИФИКАТОРА пакета (#2130): отставленное имя — её ВХОД, без " +
			"него у распознавателя нет предмета, а у находки нет координаты",
	},
	{
		Path: "services/iam/internal/supplyhygiene/package_qualifier_test.go", Lane: borderUnknownForm, Count: 2,
		Reason: "та же проверка: формы записи, которых держатель остатка не судит, названы " +
			"в её шапке — иначе граница её собственной полосы неизвестна читателю",
	},
	{
		Path: "services/iam/internal/supplyhygiene/package_qualifier_injection_test.go", Lane: laneObjectName, Count: 12,
		Reason: "доказательство той проверки инъекцией: дефект вносится отставленным " +
			"квалификатором, и он же стоит в законных близнецах, на которых она молчит",
	},
	{
		Path: "services/iam/internal/supplyhygiene/package_qualifier_injection_test.go", Lane: laneDomainAddress, Count: 1,
		Reason: "то же доказательство: имя DNS стенда — законный близнец, на котором " +
			"проверка обязана молчать; снять его значит сделать близнеца вакуумным",
	},
	{
		Path: "services/iam/internal/supplyhygiene/package_qualifier_injection_test.go", Lane: lanePlatformName, Count: 1,
		Reason: "то же доказательство: имя платформы отдельным словом — ещё один близнец " +
			"строчной полосы",
	},
	{
		Path: "services/iam/internal/supplyhygiene/package_qualifier_injection_test.go", Lane: laneContractCoordinate, Count: 1,
		Reason: "то же доказательство: пакет контракта платформы — близнец, по которому " +
			"видно, что дискриминатор структурный, а не словарный",
	},
	{
		Path: "services/iam/internal/supplyhygiene/package_qualifier_injection_test.go", Lane: borderUnknownForm, Count: 2,
		Reason: "то же доказательство: формы вне полос держателя остатка, поданные как " +
			"вход инъекции",
	},

	// ── ДОКУМЕНТ О САМОМ ПЕРЕИМЕНОВАНИИ: имя платформы здесь СВИДЕТЕЛЬСТВО (#2214) ──
	//
	// Решение «вердикт называет ревизию, а не файл» выведено ЗАМЕРОМ массовых
	// правок дома одобренных приёмок, и его довод держится на двух цитатах: пара
	// подстановки, которой правка шла, и вывод команды, который правка объявила
	// неверно. Снять оттуда имя платформы значит снять сам довод — останется
	// утверждение «две записи стали ложными» без предъявления обеих.
	//
	// ПОЧЕМУ НЕ ГРАНИЦЕЙ Б4. Граница судит по КАТАЛОГУ, и расширить её до «всякий
	// документ, чьи утверждения привязаны к прошлой ревизии» нельзя: такой признак
	// машинно неразрешим, а признак по форме освободил бы и тех, кого держатель
	// заводился ловить. Запись перечнем точна и истекает сама: число ТОЧНОЕ,
	// изменится — покраснеет; снимут документ — покраснеет тоже.
	//
	// ПОЧЕМУ НЕ ПОДНЯТИЕМ ЧИСЛА ДОЛГА. Долг означает «остаток, который предстоит
	// снять»; эти два вхождения снять НЕЛЬЗЯ никогда, и в долге они сделали бы
	// условие П3 недостижимым by construction.
	{
		Path: "services/iam/docs/engineering/architecture/verdict-names-a-revision-not-a-file.md", Lane: lanePlatformName, Count: 1,
		Reason: "пара подстановки, которой шла массовая правка каталогов службы: без " +
			"левой стороны пары решение не показывает, ЧТО именно было заменено",
	},
	{
		Path: "services/iam/docs/engineering/architecture/verdict-names-a-revision-not-a-file.md", Lane: laneObjectName, Count: 1,
		Reason: "имя объекта, которое команда печатает НА САМОМ ДЕЛЕ, — предъявление " +
			"второй находки: документ объявил один вывод, а команда даёт другой",
	},

	// ── ЯКОРЬ КЛАСТЕРА: переход СОСТОЯЛСЯ, и остаток снять НЕЛЬЗЯ (#2113) ──
	//
	// Полоса стояла в ведомости ДОЛГА, то есть объявляла работу, которой нет:
	// написание якоря переведено на `cluster_root` задачей #2113, и авторитет у
	// него один — объявление кода. Всё, что осталось, снять невозможно ни при
	// каком порядке работ, и вот проверяемый критерий каждой записи: сними
	// прежнее написание — и файл перестанет делать то, ради чего написан.
	//
	// ПОЧЕМУ ЭТО НЕ БУХГАЛТЕРИЯ, А ПОЧИНКА ПРЕДИКАТА. Долг означает «остаток,
	// который ещё предстоит снять», а его пустота есть условие П3. Строка,
	// объявляющая неснимаемое, делала условие НЕДОСТИЖИМЫМ by construction —
	// ровно тот класс, который корпус ловит в чужих предикатах готовности.
	// Ведомость решённого остаться истекает сама: числа ТОЧНЫЕ, тронут файл —
	// покраснеет.
	{
		Path: "services/iam/internal/migrations/0001_initial.sql", Lane: laneClusterAnchor, Count: 88,
		Reason: "базовая миграция СЕЕТ якорь прежним написанием, и она ПРИМЕНЕНА: " +
			"правка применённой миграции запрещена (ban #5). Перевод идёт новой " +
			"миграцией, а не переписыванием этой",
	},
	{
		Path: "services/iam/internal/migrations/20260906214500_cluster_anchor_moves_to_its_declared_spelling.sql", Lane: laneClusterAnchor, Count: 4,
		Reason: "миграция ПЕРЕХОДА: прежнее написание — её ИСТОЧНИК. Сними его, и " +
			"переписывать станет нечего — перевод превратится в пустой оператор",
	},
	{
		Path: "services/iam/internal/migrations/cluster_anchor_agrees_with_the_code_integration_test.go", Lane: laneClusterAnchor, Count: 1,
		Reason: "отрицательный контроль: проба требует, чтобы прежнего написания в своде " +
			"НЕ ОСТАЛОСЬ. Её собственный комментарий оговаривает, почему величина взята " +
			"литералом, а не из объявления, — из объявления она была бы тождественно неверной",
	},
	{
		Path: "services/iam/internal/migrations/cluster_anchor_way_back_integration_test.go", Lane: laneClusterAnchor, Count: 1,
		Reason: "запись о собственной истёкшей посылке: проба брала переход W4 входом, " +
			"переход состоялся, и разбор называет прежнее написание, чтобы объяснить, " +
			"ЧЕМ именно посылка истекла",
	},
	{
		Path: "services/iam/internal/modelrender/canon.go", Lane: laneClusterAnchor, Count: 1,
		Reason: "запись ЗАМЕРА: шапка называет сдвиг величин канона и его причину — " +
			"переход написания якоря. Сними прежнее написание, и утверждение о сдвиге " +
			"перестанет предъявлять то, между чем сдвиг измерен",
	},

	// ── ПРИСТАВКА СХЕМЫ: остались ТРИ документа, и в каждом имя есть ПРЕДМЕТ ──
	//
	// Полоса несла девять вхождений; четыре сняты этим же изменением как
	// утверждения, пережившие свой предмет (приставка имён метрик названа
	// `kacho_iam_` в шапке пакета и на странице наблюдаемости, при том что
	// дерево производит `kaname_`; плюс два синтетических имени фикстур).
	// Оставшиеся пять снять нельзя: в каждом прежняя приставка ЦИТИРУЕТСЯ как
	// то, от чего переехали.
	{
		Path: "services/iam/docs/content/terraform/provider.mdx", Lane: laneSchemaPrefixKin, Count: 3,
		Reason: "инструкция переезда состояния арендатора: страница обязана назвать " +
			"ПРЕЖНИЕ имена типов — их читает блок `moved`, и без них у арендатора нет " +
			"левой стороны перехода",
	},
	{
		Path: "services/iam/docs/engineering/architecture/known-divergences.md", Lane: laneSchemaPrefixKin, Count: 1,
		Reason: "свидетельство о СНЯТОМ канале уведомления: разбор называет его поимённо, " +
			"и проба службы требует, чтобы канала с этим именем в базе не было",
	},
	{
		Path: "services/iam/docs/engineering/components/29-relational-verdict.md", Lane: laneSchemaPrefixKin, Count: 1,
		Reason: "то же свидетельство на странице компонента: имя снятого канала — предмет " +
			"утверждения, а не остаток бренда",
	},
}

// KanameNameResidueDebt — ведомость ОСТАТКА по полосам.
//
// Замер ПЕРЕМЕРЕН на вершине накопительной линии 03c890c2da (2026-09-07);
// прежний снят на a36563df96 (2026-09-06). Числа ТОЧНЫЕ и снимаются тем же
// изменением, которое снимает остаток; повторить их можно прогоном самой
// проверки — она печатает перепись и на зелёном.
//
// Ведомость движется ВНИЗ, и перемер на 03c890c2da это подтвердил: из пятнадцати
// строк четыре опустились, одиннадцать сошлись с фактом дословно, ни одна не
// выросла. Утверждение датировано ТОЙ ревизией и нормой не является: рост
// возможен и случился ниже (#2170). Норма здесь другая и она одна — число
// ТОЧНОЕ и правится ТЕМ ЖЕ изменением, которое остаток двинуло, в любую сторону;
// выросшая строка обязана назвать, ЧЕМ именно она выросла.
//
// ТРИ СТРОКИ ОПУЩЕНЫ ЗДЕСЬ ЖЕ (#2234, 2026-09-07), и это исполнение нормы выше,
// а не ведение чужой полосы. Снятие приставки платформы с ключей витрины чарта
// продукта убрало из `services/iam/deploy` ровно ЧЕТЫРЕ вхождения (35 → 31), и
// они разошлись по трём полосам: домен и адрес −2 (169 → 168 файлов), имя
// объекта −1 (файлов столько же), слепая зона распознавателя −1 (27 → 26). Сумма
// дельт сходится с замером по каталогу; полосы, распознаватели и политика
// ведомости не тронуты.
//
// Опустились те, чьи полосы вела другая работа: переезд контракта (#2133) —
// 1269→452 вхождений и 398→37 файлов, переход якоря кластера (#2113) — 402→95 и
// 141→5, и две полосы витрины, задетые ими попутно, — имя объекта 676→675 и имя
// платформы 627→619 (287→282 файла).
//
// Числа полосы «координата пакета доступа» опустились, но НЕ до нуля, и это
// надо читать точно: #2133 перевёл координату СВОЕГО контракта, а служба
// продолжает называть контракты платформы, которые никуда не переезжали.
//
// Четыре строки правит ВОЗВРАТ ОТКАЧЕННОГО — #2170, полоса «адресат токена»
// (предшественник 63331584c7, откачен переездом контракта). Две опустились и две
// ВЫРОСЛИ; рост здесь не оговорка, а следствие, и он назван поимённо, потому что
// ведомость иначе перестаёт быть предикатом.
//
// Опустились: домен и адрес 586→577 (170→169 файлов) и бренд в тексте 100→99
// (77→76) — снято умолчание `api.kacho.cloud`, из которого выводилось клеймо
// адресата каждого выпущенного токена, и вместе с ним ушли его упоминания у
// стража и в трёх пробах.
//
// Выросли: ключ профиля и шаблона 99→101 (12→13 файлов) и имя объекта 675→676
// (243 файла — этот файл в полосе уже стоял, у него 2→3). Прибавка — ТРИ
// упоминания ключа `kacho.domain` в двух файлах: подчарт читает
// `.Values.kacho.domain` (строка шаблона и объясняющий её комментарий — два),
// служба называет тот же ключ в комментарии умолчаний (одно).
//
// Ключ существует и читается зонтичным чартом с #127; здесь заведён
// ВТОРОЙ его читатель, а не второе объявление — иначе адресат собирался бы из
// своего источника и разошёлся бы с издателем молча. Переименование самого ключа
// ведёт полоса дебрендинга, и эти строки уедут вместе с ним.
//
// Опущены ещё три строки — #2168, форма записи каталога прав. Приставка
// владельца перестала быть выписанной: отбор полосы и разбор записи спрашивают
// её у объявленного источника имён (services/iam/internal/contractnaming), и
// вместе с литералами ушла их доля остатка — координата пакета доступа 452→449
// (37→35 файлов), домен и адрес 590→586 (173→170), имя платформы 619→618 (282).
//
// Опущена ещё одна строка — #2169, адреса собственных REST-фронтов у харнесса.
// Прямого предмета у неё там нет: полоса тронула генератор коллекций, а
// коллекции — ПОРОЖДЁННЫЙ артефакт, и перегенерация подтянула отставание,
// накопленное прежними полосами переименования. Два вхождения имени платформы в
// сгенерированной коллекции полосы фасада ушли вместе с ним. Число опускает ТО
// ЖЕ изменение, которое остаток сняло, иначе ведомость перестаёт быть
// предикатом: имя платформы 618→616 (282→281).
// Опущена ещё одна строка — #2186, клиентский лист службы. Прямого предмета у
// неё там нет: полоса заводила службе собственную клиентскую личность, и по
// дороге переписала объяснение секретов подчарта — прежнее утверждало «SERVER
// cert only … does not dial peers», пережившее свой предмет. Вместе с ним ушла
// приставка `kacho-<svc>` из пояснения к имени секрета: имя объекта 676→675
// (243 файла — число файлов не изменилось, вхождение было не единственным в
// своём файле). Число опускает ТО ЖЕ изменение, которое остаток сняло.
//
// Опустилась ещё раз тем же порядком: имя объекта 675→664 (243→236 файлов) —
// #2130 снял одиннадцать вхождений прежнего КВАЛИФИКАТОРА пакета из семи файлов
// службы. Полоса записи там вторая: держатель имён каталогов её не судит by
// construction, поэтому её вёл собственный гейт
// (`internal/supplyhygiene/package_qualifier_test.go`), а его вход и его
// доказательство инъекцией внесены в ведомость решённого остаться выше — сама
// проверка обязана называть то, что запрещает.
// Опущена ещё одна строка — #2245, имя накатчика схемы. Двоичный файл, который
// оператор набирает руками при каждом обновлении версии, звался именем
// платформы; переименован в `kaname-migrator` вместе со своим производителем
// (сборка), обоими употребителями (чарт поставки и подчарт зонта) и держателями
// их согласия. Имя платформы ушло из полосы витрины целиком: имя объекта
// 664→605 (236→218 файлов). Число опускает ТО ЖЕ изменение, которое остаток
// сняло.
//
// Осталось ОДНО вхождение этой полосы, и оно за границей Б4: одобренная приёмка
// цитирует прежнее имя внутри своего утверждения о прошлой ревизии. Держатель
// его не считает by construction, и правка там стоила бы дороже остатка.
//
// ЧИСЛО ПОЛОСЫ СВЕДЕНО ЗАМЕРОМ, А НЕ СЛОЖЕНИЕМ (2026-09-07). Полосу опускали
// ДВЕ работы разом — переименование накатчика (#2245, −59 вхождений и −18
// файлов) и правка ключей витрины (#2263, −1 вхождение). Слияние дало по строке
// конфликт, и соблазн был сложить две переписи в уме: 664−59−1 = 604 вхождения,
// 236−18−0 = 218 файлов.
//
// Замер по сведённому дереву даёт 604 вхождения и **217** файлов. Вхождения
// сошлись, ФАЙЛЫ — нет: одно из снятых вхождений было в файле последним, и
// арифметика этого знать не может. Расхождение в один файл здесь безобидно, а в
// общем случае — это и есть та величина, которую нечем проверить.
//
// Отсюда норма для следующего сведения: число ведомости берётся ПРОГОНОМ на
// сведённом дереве. Полоса «домен и адрес» в том же конфликте разрешена иначе —
// её двигала только чужая работа, поэтому взято влитое значение без замера.
// РОСТ 2026-09-08 на шести полосах — правильный исход, а не регресс дерева
// (kacho#2260). Поверхность держателя не читала НАЛОЖЕНИЯ ЗНАЧЕНИЙ зонта: она
// называла каталог подчарта, но не файлы, которые ему задают значения, — а
// значение перебивает умолчание чарта, то есть объявленное в наложении и есть
// действующая величина. Имя платформы, живущее там, продукт о себе ПРОИЗНОСИТ.
// Числа выросли на то, что перестало быть невидимым: +102 вхождения на 10
// наложениях. Замер — прогоном на этом дереве, а не арифметикой.
//
// ВТОРОЙ рост в том же изменении, +6 на полосе имени платформы, — ссылки на
// задачи трекера в новых комментариях (`kacho#NNNN`). Это принятое в модуле
// написание: оно называет РЕПОЗИТОРИЙ трекера, а не продукт, и в дереве службы
// таких ссылок 411. Отличать ссылку на трекер от имени, которым продукт себя
// зовёт, — предмет полосы дебрендинга, а не этого изменения; здесь число
// приведено к факту, чтобы «остаток не вырос» осталось проверяемым.
//
// ОПУЩЕНА ТРЕМЯ ПОЛОСАМИ РАЗОМ — #2141, снятие наложения правил (2026-09-08).
// Механизм пережил своего потребителя: служба объявляет в собственном исходнике,
// что решение о доступе принимает единственный механизм, а поставка продолжала
// возить боковой контейнер правил, пять файлов правил, карты его настроек, ручку
// в профилях и две сетевые политики. Снятое унесло с собой имя платформы сразу с
// трёх полос ведомости, и числа опускает ТО ЖЕ изменение:
//
//	ключ профиля и шаблона  101→85  (13→9 файлов)  — ручка `opaSidecar.*` профилей
//	имя объекта             604→596 (217→216)      — имена снятых карт и политик
//	имя платформы           616→611 (281→278)      — путь пакета правил и метка пода
//
// Числа взяты ПРОГОНОМ на сведённом дереве, а не сложением: норма выведена
// абзацем выше, и здесь она особенно уместна — работа шла в полосе, которая
// сливалась с двумя чужими, и арифметика по файлам заведомо не сходится, когда
// снятое вхождение было в своём файле последним.
//
// ЧИСЛА НИЖЕ СВЕДЕНЫ ЗАМЕРОМ ОБЕИХ РАБОТ РАЗОМ (2026-09-08). Две правки выше
// двигали ведомость в РАЗНЫЕ стороны на пересекающихся полосах: одна расширила
// поверхность держателя до наложений значений зонта, другая сняла из поставки
// боковой контейнер правил — то есть часть файлов, которые расширенная
// поверхность как раз и начала читать. Слагаемые считают ПЕРЕСЕКАЮЩЕЕСЯ
// множество, поэтому сложение здесь неверно не «в общем случае», а заведомо.
//
// НАСКОЛЬКО неверно — измерено, а не предположено. Слева прогон держателя на
// сведённом дереве, справа арифметика `наша + (влитая − база)` по трём ведомостям:
//
//	ключ профиля и шаблона  132/14   арифметика 132/14   сошлась
//	переменная окружения     64/41   арифметика  64/41   сошлась
//	имя объекта             615/220  арифметика 614/220  РАЗОШЛАСЬ
//	имя платформы           638/292  арифметика 645/299  РАЗОШЛАСЬ
//	Б6 неизвестная форма     70/31   арифметика  70/31   сошлась
//
// Две полосы из пяти арифметика назвала бы неверно, и по-разному: у имени объекта
// на одно вхождение при сошедшихся файлах (снятое вхождение было в своём файле не
// последним), у имени платформы — на семь и семь сразу. Ни то ни другое не прошло
// бы молча: держатель сверяет ведомость с деревом ТОЧНО и в обе стороны, поэтому
// арифметика дала бы не тихий промах, а красное. Числом ведомости служит замер.
//
// ОПУЩЕНЫ ДВЕ СТРОКИ РАЗОМ — #2128, схема в комментариях контракта (2026-09-08).
// Комментарии контракта доступа называли таблицы схемой `kacho_iam`, которой
// дерево не производит с тех пор, как схема получила имя своего продукта:
// миграции службы объявляют `CREATE SCHEMA kaname` и `SET search_path TO
// kaname, public`. Утверждение пережило свой предмет и уезжало клиенту вместе с
// контрактом — комментарий контракта попадает в порождённый стаб дословно, и
// там его несли 8 файлов, 21 вхождение.
//
// Приведено к факту, а не удалено: 16 вхождений в 5 файлах контракта названы
// именем, которое дерево производит (`kaname.users`, `kaname.fga_outbox`, …) —
// так же их адресует и живой код службы. Правка целиком в комментариях: строк
// диффа вне комментария 0 у контракта и 0 у перегенерированных стабов.
//
//	имя схемы                      2/2  → 0/0
//	квалифицированное имя таблицы  14/4 → 0/0
//
// Числа взяты ПРОГОНОМ держателя на этом дереве, а не арифметикой; двинулись
// ровно эти две полосы, остальные тринадцать строк ведомости совпали с фактом
// дословно.
//
// ЧТО ОСТАЛОСЬ НЕДЕРЖИМЫМ — названо числом и задачей, а не умолчано. Новая
// координата `kaname.<таблица>` в контракте не сверяется с деревом ничем: гейт
// имени схемы службы судит ДЕРЕВО СЛУЖБЫ (`serviceRoot = "../.."`), а контракт
// лежит вне его и вне модуля службы — тянуть его корень вверх запрещено тем же
// классом, что ведут #2145 и #2239; этот держатель ловит имя ПЛАТФОРМЫ и об
// имени собственного продукта не утверждает ничего by construction. То есть
// класс приведён к факту, но не закрыт построением. Предмет и предикат снятия —
// #2291.
//
// ЗАКРЫТЫ ДВЕ ПОЛОСЫ И СНЯТА ЛОЖНАЯ ПОЛОВИНА ТРЕТЬЕЙ (#2076, 2026-09-08). Все
// три числа сняты ПРОГОНОМ держателя на этом дереве; сложение здесь заведомо
// неверно — правки пересекаются по файлам, а перевод вхождения между полосой и
// границей файлов не убавляет.
//
//	якорь кластера            95/5   → 0/0
//	сосед по приставке схемы   9/7   → 0/0
//	имя платформы            638/292 → 321/120
//
// ПЕРВЫЕ ДВЕ ЗАКРЫТЫ, И ЭТО НЕ БУХГАЛТЕРИЯ. Строка долга объявляет «остаток,
// который ещё предстоит снять», а пустота ведомости есть условие П3. Обе строки
// объявляли работу, которой нет:
//
//   - ЯКОРЬ КЛАСТЕРА переведён на объявленное написание задачей #2113, и всё
//     оставшееся снять НЕЛЬЗЯ ни при каком порядке работ: 88 вхождений сеет
//     ПРИМЕНЁННАЯ базовая миграция (ban #5), 4 — миграция перехода, для которой
//     прежнее написание есть ИСТОЧНИК, 2 — пробы, требующие его ОТСУТСТВИЯ в
//     своде, 1 — запись замера, объясняющая сдвиг величин канона. Перенесено в
//     ведомость решённого остаться пятью записями с точными числами;
//   - СОСЕД ПО ПРИСТАВКЕ СХЕМЫ: четыре вхождения СНЯТЫ как утверждения,
//     пережившие свой предмет (шапка пакета метрик и страница наблюдаемости
//     объявляли приставку имён `kacho_iam_`, тогда как дерево производит
//     `kaname_` — 51 различное имя, и таблица под самим утверждением их и
//     перечисляет; плюс два синтетических имени фикстур). Оставшиеся пять
//     цитируют прежнюю приставку как то, ОТ ЧЕГО переехали: инструкция переезда
//     состояния арендатора и два свидетельства о снятом канале уведомления.
//
// Строка, объявляющая неснимаемое, делает условие П3 НЕДОСТИЖИМЫМ by
// construction — тот же класс, что корпус ловит в чужих предикатах готовности.
//
// ТРЕТЬЯ ПОЛОСА НЕ ЗАКРЫТА, У НЕЁ СНЯТА ЛОЖНАЯ ПОЛОВИНА. Заведена граница Б7:
// имя платформы со знаком номера и цифрой есть ССЫЛКА НА ЗАДАЧУ, то есть
// координата репозитория УЧЁТА, а не имя, которым продукт себя зовёт. Таких
// вхождений на поверхности 317, и до этого изменения они стояли в долге —
// половина полосы объявляла работу, которой не будет никогда: ссылка на задачу
// правке не подлежит, она называет живой репозиторий. Остаток полосы стал
// проверяемым числом: 321 вхождение в 120 файлах, и это уже имя платформы, а не
// цитата её трекера.
//
// ПОЧЕМУ ГРАНИЦЕЙ, А НЕ ЗАПИСЯМИ ВЕДОМОСТИ. Ссылка на задачу — принятое в
// дереве написание, она появляется в КАЖДОМ новом разборе; перечень пришлось бы
// дополнять каждым изменением, которое сослалось на задачу, и он не истекал бы
// никогда. Устройство дискриминатора, его две причины и доказательство
// инъекцией по каждой — в шапке isTrackerReference и в пробе
// TestKanameNameResidueTrackerReferenceIsToldByForm.
//
// ИТОГ ЭТОГО ИЗМЕНЕНИЯ, прогоном: неснятыми 2905 вхождений на 12 полосах →
// 2484 на 10.
// ПРИРОСТ +8/+1 ОТ #2360 — ТОТ ЖЕ ИСХОД, ОТДЕЛЬНАЯ ПРИЧИНА. Проба инъекции
// круговой сверки посева личности модуля несёт СИНТЕТИЧЕСКИЕ имена модулей
// (`kacho-alpha`, `kacho-beta`, `kacho-gamma`) — восемь вхождений в одном файле,
// полоса «имя объекта». Фикстура называет объекты платформы теми же именами, что
// и дерево, и назвать их иначе значило бы проверять не то, что проверяется.
//
// Записано в долг полосы Р3, а не прощено: имена синтетические, но снимаются они
// той же работой и тем же переименованием, что остальные.
//
// ПРИРОСТ +2/+1 ОТ #2344, ПРИВЕДЁН К ФАКТУ, А НЕ ПРОЩЁН. Проба перехода через
// собственный REST-фронт (services/iam/internal/authzguard/own_rest_front_hop_test.go)
// называет ДОМЕН ДОВЕРИЯ платформы дважды (`kacho.cloud` в SAN и в конструкторе
// домена) и имя платформы один раз (сегмент `ns/kacho` того же SAN). Это не
// небрежность автора и не то, что правится в его полосе: домен доверия дерева
// сегодня ИМЕННО таков, и проба, назвавшая бы другой, лгала бы о посадке.
//
// Поэтому вхождения записаны в долг тех полос, которые их и снимут (Р10 №2 —
// домен доверия, Р3 — имя платформы отдельным словом), а не в ведомость
// решённого остаться: они снимаемы, просто не этой правкой. Ведомость обязана
// называть ФАКТ дерева — иначе держатель перестаёт мерить рост и начинает мерить
// расхождение с чужой памятью.
var KanameNameResidueDebt = []NameResidueDebt{
	{laneContractCoordinate, 449, 35, "#2133 — имя пакета контракта следует за продуктом (Р14)"},
	{laneSchemaName, 0, 0, "снято #2128: контракт называет схему, которую дерево производит"},
	{laneDatabaseName, 0, 0, "снято: имя базы переименовано вместе со схемой"},
	{laneQualifiedTable, 0, 0, "снято #2128: контракт называет таблицы схемой, которую дерево производит"},
	{laneEnvKnob, 64, 41, "линия дебрендинга: ручки службы"},
	{laneChartKnob, 122, 14, "линия дебрендинга: ключи чарта оператора"},
	{laneClaimAssertion, 74, 26, "О1 эпика #2076 — межрепозиторный контракт, требует окна двух написаний"},
	{laneIdentityHeader, 85, 42, "Р10 №1 — заголовки переданной личности"},
	{laneClusterAnchor, 0, 0, "закрыто #2113: переход состоялся, неснимаемое перенесено в ведомость решённого остаться"},
	{laneSchemaPrefixKin, 0, 0, "закрыто Р5 эпика #2076: приставка имён метрик приведена к факту"},
	{laneDomainAddress, 577, 169, "Р10 №2 — домен доверия и адреса стенда"},
	{laneObjectName, 623, 221, "Р3 эпика #2076 — витрина оператора"},
	{laneBrandInText, 99, 76, "Р3 эпика #2076 — бренд в прозе и на клиентских страницах"},
	{lanePlatformName, 316, 120, "Р3 эпика #2076 — имя платформы отдельным словом"},
	{borderUnknownForm, 64, 26, "слепая зона распознавателя: рост числа означает новую форму записи"},
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
			LineRest:   string(rs[b:]),
			InChart:    inChart,
		})
		i += n - 1
	}
	return out
}
