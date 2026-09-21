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
	// платформа: по пути 17 · по имени 1586 · склейкой 1 · по пути API 41 ·
	// архивов 3. Архивы: два чарта издателя по имени и
	// `services/storage/tools/testdata/rg11-make/live-corpus.json.gz` по
	// СОДЕРЖИМОМУ — архив, которого перечень суффиксов не видел вовсе: он не
	// кончается на `.tgz`, уходил в «пропущено» и не был ничьим числом.
	//
	// ХВОСТ измерен по всему пакету гейтов и входит в число: 106 строк в 13
	// файлах `internal/repohygiene`. Из них сам гейт с двумя пробами — 37
	// (в самом гейте одна строка: `retiredVendorMarks`; шапка и комментарии не
	// судятся), соседи — 69: словарь поверхностей поставщика с пробами 39,
	// надгробие издателя с инъекцией 19, прочие 11.
	//
	// СНИЖЕНО 1963 → 1648 (−315) снятием механизма сессии чужого поставщика на
	// крае: пять файлов механизма, их ссылки в пробах и гейтах края, четыре
	// ручки и устаревшая подсказка оператора. Число измерено ЭТИМ гейтом на
	// коммите, которым снижение внесено; разбивка выше — из той же переписи.
	vendorTreePlatform: 1648,
	// служба доступа по пину `go.mod`: по пути 10 · по имени 1057 ·
	// по пути API 40; архивов и двоичного нет
	vendorTreeAccess: 1107,
	// фундамент по пину `go.mod`: движок сюда ещё не приехал, и потолок держит ноль
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
		"двоичных %d · архивов %d) · потолок %d",
		c.Walked, c.Files, c.Blobs, c.Archives, c.Sealed, c.Prose, c.Lines,
		c.Bindings, c.ByPath, c.ByName, c.ByGlue, c.BySurface, c.ByBinary, c.ByArchive, c.Ceiling)
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
	lower := strings.ToLower(rel)
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

// vendorCarriesMark — текст несёт имя издателя. Текст обязан быть уже в нижнем
// регистре.
func vendorCarriesMark(lower string) bool {
	for _, m := range retiredVendorMarks {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// vendorPathAxis — путь несёт имя издателя. `oryd/` берётся без косой черты:
// в пути это каталог.
func vendorPathAxis(rel string) bool {
	lower := strings.ToLower(rel)
	for _, m := range retiredVendorMarks {
		if strings.Contains(lower, strings.TrimSuffix(m, "/")) {
			return true
		}
	}
	return false
}

// vendorLineAxis — чем строка привязана к издателю. Пусто — не привязана.
func vendorLineAxis(lower string) string {
	if vendorCarriesMark(lower) {
		return vendorAxisName
	}
	if vendorCarriesMark(vendorGlueJoin.ReplaceAllString(lower, "")) {
		return vendorAxisGlued
	}
	// Безымянная поверхность берётся у единственного дома — словаря путей
	// поставщика. Своего перечня здесь нет.
	for _, s := range ProviderSurfaces {
		if strings.Contains(lower, strings.ToLower(s.Path)) {
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

// vendorArchiveAxis — чем архив привязан к издателю. Пусто — не привязан.
// Судится ВЕСЬ путь, а не только имя файла: каталог издателя — такая же
// привязка, как имя архива.
func vendorArchiveAxis(a vendorArchive) string {
	if vendorPathAxis(a.Name) {
		return vendorAxisArchiveName
	}
	if vendorCarriesMark(strings.ToLower(a.Text)) {
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
			lowerBody := strings.ToLower(body)
			if vendorLineAxis(lowerBody) == "" {
				continue // быстрый путь: в файле нет ни одного признака класса
			}
			lines := strings.Split(body, "\n")
			lowerLines := strings.Split(lowerBody, "\n")
			for i, line := range lines {
				if vendorCommentLine(line) {
					continue
				}
				axis := vendorLineAxis(lowerLines[i])
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
			if vendorCarriesMark(strings.ToLower(corpus.Blobs[rel])) {
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
