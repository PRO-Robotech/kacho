// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// retiredidentityvendorceiling.go — УБЫВАЮЩИЙ ПОТОЛОК привязок к снимаемому
// издателю личности (задача #2730).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Службы прежнего провайдера личности (Hydra — выдача токена, Kratos — сессия и
// самообслуживание) снимаются. Финальный предикат снятия существовал только в
// отчёте: его никто не прогонял, и РОСТ числа привязок не замечал никто. Здесь
// он живёт в дереве гейтом.
//
// Форма — потолок, и он УБЫВАЮЩИЙ. В дереве записано текущее число; гейт
// краснеет при его РОСТЕ и требует переписать запись при УБЫВАНИИ. Так каждый шаг
// снятия доказывается числом, а не обещанием, и обратный ход стоит отдельного
// решения человека.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ ОБЯЗАН БЫТЬ ВЕРЕН В НУЛЕ — ИМЕННО ТАМ ЕГО ПРОЧТУТ КАК ДОКАЗАТЕЛЬСТВО
//
// Число этого гейта читают один раз всерьёз: на последнем шаге снятия, когда по
// нулю решают «предмет снят целиком, гейт можно убрать». Значит, дыра опасна не
// в 3000 и не в 300, а в НУЛЕ: сценарий отказа выглядит так — тела файлов
// очищены, каталоги издателя остались, гейт печатает ноль и подписывает снятие,
// которого не было.
//
// Отсюда устройство: судится не только ТЕЛО, но и ПУТЬ, и физическое присутствие
// архива, и двоичное содержимое. Ноль этого гейта означает «в трёх деревьях нет
// ни строки, ни пути, ни архива, ни двоичного файла, несущих имя издателя» —
// утверждение, которое пустой каталог с очищенными файлами опровергает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЕДИНИЦА СЧЁТА — СТРОКА, И ОНА СКЛАДЫВАЕТСЯ ПО ФАЙЛУ ТАК
//
//	строка текста, несущая привязку     1 единица за строку (три вхождения в
//	                                    одной строке — всё равно одна)
//	путь файла, несущий имя издателя    1 единица за ФАЙЛ, сверх его строк
//	двоичный файл: имя издателя в бай-  1 единица за файл
//	    тах
//	архив                               1 единица за архив, независимо от числа
//	                                    совпадений внутри
//
// Число нельзя читать как «столько мест в коде»: мест меньше, а вхождений
// больше.
//
// ─────────────────────────────────────────────────────────────────────────────
// ТРИ ДЕРЕВА, И ВЕДОМОСТЬ НЕ СУЖАЕТСЯ МОЛЧА
//
// Обход идёт по ТРЁМ деревьям продукта: платформа `kacho` (рабочее дерево
// прогона), служба доступа `kaname` и фундамент `corelib`. Два последних
// читаются ПО ПИНУ из `go.mod` — тем деревом, против которого платформа
// собирается, — а не по случайному состоянию соседнего клона.
//
// Перечень деревьев — `retiredVendorTrees` — ЗАКРЫТ и является предпосылкой.
// Ведомость обязана накрывать его ровно: строка, снятая из ведомости, уводила бы
// целое дерево с суда молча (перепись печатала бы «деревьев два», а нули
// фундамента были бы неотличимы от законного нуля). Поэтому недостающая строка
// ведомости и лишняя строка ведомости — ОТКАЗ, а не «находок ноль», а сама
// предпосылка «деревьев три» проверяется по графу сборки: см.
// `TestRetiredVendorCeilingLedgerCoversEveryTreeOfTheBuildGraph`.
//
// Фундамент сегодня даёт НОЛЬ, и это не повод его не смотреть: движок личности
// вводится именно туда.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАСПОЗНАВАТЕЛЬ СТРОИТСЯ ОТ КЛАССА, А НЕ ОТ ПЕРЕЧНЯ ФОРМ
//
// Перечень форм («переменная окружения вида ЧТОТО_HYDRA_ЧТОТО», «имя службы
// hydra-admin», «алиас подчарта pg-hydra», «столбец hydra_client_id», «имя
// печенья ory_kratos_session», «образ oryd/hydra») — это НЕ класс, а его
// сегодняшние отпечатки: форма, о которой перечень не знает, даёт не красное и
// не зелёное, а молчание.
//
// Класс здесь один и называется так: ТЕКСТ НЕСЁТ ИМЯ СНЯТОГО ИЗДАТЕЛЯ. Имя —
// `hydra`, `kratos` и пространство его образов `oryd/`; вхождение ищется
// подстрокой без учёта регистра. МЕСТО, где текст прочитан, — это отдельный
// вопрос, и мест шесть, по одной оси на каждое:
//
//	vendorAxisPath         путь файла
//	vendorAxisName         строка текста
//	vendorAxisGlued        строка текста, где имя склеено из соседних литералов
//	vendorAxisSurface      строка текста, несущая путь API издателя без его имени
//	vendorAxisBinary       байты двоичного файла
//	vendorAxisArchive*     имя или содержимое архива
//
// Безымянная поверхность — единственное, что именем не ловится: пути API
// издателя, в которых его имени нет. Их перечень здесь НЕ заводится: он уже
// имеет единственный дом — `ProviderSurfaces` в `providersurface.go`.
//
// Две оси из шести сегодня БЕЗ ПРЕДМЕТА, и это сказано числом, а не умолчанием:
// двоичных файлов в платформе ноль (перепись печатает «двоичных 0»), склейкой
// ловится одна строка — собственной инъекции этого гейта. Обе держатся пробами
// на СИНТЕТИКЕ во временном каталоге: фикстура, привязанная к живой строке,
// истекла бы вместе с ней, а эти переживут день, когда предмет приедет.
//
// ─────────────────────────────────────────────────────────────────────────────
// АРХИВ УЗНАЁТСЯ ПО СОДЕРЖИМОМУ, А НЕ ПО ПЕРЕЧНЮ СУФФИКСОВ
//
// Прежде архивом считалось имя, кончающееся на `.tgz`/`.tar.gz`. Перечень
// суффиксов — тот же перечень форм, и обходится он одним переименованием: те же
// байты под именем `.zip` уходили в счётчик «пропущено», то есть зелёное при
// физически присутствующем предмете — ровно тот дефект, против которого плечо
// архива и заведено.
//
// Признак архивности выведен иначе: из ПЕРВЫХ БАЙТОВ самого файла
// (`vendorArchiveFormat`). Чем он лучше перечня: суффикс — это имя, его выбирает
// автор файла и меняет одним `mv`; сигнатура — след того, ЧЕМ файл произведён,
// и подделать её, оставаясь архивом, нельзя. Перечень суффиксов закрыт по
// построению — перечень форматов открыт: формат, которого распознаватель не
// знает, всё равно виден как «не текст» и судится байтами (ось
// `vendorAxisBinary`), то есть промах перечня даёт не молчание, а другую ось.
//
// Архив — находка, если ЛИБО его путь несёт имя издателя, ЛИБО его содержимое
// несёт. Имя подпирает содержимое, содержимое подпирает имя: переименование
// внутри чарта не ослепляет, переименование самого чарта — тоже. Различение
// своих от чужих — ПО ИМЕНИ, а не по количеству: предикат «архивов ноль» снёс бы
// заодно чужое вендоренное (cert-manager, ingress-nginx, postgresql).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ГЕЙТ НЕ ВИДИТ — И ЧЕМ ДОКАЗАНА ПОЛНОТА ЭТОГО СПИСКА
//
// Полнота доказана ДВУМЯ РАЗНЫМИ СПОСОБАМИ, и способ назван у каждой части,
// потому что перечень и обход доказывают разное:
//
//	ПО КАТЕГОРИЯМ ФАЙЛОВ — ОБХОДОМ. Всякий путь, принесённый обходом, попадает
//	ровно в одну категорию: судимое построчно · двоичное · архив · проза.
//	Сумма категорий сверяется с числом обойдённых путей, и расхождение —
//	ОТКАЗ (`errVendorPartition`). Пятой категории, о которой забыли, быть не
//	может: она обрушила бы сверку. Из четырёх категорий выведена из-под суда
//	ровно одна — проза.
//
//	ПО ФОРМАМ ЗАПИСИ ВНУТРИ СТРОКИ — ПЕРЕЧИСЛЕНИЕМ, и это сказано прямо: перечень
//	ниже закрыт словом автора, а не машиной. Его держит проба
//	`TestRetiredVendorClassSubsumesEverySourceArm`, гоняющая по дереву каждое
//	плечо предиката-источника.
//
// Границы:
//
//  1. ПРОЗА. Файлы `.md`/`.mdx` не судятся ни телом, ни путём, как и строки,
//     НАЧИНАЮЩИЕСЯ маркером комментария: упоминание издателя в тексте не
//     привязывает к нему дерево, а утверждение о нём как о действующем судит
//     соседнее надгробие `retiredissuerclaim.go`. Давить числом на надгробия —
//     значит требовать стереть историю.
//  2. ПОРТ. `4444`/`4445` под ось не заведены: порт принадлежит АДРЕСУ, а адрес
//     приезжает настройкой; то же число стоит у нашего собственного стенда и у
//     чужого набора соответствия.
//  3. ИМЯ, СОБРАННОЕ НЕ В ОДНОЙ СТРОКЕ. Склейка соседних литералов в одной
//     строке (`"hy" + "dra"`) ЛОВИТСЯ осью `vendorAxisGlued`. Не ловится сборка
//     через переменную (`v := "hy"; v + "dra"`), через перенос строки и
//     шаблоном сборки: решение о них требует разбора каждого языка дерева, а
//     через переменную не решается и разбором. Измерено на этом коммите: строк,
//     ловящихся ТОЛЬКО склейкой, в платформе ноль — ось заведена не под
//     сегодняшнюю находку, а под форму.
//  4. АДРЕС, ПРОЧИТАННЫЙ ИЗ НАСТРОЙКИ В РАНТАЙМЕ. Предиката не существует;
//     держится обзором.
//  5. АРХИВ В ФОРМАТЕ, КОТОРЫЙ НЕ РАСПАКОВЫВАЕТСЯ ЗДЕСЬ (`xz`, `zstd`, `bzip2`,
//     `7z`). Он не исчезает из суда: путь и сырые байты судятся, а число
//     нераспакованных печатается переписью отдельным счётчиком — молчания нет.
//  6. СВОЙ ХВОСТ. Этот файл и его пробы сами несут имя издателя и в число
//     ВХОДЯТ; входит и соседнее надгробие со своей инъекцией. Хвост измерен и
//     назван рядом с записью потолка. Когда предмет уйдёт, гейт снимается вместе
//     с ним — и число уйдёт в ноль тем же изменением.

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// retiredVendorMarks — имя снятого издателя личности, по которому узнаётся
// класс. Не перечень форм записи: подстрока без учёта регистра накрывает любую
// форму — переменную окружения, имя службы, алиас подчарта, столбец, печенье,
// идентификатор в коде, сегмент пути.
//
// `oryd/` — пространство ОБРАЗОВ издателя; голое `ory` сюда НЕ входит, и это
// решение, а не упущение: под тем же именем издатель публикует БИБЛИОТЕКИ, и
// одна из них — `github.com/ory/fosite` — законно остаётся у собственной
// чеканки токенов. Ось по слову `ory` краснела бы на коде, ради которого снятие
// и делается.
var retiredVendorMarks = []string{"hydra", "kratos", "oryd/"}

// Имена деревьев продукта — как в переписи.
const (
	vendorTreePlatform   = "kacho"   // рабочее дерево прогона
	vendorTreeAccess     = "kaname"  // служба доступа, по пину go.mod
	vendorTreeFoundation = "corelib" // фундамент, по пину go.mod
)

// retiredVendorTrees — ЗАКРЫТЫЙ перечень деревьев под судом. Предпосылка гейта:
// деревьев три, и ведомость накрывает их ровно. Проверяется по графу сборки.
var retiredVendorTrees = []string{vendorTreePlatform, vendorTreeAccess, vendorTreeFoundation}

// retiredVendorTreeModules — чем читается дерево, которое не является рабочим:
// пин модуля из `go.mod`. Единственный дом этого соответствия: и прогон, и
// проверка предпосылки берут его здесь.
var retiredVendorTreeModules = map[string]string{
	vendorTreeAccess:     "github.com/PRO-Robotech/kaname",
	vendorTreeFoundation: "github.com/PRO-Robotech/corelib",
}

// retiredVendorCeilings — УБЫВАЮЩИЙ ПОТОЛОК по дереву. Единица — СТРОКА
// (раскладка единицы — в шапке).
//
// Числа получены прогоном ЭТОГО гейта на ревизии, которой он заведён (команда —
// в шапке пробы `retiredidentityvendorceiling_test.go`); чисел источника здесь
// нет ни одного: 324 и 314, названные отчётом, не воспроизводятся ни одним
// выражением обхода, а 726 посчитаны другим набором плеч и другим корпусом.
//
// Запись меняется ТОЛЬКО вниз и тем же изменением, которым снижено число.
var retiredVendorCeilings = map[string]int{
	// платформа: по пути 25 · по имени 1810 · склейкой 1 · по пути API 45 ·
	// архивов 3. Архивы: два чарта издателя по имени и
	// `services/storage/tools/testdata/rg11-make/live-corpus.json.gz` по
	// СОДЕРЖИМОМУ — архив, которого перечень суффиксов не видел вовсе.
	//
	// ЧИСЛО СНИЖЕНО НА 79 ГРАНИЦЕЙ СЛОВА, а не снятием привязок, и это сказано
	// прямо: прежняя запись 1963 включала 92 строки в 28 файлах, где имя
	// издателя втекло в английское слово семейства `hydrate`. Из них 15 — по
	// три строки в каждом из ПЯТИ файлов блокировок консоли, где лежит чужой
	// пакет `@radix-ui/react-use-is-hydrated`: их снимает обновление чужой
	// зависимости, а не снятие издателя, то есть предикат «ноль» был
	// НЕВЫПОЛНИМ никакой нашей работой. Сегодня строк в файлах блокировок НОЛЬ.
	//
	// НОЛЬ ДОСТИЖИМ, и артефакт этому есть В ДЕРЕВЕ — фундамент. Он судится
	// целиком и на общих основаниях: 466 путей обойдено, 466 судимо построчно,
	// 97 870 строк прочитано, привязок НОЛЬ при потолке НОЛЬ, зелёный. То есть
	// «ноль привязок» тут не равно «ноль прочитанного», и это видно из переписи
	// каждого прогона, а не из чьего-то отчёта.
	//
	// Прежняя редакция этой записи ссылалась на снятие всех привязок «в
	// одноразовой копии» с числами 6380 · 3266 · 466. Ссылка ОТОЗВАНА: того
	// разбора в дереве нет, воспроизвести названные числа нечем, а
	// доказательство, которого нельзя повторить, доказательством не является.
	//
	// Разница 92 против 79 — ХВОСТ этого же изменения: пробы границы слова
	// несут имя издателя в своих синтетических входах и добавили 13 строк.
	//
	// ХВОСТ измерен по всему пакету гейтов и входит в число: 119 строк в 13
	// файлах `internal/repohygiene` (было 106 до проб этой границы). Из них сам
	// гейт с двумя пробами — 50, соседи — 69.
	//
	// 1884 -> 1893: ПОТОЛОК ПОДНЯТ СОЗНАТЕЛЬНО, и причина одна — ХВОСТ четырёх
	// новых проб этого же гейта. Координата: `retiredidentityvendorceiling_injection_test.go`,
	// разделы про смещение приведения и про вторую цену границы. Девять строк
	// суть их синтетические входы: `X_HYDRATE_Y`, `oryd/hydra`, `hydra-admin`,
	// `hydraClaims`, `HydraIssuer`, `kratosURL` и три входа со смещением. Ни
	// одна привязка продукта не вернулась: в службе доступа и фундаменте числа
	// не изменились (1107 и 0).
	//
	// Рост этого рода — плата за то, что пробы работают НАСТОЯЩИМ входом, а не
	// подставным. Снимать его подделкой входа нельзя: проба, спрятавшаяся от
	// собственного гейта, перестаёт быть пробой.
	vendorTreePlatform: 1893,
	// служба доступа по пину `go.mod`: по пути 10 · по имени 1057 ·
	// по пути API 40; архивов и двоичного нет. Граница слова здесь не сняла ни
	// одной строки: слитных написаний два (`hydraadminurl`,
	// `hydraadmintokenenv`), и обе строки несут написание с границей рядом.
	vendorTreeAccess: 1107,
	// фундамент по пину `go.mod`: движок сюда ещё не приехал, и потолок держит
	// ноль. Рабочая копия соседнего клона тут не при чём: платформа собирается
	// против ПИНА, и `replace` на внутренний модуль запрещён нормой
	// (`polyrepo.md`), поэтому иное число в чужом клоне — свойство несведённой
	// работы, а не этого дерева.
	vendorTreeFoundation: 0,
}

// Отказы: там, где судить не по чему. Третья категория не растворяется в зелёном.
var (
	// errVendorEmptyWalk — обход не принёс файлов: «ноль находок» здесь
	// означало бы «ноль прочитанного».
	errVendorEmptyWalk = errors.New("обход пуст")
	// errVendorLedger — ведомость не накрывает закрытый перечень деревьев:
	// снятая строка увела бы дерево с суда молча.
	errVendorLedger = errors.New("ведомость не накрывает перечень деревьев")
	// errVendorPartition — обход и разбиение по категориям разошлись: значит,
	// есть категория, которая не судится и не объявлена границей.
	errVendorPartition = errors.New("разбиение обхода по категориям неполно")
)

// vendorArchive — один архив дерева.
type vendorArchive struct {
	// Name — путь архива в дереве.
	Name string
	// Format — чем опознан: `gzip`, `zip`, `tar`, `xz`, `zstd`, `bzip2`, `7z`.
	Format string
	// Opened — содержимое удалось развернуть. Ложь означает, что судится путь и
	// сырые байты; число таких архивов печатается переписью отдельно.
	Opened bool
	// Text — развёрнутое содержимое либо сырые байты, если развернуть нечем.
	Text string
}

// vendorTreeCorpus — прочитанное одного дерева. Судья получает ТЕЛА, а не пути:
// инъекция обязана звать то же, что исполняется на дереве.
//
// Категории разбиения — четыре, и всякий обойдённый путь попадает ровно в одну:
// Bodies (судится построчно) · Blobs (судится байтами) · Archives (судится
// целиком) · Prose (граница). Walked сверяется с их суммой.
type vendorTreeCorpus struct {
	Bodies   map[string]string
	Blobs    map[string]string
	Archives []vendorArchive
	Prose    []string
	// LinesRead — объём осмотренного, считает читатель.
	LinesRead int
	// Walked — сколько путей принёс обход. Ноль означает «читатель не сказал»,
	// и тогда сверка разбиения берёт сумму категорий.
	Walked int
}

// vendorBinding — одна привязка.
type vendorBinding struct {
	Tree string
	File string
	// Line — номер строки; 0 там, где судится файл целиком (путь, архив, байты).
	Line int
	// Axis — чем поймано.
	Axis string
	Text string
}

// Оси. Названы константами, чтобы инъекция утверждала ОСЬ, а не подстроку текста.
const (
	vendorAxisPath        = "имя издателя в пути"
	vendorAxisName        = "имя издателя в строке"
	vendorAxisGlued       = "имя издателя, склеенное в строке"
	vendorAxisSurface     = "путь API издателя"
	vendorAxisBinary      = "имя издателя в байтах двоичного"
	vendorAxisArchiveName = "имя архива"
	vendorAxisArchiveBody = "содержимое архива"
)

// vendorTreeCensus — объём осмотренного по дереву.
type vendorTreeCensus struct {
	// Разбиение обхода: сумма четырёх обязана равняться Walked.
	Walked   int
	Files    int
	Blobs    int
	Archives int
	Prose    int
	// Sealed — архивов, которые развернуть нечем: судятся путём и сырыми байтами.
	Sealed int
	Lines  int
	// BoundaryDropped — строк, где имя издателя ЕСТЬ, но отдельным словом не
	// стоит: английское `hydrate` и его родня. Цена границы слова, названная
	// числом, а не замолчанная.
	BoundaryDropped int
	// BoundaryWords — РАЗНЫЕ слова, составившие предыдущее число. Новое слово
	// здесь — находка: так видно слитное написание настоящей привязки.
	BoundaryWords []string
	// UpperKept — строк, где имя издателя продолжено ПРОПИСНОЙ буквой и потому
	// ЗАСЧИТАНО привязкой. Вторая цена границы, и она зеркальна первой: правило
	// смотрит на регистр следующего байта, поэтому `hydraClaims` (настоящая
	// привязка) и `HYDRATE` (английское слово прописными) для него НЕРАЗЛИЧИМЫ
	// в принципе. Ложное срабатывание здесь так же возможно, как ложное
	// отрицание выше, и обязано звучать тем же числом — перепись, честная в
	// одну сторону, читается как честная целиком.
	UpperKept int
	// UpperWords — РАЗНЫЕ слова, составившие предыдущее число.
	UpperWords []string

	Bindings  int
	ByPath    int
	ByName    int
	ByGlue    int
	BySurface int
	ByBinary  int
	ByArchive int
	Ceiling   int
}

func (c vendorTreeCensus) String() string {
	return fmt.Sprintf("путей обойдено %d = судимо построчно %d · двоичных %d · архивов %d "+
		"(нераспакованных %d) · прозы %d (граница) · строк прочитано %d · "+
		"привязок %d строк (по пути %d · по имени %d · склейкой %d · по пути API %d · "+
		"двоичных %d · архивов %d) · потолок %d · отброшено границей слова %d строк "+
		"(разных слов %d: %s) · засчитано именем ПРОПИСНЫМИ с прописной следом %d строк "+
		"(разных слов %d: %s)",
		c.Walked, c.Files, c.Blobs, c.Archives, c.Sealed, c.Prose, c.Lines,
		c.Bindings, c.ByPath, c.ByName, c.ByGlue, c.BySurface, c.ByBinary, c.ByArchive,
		c.Ceiling, c.BoundaryDropped, len(c.BoundaryWords), strings.Join(c.BoundaryWords, ", "),
		c.UpperKept, len(c.UpperWords), strings.Join(c.UpperWords, ", "))
}

// vendorCeilingFinding — расхождение числа с записью.
type vendorCeilingFinding struct {
	Tree    string
	Ceiling int
	Actual  int
	Kind    string
	// Coords — адреса, из которых число сложилось: «N · путь», ПО ПУТИ и
	// полностью. Красное без адреса заставляет искать руками, а усечённый
	// перечень — хуже полного: выросшая на единицу координата стоит в нём
	// последней и отрезается ровно тогда, когда нужна.
	Coords []string
}

// Виды расхождений: их два, и лечатся они по-разному.
const (
	// vendorFindingGrown — привязок стало БОЛЬШЕ записанного.
	vendorFindingGrown = "число выросло над потолком"
	// vendorFindingShrunk — привязок стало МЕНЬШЕ: потолок убывающий, запись
	// обязана быть переписана тем же изменением, которым число снижено.
	vendorFindingShrunk = "число убыло, а запись потолка осталась прежней"
)

func (f vendorCeilingFinding) String() string {
	var head string
	switch f.Kind {
	case vendorFindingGrown:
		head = fmt.Sprintf("дерево %s: привязок к снятому издателю %d строк при потолке %d "+
			"(+%d) — %s. Снятие идёт в одну сторону: либо снимите привязку, либо "+
			"объясните рост в задаче и поднимите запись СОЗНАТЕЛЬНО, отдельным решением",
			f.Tree, f.Actual, f.Ceiling, f.Actual-f.Ceiling, f.Kind)
	default:
		head = fmt.Sprintf("дерево %s: привязок к снятому издателю %d строк при потолке %d "+
			"(−%d) — %s. Перепишите потолок на %d тем же изменением: потолок, переживший "+
			"своё число, прощает возврат ровно настолько, насколько успели снять",
			f.Tree, f.Actual, f.Ceiling, f.Ceiling-f.Actual, f.Kind, f.Actual)
	}
	if len(f.Coords) == 0 {
		return head
	}
	return head + fmt.Sprintf("\n  адреса (строк · файл), все %d, по пути — сверьте с "+
		"прошлым прогоном, расхождение и есть координата роста:\n  ", len(f.Coords)) +
		strings.Join(f.Coords, "\n  ")
}

// vendorProseFile — проза: единственная категория, выведенная из-под суда
// целиком, телом И путём. Суффикс здесь решает законно: `.md`/`.mdx` — это
// ОБЪЯВЛЕННЫЙ вид содержимого, а не догадка о физической форме файла.
func vendorProseFile(rel string) bool {
	lower := vendorASCIILower(rel)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".mdx")
}

// vendorArchiveFormat — чем произведён файл, по его ПЕРВЫМ БАЙТАМ. Пусто — не
// архив. Перечня суффиксов здесь нет и быть не должно: суффикс — имя, а имя
// меняется одним `mv`.
func vendorArchiveFormat(data []byte) string {
	switch {
	case len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b:
		return "gzip"
	case len(data) >= 4 && data[0] == 'P' && data[1] == 'K' &&
		(data[2] == 0x03 || data[2] == 0x05 || data[2] == 0x07):
		return "zip"
	case len(data) >= 6 && bytes.HasPrefix(data, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}):
		return "xz"
	case len(data) >= 4 && bytes.HasPrefix(data, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		return "zstd"
	case len(data) >= 3 && bytes.HasPrefix(data, []byte("BZh")):
		return "bzip2"
	case len(data) >= 6 && bytes.HasPrefix(data, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	case len(data) >= 262 && string(data[257:262]) == "ustar":
		return "tar"
	}
	return ""
}

// vendorArchiveTextCap — сколько текста берётся из архива. Архив судится по
// признаку «принадлежит издателю», а не по числу совпадений; предел держит
// прогон конечным на чужом вендоренном чарте.
const vendorArchiveTextCap = 4 << 20

// vendorArchiveText — развёрнутое содержимое архива и признак, удалось ли
// развернуть. Не удалось — судятся сырые байты, а счётчик нераспакованных
// печатается переписью: слепая зона названа числом, а не замолчана.
func vendorArchiveText(format string, data []byte) (string, bool) {
	cut := func(s string) string {
		if len(s) > vendorArchiveTextCap {
			return s[:vendorArchiveTextCap]
		}
		return s
	}
	switch format {
	case "gzip":
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return cut(string(data)), false
		}
		defer func() { _ = gz.Close() }()
		// Разжатый поток tar несёт ИМЕНА записей открытым текстом в заголовках,
		// поэтому отдельный проход tar-читателем ничего не добавляет.
		out, err := io.ReadAll(io.LimitReader(gz, vendorArchiveTextCap))
		if err != nil && len(out) == 0 {
			return cut(string(data)), false
		}
		return string(out), true
	case "zip":
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return cut(string(data)), false
		}
		var b strings.Builder
		for _, f := range zr.File {
			if b.Len() >= vendorArchiveTextCap {
				break
			}
			b.WriteString(f.Name)
			b.WriteString("\n")
			rc, err := f.Open()
			if err != nil {
				continue
			}
			chunk, _ := io.ReadAll(io.LimitReader(rc, int64(vendorArchiveTextCap-b.Len())))
			_ = rc.Close()
			b.Write(chunk)
		}
		return b.String(), true
	case "tar":
		// Заголовки tar — открытый текст, разворачивать нечего.
		return cut(string(data)), true
	default:
		// xz · zstd · bzip2 · 7z — стандартной библиотекой не разворачиваются.
		return cut(string(data)), false
	}
}

// vendorTextBytes — файл ТЕКСТ. Пустой файл — текст: он судится путём, и это
// ровно тот случай, ради которого гейт верен в нуле.
func vendorTextBytes(data []byte) bool {
	return !bytes.ContainsRune(data, 0) && utf8.Valid(data)
}

// vendorCorpusFromPaths — корпус дерева из перечня относительных путей. Каждый
// путь попадает ровно в одну категорию; Walked считает все.
func vendorCorpusFromPaths(root string, rels []string) vendorTreeCorpus {
	corpus := vendorTreeCorpus{Bodies: map[string]string{}, Blobs: map[string]string{}}
	for _, rel := range rels {
		corpus.Walked++
		if vendorProseFile(rel) {
			corpus.Prose = append(corpus.Prose, rel)
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs) // #nosec G304 -- путь получен обходом дерева под его корнем
		if err != nil {
			// Нечитаемый путь всё равно судится ПУТЁМ: он физически есть.
			corpus.Blobs[rel] = ""
			continue
		}
		if format := vendorArchiveFormat(data); format != "" {
			text, opened := vendorArchiveText(format, data)
			corpus.Archives = append(corpus.Archives,
				vendorArchive{Name: rel, Format: format, Opened: opened, Text: text})
			continue
		}
		if !vendorTextBytes(data) {
			corpus.Blobs[rel] = string(data)
			continue
		}
		body := string(data)
		corpus.Bodies[rel] = body
		corpus.LinesRead += strings.Count(body, "\n") + 1
	}
	return corpus
}

// vendorGlueJoin — склейка соседних строковых литералов: `"hy" + "dra"`,
// `'hy'+'dra'`, `"hy" "dra"`. Имя, собранное ЧЕРЕЗ ПЕРЕМЕННУЮ или через перенос
// строки, сюда не попадает и объявлено границей (шапка, п. 3).
var vendorGlueJoin = regexp.MustCompile("[\"'`]\\s*\\+?\\s*[\"'`]")

// vendorCarriesMark — текст несёт имя издателя ГДЕ-НИБУДЬ, без разбора границ.
// Это ПРЕ-ФИЛЬТР, а не признак привязки: он отсеивает файлы, где искать нечего,
// и он же служит знаменателем для счётчика отброшенных границей слова. Текст
// обязан быть уже в нижнем регистре.
func vendorCarriesMark(lower string) bool {
	for _, m := range retiredVendorMarks {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// vendorASCIILower — приведение к нижнему регистру ПО БАЙТАМ, только латиница.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ СВОЁ, А НЕ БИБЛИОТЕЧНОЕ
//
// Смещение ищется в приведённом тексте, а байт границы читается в ИСХОДНОМ, и
// это верно только пока приведение СОХРАНЯЕТ ДЛИНУ. Библиотечное её не
// сохраняет: `İ` (U+0130) даёт две руны, а на невалидной последовательности
// каждый негодный байт превращается в U+FFFD и РАСТЁТ ВТРОЕ. Тогда смещение,
// найденное в одном тексте, указывает в другом мимо — и промах идёт в сторону
// ЗАНИЖЕНИЯ: граница читается по чужому байту, привязка не засчитывается,
// потолок пропускает её и называет это успехом.
//
// Радиус на день правки НУЛЕВОЙ: ни один путь, ни одна строка, ни одно
// двоичное и ни один из шести архивов дерева смещения не дают. Механизм
// оживает от первого архива формата, который разворачивается в невалидную
// последовательность, — молча.
//
// Приведение по байтам годится здесь по построению: метки (`hydra`, `kratos`,
// `oryd/`) — ASCII, и признак границы — «строчная буква латиницы» — тоже ASCII.
// Всё, что вне ASCII, границей является в обоих приведениях одинаково.
func vendorASCIILower(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if b == nil {
				b = []byte(s)
			}
			b[i] = c + ('a' - 'A')
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}

// vendorLowerLetter — строчная буква латиницы. Ровно она продолжает СЛОВО:
// прописная буква, цифра, разделитель и конец строки — всё это границы токена в
// любом из здешних написаний (camelCase, SNAKE_CASE, kebab-case, путь, точка).
func vendorLowerLetter(b byte) bool { return b >= 'a' && b <= 'z' }

// vendorMarkBoundedIn — текст несёт имя издателя КАК ОТДЕЛЬНОЕ СЛОВО.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ГРАНИЦА
//
// Подстрока без границы засчитывает привязкой английское слово `hydrate`:
// `hydrate`, `hydrated`, `setHydrated`, `hydrateStringListFields`,
// `hydrateHealthCheck`, `hydrates` — шесть разных слов, ни одно из которых к
// издателю отношения не имеет. Измерено на ревизии заведения границы: 92 строки
// в 28 файлах платформы были привязаны ТОЛЬКО так, и 15 из них — три строки в
// каждом из пяти файлов блокировок консоли, где лежит чужой пакет
// `@radix-ui/react-use-is-hydrated`. Пока они считались, предикат «ноль»
// НЕВЫПОЛНИМ никакой работой: их снимает не снятие издателя, а обновление
// чужой зависимости.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГРАНИЦА ОДНОСТОРОННЯЯ — ТОЛЬКО СПРАВА
//
// Слева границы не требуется намеренно: имя приходит вторым корнем сплошь и
// рядом (`stubHydra`, `testHydraIss`, `newHydra`, `tryKratosSession`), и
// требование границы слева снесло бы настоящие привязки. Справа же продолжение
// СТРОЧНОЙ буквой означает, что имя втекло в другое слово: `hydra` + `te`.
// Прописная буква границей ЯВЛЯЕТСЯ — это шов camelCase (`hydraClaims`,
// `kratosURL`), как и `_`, `-`, `.`, `/`, кавычка, цифра и конец строки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ГРАНИЦА СТОИТ — НАЗВАНО ЧИСЛОМ В ОБЕ СТОРОНЫ
//
// Слитное написание строчными (`hydraadminurl`, `hydraadmintokenenv` — два
// таких слова в службе доступа) правило отбрасывает, хотя это настоящие
// привязки. Измерено: строк, привязанных ТОЛЬКО ими, НОЛЬ — на тех же строках
// стоит написание с границей. Чтобы завтрашнее такое слово не пропало молча,
// перепись печатает отдельный счётчик `отброшено границей слова` вместе с
// РАЗНЫМИ словами, которые его составили: новое слово в этом перечне видно
// сразу и разбирается как находка.
//
// ЛОЖНОЕ СРАБАТЫВАНИЕ названо тем же способом, иначе перепись, честная в одну
// сторону, читалась бы как честная целиком. Правило смотрит на регистр
// следующего байта, поэтому имя ПРОПИСНЫМИ, продолженное прописной, неотличимо
// от английского слова прописными: `HYDRAADMIN` и `HYDRATE` для него одно и то
// же. Шов camelCase сюда не входит — там прописная следом есть начало второго
// корня, то есть законная граница. Счётчик `засчитано именем ПРОПИСНЫМИ с
// прописной следом` печатается рядом с первым; сегодня он НОЛЬ по всем трём
// деревьям.
//
// Имя, кончающееся разделителем (`oryd/`), границу несёт в самом разделителе:
// искать его продолжение незачем.
func vendorMarkBoundedIn(text string) bool {
	lower := vendorASCIILower(text)
	for _, m := range retiredVendorMarks {
		if strings.HasSuffix(m, "/") {
			if strings.Contains(lower, m) {
				return true
			}
			continue
		}
		for idx := 0; ; {
			k := strings.Index(lower[idx:], m)
			if k < 0 {
				break
			}
			end := idx + k + len(m)
			if end >= len(text) || !vendorLowerLetter(text[end]) {
				return true
			}
			idx = end
		}
	}
	return false
}

// vendorMarkWordsContinuedByUpper — РАЗНЫЕ слова, где имя издателя написано
// ПРОПИСНЫМИ и продолжено прописной буквой.
//
// Это ЕДИНСТВЕННАЯ форма, в которой ложное срабатывание границы неотличимо от
// настоящей привязки. Шов camelCase (`hydraClaims`, `HydraIssuer`) сюда НЕ
// входит: там имя написано смешанным регистром, а прописная следом — начало
// второго корня, то есть законная граница. А вот `HYDRATE` — имя прописными и
// прописная следом, ровно как у законного `HYDRAADMIN`: правило смотрит на
// регистр следующего байта и различить их не может В ПРИНЦИПЕ.
//
// Зачем считать отдельно: первая цена границы (ложное отрицание) печатается
// числом и словами, и перепись, честная в одну сторону, читается как честная
// целиком. Обе цены названы числом, иначе вторая молчит.
func vendorMarkWordsContinuedByUpper(text string, into map[string]struct{}) {
	lower := vendorASCIILower(text)
	for _, m := range retiredVendorMarks {
		if strings.HasSuffix(m, "/") {
			continue
		}
		for idx := 0; ; {
			k := strings.Index(lower[idx:], m)
			if k < 0 {
				break
			}
			at := idx + k
			end := at + len(m)
			idx = end
			if end >= len(text) || !vendorUpperLetter(text[end]) {
				continue
			}
			// Само имя обязано быть ПРОПИСНЫМИ: смешанный регистр — это шов
			// camelCase, а он законная граница, а не спорный случай.
			allUpper := true
			for i := at; i < end; i++ {
				if !vendorUpperLetter(text[i]) {
					allUpper = false
					break
				}
			}
			if !allUpper {
				continue
			}
			lo, hi := at, end
			for lo > 0 && vendorWordByte(text[lo-1]) {
				lo--
			}
			for hi < len(text) && vendorWordByte(text[hi]) {
				hi++
			}
			into[text[lo:hi]] = struct{}{}
		}
	}
}

// vendorUpperLetter — прописная буква латиницы.
func vendorUpperLetter(b byte) bool { return b >= 'A' && b <= 'Z' }

// vendorWordByte — байт, продолжающий слово.
func vendorWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

// vendorMarkWordsWithoutBoundary — РАЗНЫЕ слова, в которых имя издателя втекло в
// другое слово. Перечень печатается переписью: цена границы названа словами, а
// не оценкой.
func vendorMarkWordsWithoutBoundary(text string, into map[string]struct{}) {
	isWord := vendorWordByte
	lower := vendorASCIILower(text)
	for _, m := range retiredVendorMarks {
		if strings.HasSuffix(m, "/") {
			continue
		}
		for idx := 0; ; {
			k := strings.Index(lower[idx:], m)
			if k < 0 {
				break
			}
			at := idx + k
			end := at + len(m)
			idx = end
			if end >= len(text) || !vendorLowerLetter(text[end]) {
				continue
			}
			lo := at
			for lo > 0 && isWord(text[lo-1]) {
				lo--
			}
			hi := end
			for hi < len(text) && isWord(text[hi]) {
				hi++
			}
			into[text[lo:hi]] = struct{}{}
		}
	}
}

// vendorPathAxis — путь несёт имя издателя ОТДЕЛЬНЫМ СЛОВОМ. `oryd/` берётся без
// косой черты: в пути это каталог, и следом за ним стоит разделитель.
func vendorPathAxis(rel string) bool {
	stems := make([]string, 0, len(retiredVendorMarks))
	for _, m := range retiredVendorMarks {
		stems = append(stems, strings.TrimSuffix(m, "/"))
	}
	lower := vendorASCIILower(rel)
	for _, m := range stems {
		for idx := 0; ; {
			k := strings.Index(lower[idx:], m)
			if k < 0 {
				break
			}
			end := idx + k + len(m)
			if end >= len(rel) || !vendorLowerLetter(rel[end]) {
				return true
			}
			idx = end
		}
	}
	return false
}

// vendorLineAxis — чем строка привязана к издателю. Пусто — не привязана.
//
// Принимает строку В ИСХОДНОМ РЕГИСТРЕ: граница слова читается по нему, и
// приведение к нижнему регистру ДО разбора стёрло бы шов camelCase, то есть
// ровно ту границу, ради которой правило заведено.
func vendorLineAxis(line string) string {
	if vendorMarkBoundedIn(line) {
		return vendorAxisName
	}
	if vendorMarkBoundedIn(vendorGlueJoin.ReplaceAllString(line, "")) {
		return vendorAxisGlued
	}
	// Безымянная поверхность берётся у единственного дома — словаря путей
	// поставщика. Своего перечня здесь нет.
	lower := vendorASCIILower(line)
	for _, s := range ProviderSurfaces {
		if strings.Contains(lower, vendorASCIILower(s.Path)) {
			return vendorAxisSurface
		}
	}
	return ""
}

// vendorCommentLine — строка НАЧИНАЕТСЯ маркером комментария.
//
// Маркеры выведены обходом дерева, а не памятью: измерены префиксы строк,
// несущих имя издателя. Форма `--`/`*` требует ПРОБЕЛА следом — без него это не
// комментарий, а флаг командной строки (`--env-var "…HYDRA_PUBLIC_PORT…"`),
// то есть настоящая привязка; измерено 15 таких строк на `--` в платформе.
func vendorCommentLine(line string) bool {
	s := strings.TrimLeft(line, " \t")
	switch {
	case strings.HasPrefix(s, "#"), strings.HasPrefix(s, "//"),
		strings.HasPrefix(s, "<!--"), strings.HasPrefix(s, ";"):
		return true
	case s == "--" || s == "*":
		return true
	case strings.HasPrefix(s, "-- "), strings.HasPrefix(s, "--\t"),
		strings.HasPrefix(s, "* "), strings.HasPrefix(s, "*\t"):
		return true
	}
	return false
}

// vendorLineHasUpperContinuation — в строке есть имя издателя, продолженное
// прописной буквой.
func vendorLineHasUpperContinuation(text string) bool {
	seen := map[string]struct{}{}
	vendorMarkWordsContinuedByUpper(text, seen)
	return len(seen) > 0
}

// vendorArchiveAxis — чем архив привязан к издателю. Пусто — не привязан.
// Судится ВЕСЬ путь, а не только имя файла: каталог издателя — такая же
// привязка, как имя архива.
func vendorArchiveAxis(a vendorArchive) string {
	if vendorPathAxis(a.Name) {
		return vendorAxisArchiveName
	}
	if vendorMarkBoundedIn(a.Text) {
		return vendorAxisArchiveBody
	}
	return ""
}

// judgeRetiredVendorCeiling — тело гейта над прочитанным. Инъекция зовёт ЕГО же.
func judgeRetiredVendorCeiling(
	corpora map[string]vendorTreeCorpus,
	ceilings map[string]int,
) ([]vendorCeilingFinding, map[string]vendorTreeCensus, []vendorBinding, error) {
	// ПРЕДПОСЫЛКА: ведомость накрывает закрытый перечень деревьев РОВНО.
	// Снятая строка увела бы дерево с суда молча; лишняя означала бы, что
	// перечень деревьев и ведомость разошлись.
	if len(ceilings) != len(retiredVendorTrees) {
		return nil, nil, nil, fmt.Errorf("%w: деревьев объявлено %d, строк ведомости %d",
			errVendorLedger, len(retiredVendorTrees), len(ceilings))
	}
	for _, tree := range retiredVendorTrees {
		if _, ok := ceilings[tree]; !ok {
			return nil, nil, nil, fmt.Errorf("%w: у дерева %q нет строки — оно ушло бы с суда "+
				"молча, а его нули были бы неотличимы от законного нуля", errVendorLedger, tree)
		}
	}

	census := make(map[string]vendorTreeCensus, len(retiredVendorTrees))
	var findings []vendorCeilingFinding
	var bindings []vendorBinding

	for _, tree := range retiredVendorTrees {
		corpus, ok := corpora[tree]
		if !ok {
			return nil, census, nil, fmt.Errorf("%w: дерево %q не обойдено — его число "+
				"неизвестно, и «ноль находок» означало бы «ноль прочитанного»", errVendorEmptyWalk, tree)
		}
		parts := len(corpus.Bodies) + len(corpus.Blobs) + len(corpus.Archives) + len(corpus.Prose)
		if parts == 0 {
			return nil, census, nil, fmt.Errorf("%w: дерево %q принесло ноль файлов", errVendorEmptyWalk, tree)
		}
		walked := corpus.Walked
		if walked == 0 {
			walked = parts
		}
		if walked != parts {
			return nil, census, nil, fmt.Errorf("%w: дерево %q — путей обойдено %d, "+
				"разложено по категориям %d: есть категория, которая не судится и не "+
				"объявлена границей", errVendorPartition, tree, walked, parts)
		}

		boundaryWords := map[string]struct{}{}
		upperWords := map[string]struct{}{}
		c := vendorTreeCensus{
			Walked:   walked,
			Blobs:    len(corpus.Blobs),
			Archives: len(corpus.Archives),
			Prose:    len(corpus.Prose),
			Lines:    corpus.LinesRead,
			Ceiling:  ceilings[tree],
		}

		add := func(file string, line int, axis, text string) {
			bindings = append(bindings, vendorBinding{
				Tree: tree, File: file, Line: line, Axis: axis, Text: text})
		}

		rels := make([]string, 0, len(corpus.Bodies))
		for rel := range corpus.Bodies {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		for _, rel := range rels {
			// Проза отсеивается ЗДЕСЬ, а не только у читателя: инъекция зовёт
			// судью напрямую, и правило отбора корпуса обязано жить в нём.
			if vendorProseFile(rel) {
				c.Prose++
				continue
			}
			c.Files++
			if vendorPathAxis(rel) {
				c.ByPath++
				add(rel, 0, vendorAxisPath, rel)
			}
			body := corpus.Bodies[rel]
			lowerBody := vendorASCIILower(body)
			// Быстрый путь берёт ПРЕ-ФИЛЬТР без границ: файл, где имени нет
			// вовсе, дальше не читается, а файл, где оно втекло в другое слово,
			// читается — иначе счётчик отброшенных границей был бы слеп ровно
			// на свой предмет.
			if !vendorCarriesMark(lowerBody) && vendorLineAxis(body) == "" {
				continue
			}
			lines := strings.Split(body, "\n")
			lowerLines := strings.Split(lowerBody, "\n")
			for i, line := range lines {
				if vendorCommentLine(line) {
					continue
				}
				axis := vendorLineAxis(line)
				if axis != vendorAxisName && axis != vendorAxisGlued &&
					vendorCarriesMark(lowerLines[i]) {
					// Имя в строке есть, но отдельным словом не стоит: строка
					// отброшена ГРАНИЦЕЙ. Цена границы — число и слова.
					c.BoundaryDropped++
					vendorMarkWordsWithoutBoundary(line, boundaryWords)
				}
				if axis == vendorAxisName {
					// Вторая цена границы: строка засчитана, а регистр
					// следующего байта отличить привязку от английского слова
					// прописными не может.
					before := len(upperWords)
					vendorMarkWordsContinuedByUpper(line, upperWords)
					if len(upperWords) > before || vendorLineHasUpperContinuation(line) {
						c.UpperKept++
					}
				}
				switch axis {
				case vendorAxisName:
					c.ByName++
				case vendorAxisGlued:
					c.ByGlue++
				case vendorAxisSurface:
					c.BySurface++
				default:
					continue
				}
				add(rel, i+1, axis, strings.TrimSpace(line))
			}
		}

		blobs := make([]string, 0, len(corpus.Blobs))
		for rel := range corpus.Blobs {
			blobs = append(blobs, rel)
		}
		sort.Strings(blobs)
		for _, rel := range blobs {
			if vendorPathAxis(rel) {
				c.ByPath++
				add(rel, 0, vendorAxisPath, rel)
			}
			if vendorMarkBoundedIn(corpus.Blobs[rel]) {
				c.ByBinary++
				add(rel, 0, vendorAxisBinary, rel)
			}
		}

		archives := append([]vendorArchive(nil), corpus.Archives...)
		sort.Slice(archives, func(i, j int) bool { return archives[i].Name < archives[j].Name })
		for _, a := range archives {
			if !a.Opened {
				c.Sealed++
			}
			axis := vendorArchiveAxis(a)
			if axis == "" {
				continue
			}
			c.ByArchive++
			add(a.Name, 0, axis, a.Name)
		}

		for w := range boundaryWords {
			c.BoundaryWords = append(c.BoundaryWords, w)
		}
		sort.Strings(c.BoundaryWords)
		for w := range upperWords {
			c.UpperWords = append(c.UpperWords, w)
		}
		sort.Strings(c.UpperWords)
		c.Bindings = c.ByPath + c.ByName + c.ByGlue + c.BySurface + c.ByBinary + c.ByArchive
		census[tree] = c

		switch {
		case c.Bindings > c.Ceiling:
			findings = append(findings, vendorCeilingFinding{
				Tree: tree, Ceiling: c.Ceiling, Actual: c.Bindings, Kind: vendorFindingGrown,
				Coords: vendorCoords(bindings, tree)})
		case c.Bindings < c.Ceiling:
			findings = append(findings, vendorCeilingFinding{
				Tree: tree, Ceiling: c.Ceiling, Actual: c.Bindings, Kind: vendorFindingShrunk,
				Coords: vendorCoords(bindings, tree)})
		}
	}
	return findings, census, bindings, nil
}

// vendorCoords — адреса дерева: «N · путь», ПО ПУТИ и все. Порядок выбран не для
// красоты: по пути два прогона сравнимы построчно, и координата роста видна
// разностью. По убыванию числа она была бы в самом низу — там, где перечень и
// обрезают.
func vendorCoords(bindings []vendorBinding, tree string) []string {
	byFile := map[string]int{}
	for _, b := range bindings {
		if b.Tree == tree {
			byFile[b.File]++
		}
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, fmt.Sprintf("%5d · %s", byFile[f], f))
	}
	return out
}
