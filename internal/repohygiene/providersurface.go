// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// providersurface.go — разбор «до внешнего поставщика удостоверений достаёт
// ровно то, что названо ведомостью» (задача #900, фаза Ф4 эпика #896).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Платформа переезжает на СВОЮ чеканку токенов. Переезд идёт фазами, и в каждой
// внешний поставщик ещё жив: снять его целиком нельзя, пока вторая полоса приёма
// (токен нашего издателя против края) не доказана сквозным вызовом.
//
// Пока он жив, опасно не то, что он есть, а то, что поверхность к нему РАСТЁТ
// незамеченной. Возвращается и разрастается такое не решением, а по одному
// вызову: «здесь бы сюда сходить» — и через полгода снятие снова стоит перед
// поверхностью, которую никто не переписывал.
//
// Гейт делает поверхность ВЕДОМОСТЬЮ: каждое место прод-кода, говорящее с
// поставщиком по его API, названо поимённо вместе с тем, ЧТО оно у него просит.
// Новое место — находка. Новая просьба у названного места — находка. Запись, у
// которой больше нет предмета, — тоже находка: по мере снятия ведомость обязана
// СОКРАЩАТЬСЯ, а не переживать свой предмет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРЕДИКАТ — ПУТИ ЕГО API, А НЕ СЛОВО «HYDRA»
//
// Слово в дереве остаётся законным и во множестве мест: имена настроек
// (`KACHO_HYDRA_ADMIN_URL`), колонка зеркала клиента в применённых миграциях,
// разбор самого переезда в комментариях. Гейт по слову краснел бы на исправном
// дереве — и был бы снят первым же обходом.
//
// Различает не слово, а РАЗГОВОР. Достать до поставщика значит заговорить на его
// API, а его API — это его пути. Они пишутся строковым литералом (целиком или
// хвостом к базовому адресу), и другого способа их назвать нет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЗДЕСЬ НЕТ И ПОЧЕМУ
//
// В словаре НЕТ `/.well-known/jwks.json`. Этот путь неоднозначен by
// construction: по нему отдаёт набор ключей и поставщик, и МЫ САМИ — зеркало на
// внутреннем слушателе службы прав и наша собственная публикация. Гейт,
// включивший его, краснел бы на нашем обработчике, то есть на коде, ради
// которого переезд и делается. Отличить наш от чужого по пути нельзя — их
// различает АДРЕС, а адрес приезжает настройкой и в исходнике не стоит.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА НАЗВАНА ВСЛУХ
//
// Гейт НЕ поймает обращение к поставщику, чей путь собран по частям в рантайме
// (`"/admin/" + kind + "s"`) либо прочитан из настройки. Такого предиката не
// существует. Что гейт даёт — невозможность ДОБАВИТЬ разговор с поставщиком
// обычным способом, каким его добавляют, и невозможность оставить в ведомости
// запись, которой нечего называть. Чего не даёт — доказательства, что иных
// разговоров нет вовсе; это держится обзором и приёмкой.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// ProviderSurface — одна поверхность API внешнего поставщика: то, что у него
// просят, названное его же путём.
type ProviderSurface struct {
	// Path — путь его API. Совпадение ищется ВХОЖДЕНИЕМ в строковый литерал:
	// путь пишется и целиком, и хвостом к базовому адресу, и с идентификатором
	// вслед за ним.
	Path string
	// What — что этой поверхностью делают. Читается человеком в тексте находки.
	What string
}

// ProviderSurfaces — словарь путей внешнего поставщика.
//
// ПРАВИЛО ОТБОРА, и оно единственное: сюда попадает путь, который обслуживает
// ТОЛЬКО поставщик. Путь, который обслуживаем и мы, маркером разговора с ним
// быть не может — гейт краснел бы на собственном обработчике (см. шапку про
// набор ключей).
var ProviderSurfaces = []ProviderSurface{
	{Path: "/admin/clients", What: "жизненный цикл его OAuth-клиента"},
	{Path: "/admin/oauth2/introspect", What: "проверка живости предъявленного токена"},
	{Path: "/admin/oauth2/auth/sessions/login", What: "снятие сессий входа"},
	{Path: "/admin/trust/grants/jwt-bearer/issuers", What: "доверие издателю утверждения"},
	{Path: "/oauth2/token", What: "выдача токена"},
}

// ProviderLedgerEntry — одна запись ведомости: файл прод-кода, которому РАЗРЕШЕНО
// говорить с поставщиком, и перечень того, о чём он с ним говорит.
type ProviderLedgerEntry struct {
	// File — путь файла от корня дерева, точным совпадением. Не префикс:
	// префикс накрыл бы соседей, которых никто не рассматривал.
	File string
	// Surfaces — пути словаря, которые этому файлу разрешены. Путь вне перечня
	// — находка даже у названного файла: «ещё один вызов туда же» и есть тот
	// способ, каким поверхность растёт.
	Surfaces []string
	// Why — зачем это место существует сегодня.
	Why string
	// Until — при каком факте о дереве запись обязана быть снята. Ведомость
	// сокращается по мере снятия, и предикат снятия стоит рядом с записью, а не
	// в чужой голове.
	Until string
}

// ProviderFinding — одно расхождение ведомости с деревом.
type ProviderFinding struct {
	File string
	Line int
	// Surface — путь поставщика, о котором находка. Пусто у находки про
	// пережившую предмет запись.
	Surface string
	// Kind — вид расхождения. Их три, и лечатся они по-разному.
	Kind string
	// Detail — человеческая часть.
	Detail string
}

// Виды расхождений. Названы константами, чтобы проба инъекции утверждала ВИД, а
// не подстроку текста.
const (
	// ProviderFindingUnledgered — файл говорит с поставщиком и в ведомости не
	// назван.
	ProviderFindingUnledgered = "место не названо ведомостью"
	// ProviderFindingUndeclared — названный файл просит поверхность, которой за
	// ним не объявлено.
	ProviderFindingUndeclared = "поверхность не объявлена за этим местом"
	// ProviderFindingStale — запись ведомости пережила свой предмет.
	ProviderFindingStale = "запись ведомости пережила предмет"
)

// ProviderCensus — объём осмотренного. «Ноль находок» обязано быть отличимо от
// «ноль прочитанного».
type ProviderCensus struct {
	// Files — прочитано непроверочных файлов Go.
	Files int
	// Literals — осмотрено строковых литералов в исполняемой части.
	Literals int
	// Carriers — файлов, где разговор с поставщиком найден.
	Carriers int
	// Reaches — всего мест разговора (файл может нести несколько).
	Reaches int
	// LedgerEntries — записей ведомости на входе.
	LedgerEntries int
	// LedgerSurfaces — объявлений поверхностей в ведомости.
	LedgerSurfaces int
	// ProseMentions — файлов, называющих поставщика ТОЛЬКО в комментарии.
	// Считается отдельно и находкой НЕ является: разбор переезда — законная
	// проза, и гейт, который её ловит, краснеет на собственном объяснении.
	ProseMentions int
}

// providerReach — одно найденное место разговора.
type providerReach struct {
	file    string
	line    int
	surface string
}

// FindProviderSurface разбирает исходники (имя → содержимое) и сверяет найденные
// разговоры с поставщиком против ведомости.
//
// Возвращает находки трёх видов и перепись осмотренного. Ошибка — только на
// неразбираемом исходнике: молчаливый пропуск нечитаемого файла превратил бы
// «не прочитали» в «нарушений нет».
func FindProviderSurface(
	sources map[string]string, ledger []ProviderLedgerEntry,
) ([]ProviderFinding, ProviderCensus, error) {
	census := ProviderCensus{LedgerEntries: len(ledger)}

	allowed := make(map[string]map[string]struct{}, len(ledger))
	for _, e := range ledger {
		set := make(map[string]struct{}, len(e.Surfaces))
		for _, s := range e.Surfaces {
			set[s] = struct{}{}
			census.LedgerSurfaces++
		}
		allowed[e.File] = set
	}

	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	var (
		findings []ProviderFinding
		reaches  []providerReach
	)
	for _, name := range names {
		src := sources[name]
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			return nil, census, fmt.Errorf("разбор %s: %w", name, err)
		}
		census.Files++

		found := 0
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			census.Literals++
			// Разбирается ЗНАЧЕНИЕ литерала, а не его исходный текст: путь
			// пишется и обратными кавычками, и с экранированием, и вердикт не
			// вправе зависеть от того, как автор его записал.
			val, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				val = lit.Value
			}
			for _, s := range ProviderSurfaces {
				if strings.Contains(val, s.Path) {
					found++
					reaches = append(reaches, providerReach{
						file: name, line: fset.Position(lit.Pos()).Line, surface: s.Path,
					})
				}
			}
			return true
		})
		if found > 0 {
			census.Carriers++
			census.Reaches += found
			continue
		}
		if mentionsProviderInProse(file) {
			census.ProseMentions++
		}
	}

	// (1) и (2): места против ведомости.
	seen := map[string]map[string]struct{}{}
	for _, r := range reaches {
		if seen[r.file] == nil {
			seen[r.file] = map[string]struct{}{}
		}
		seen[r.file][r.surface] = struct{}{}

		set, listed := allowed[r.file]
		if !listed {
			findings = append(findings, ProviderFinding{
				File: r.file, Line: r.line, Surface: r.surface,
				Kind:   ProviderFindingUnledgered,
				Detail: surfaceWhat(r.surface),
			})
			continue
		}
		if _, ok := set[r.surface]; !ok {
			findings = append(findings, ProviderFinding{
				File: r.file, Line: r.line, Surface: r.surface,
				Kind:   ProviderFindingUndeclared,
				Detail: surfaceWhat(r.surface),
			})
		}
	}

	// (3): записи против дерева. Проверяется КАЖДАЯ объявленная поверхность, а
	// не только сам файл: запись, у которой снята одна из двух поверхностей,
	// продолжала бы разрешать снятое.
	for _, e := range ledger {
		got := seen[e.File]
		if len(got) == 0 {
			findings = append(findings, ProviderFinding{
				File: e.File, Kind: ProviderFindingStale,
				Detail: "файл больше не говорит с поставщиком ни по одному пути словаря" +
					" — предикат снятия записи: " + e.Until,
			})
			continue
		}
		for _, s := range e.Surfaces {
			if _, ok := got[s]; !ok {
				findings = append(findings, ProviderFinding{
					File: e.File, Surface: s, Kind: ProviderFindingStale,
					Detail: "поверхность за этим местом объявлена, а в файле её больше нет" +
						" — предикат снятия записи: " + e.Until,
				})
			}
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, census, nil
}

// surfaceWhat — что делают этой поверхностью, по словарю.
func surfaceWhat(path string) string {
	for _, s := range ProviderSurfaces {
		if s.Path == path {
			return s.What
		}
	}
	return ""
}

// mentionsProviderInProse — назван ли поставщик в комментарии этого файла.
//
// Считается ОТДЕЛЬНО от находок и находкой НЕ является. Печатается затем, чтобы
// «ноль находок» не читалось как «слова в дереве нет»: слово живёт в разборе
// переезда, в именах настроек и в шапках применённых миграций, и всё это
// законно.
func mentionsProviderInProse(file *ast.File) bool {
	for _, group := range file.Comments {
		if strings.Contains(strings.ToLower(group.Text()), "hydra") {
			return true
		}
	}
	return false
}
