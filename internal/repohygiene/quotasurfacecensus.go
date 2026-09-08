// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// quotasurfacecensus.go — перепись поверхностей слова «квота» по дереву.
//
// # Предмет
//
// Задача продукта #2135: признак «одно слово по всему дереву» даёт число
// кандидатов (262 в теле задачи, 317 на её перемере), и это число будет
// прочитано как «столько-то мест под снос» первым же, кто возьмёт работу, — он
// снесёт лишнее либо не снесёт нужное. Под одним словом в дереве живут РАЗНЫЕ
// предметы, и уходят из них не все.
//
// Приёмка KAN-QUOTA-1 (§2) называет ЧЕТЫРЕ поверхности: авторитет величин
// (уходит из службы доступа целиком), учёт у пяти доменов-владельцев
// (остаётся), учёт самой службы доступа (шесть видов уходят, три
// перевыражаются посадкой) и предел скорости приёма аккаунтов (остаётся).
//
// # Что перепись добавляет к этим четырём и почему
//
// Четыре поверхности объявлены В ГРАНИЦАХ СЛУЖБЫ ДОСТУПА («уходит ли из
// службы?»), а признак задачи обходит ВСЁ дерево. Поэтому четвёрка кандидатов
// не покрывает, и это не пропуск приёмки, а разные рамки. Перепись заводит две
// оставшиеся полосы явно, вместо того чтобы прятать их в «прочее»:
//
//   - «Ч» — ЧУЖОЙ предмет под тем же словом: предел подписчика в потоке
//     изменений, ёмкость проекта в байтах у хранилища, отказ чужой системы
//     хранения, предел браузерного хранилища и слово, в которое признак попал
//     подстрокой. Это не квота счёта ресурсов ни в одном смысле, и снос по
//     списку задел бы её;
//   - «П» — упоминание в прозе или ведомости: комментарий, страница, перечень
//     приёмок. Машинерии тут нет; правится тогда, когда становится ложью.
//
// # Признак переписи ШИРЕ признака задачи, и разница названа числом
//
// Признак задачи (`quota|Quota|InternalLimit|LimitService`) чувствителен к
// регистру и не знает словаря каталога видов. Следствие измерено, а не
// предположено: файл закрытого каталога `services/iam/internal/domain/limit.go`
// — тот самый, вокруг которого стоят пункты 13–15 условия готовности S4, — в
// 317 кандидатов НЕ входит: строчного `quota` и `Quota` в нём нет ни одного, а
// есть `LimitKind`, `CountableKind` и `QUOTA_NOT_PROVISIONED` заглавными.
//
// Поэтому перепись судит по расширенному признаку и печатает ОБЕ величины —
// свою и задачи. Одно число здесь скрыло бы ровно тот файл, ради которого
// работа делается.
//
// # Форма вердикта — ПАРА чисел на каждой поверхности
//
// `DoD S4` п. 1 приёмки требует: предикат, давший ноль, к вердикту не
// допускается, пока не назван его парный замер «давал не ноль». Перепись
// печатает на каждой поверхности «осмотрено» и «найдено», поэтому её сегодняшний
// вывод и есть тот парный замер для стадии, которая ещё не начата.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// quotaCensusExtensions — расширения, по которым идёт обход. Взяты у признака
// задачи #2135 дословно, чтобы числа переписи были сравнимы с 262 и 317.
var quotaCensusExtensions = []string{
	".go", ".proto", ".sql", ".ts", ".tsx", ".mdx", ".py", ".json", ".yaml",
}

// quotaCensusOwnSource — исходник САМОЙ переписи.
//
// Он несёт признаки всех поверхностей сразу — иначе он не мог бы их различать, —
// и потому был бы отнесён к предмету наравне с предметом. Это тот класс, который
// проверка по образцу над сырым текстом заводит себе сама: она не отличает
// предмет от его ОПИСАНИЯ.
//
// Исключены только эти строки, а не каталог гейтов целиком: соседние гейты держат
// свойства учёта и величин по-настоящему, и снятие их — часть работы стадии S4
// (её условие готовности прямо требует, чтобы гейты снятого предмета уходили
// вместе с фикстурами). Спрятав каталог, перепись потеряла бы их из виду.
//
// Исключение ИМЕНОВАНО и самоистекает: гейт требует, чтобы файл в дереве был.
// Переименуют — исключение останется без предмета и станет находкой, а не
// перестанет действовать молча.
const quotaCensusOwnSource = "internal/repohygiene/quotasurfacecensus.go"

// quotaCensusExcludedPrefix — сгенерированное дерево стабов. Исключено тем же
// признаком задачи: стабы правятся генерацией, а не руками, и о поверхностях
// ничего не решают.
const quotaCensusExcludedPrefix = "pkg/api/"

// quotaCensusMark — признак ПЕРЕПИСИ. Шире признака задачи на две оси:
// нечувствителен к регистру (иначе `QUOTA_NOT_PROVISIONED` не виден) и знает
// словарь каталога видов (иначе не виден сам файл каталога).
var quotaCensusMark = regexp.MustCompile(`(?i)quota|InternalLimit|LimitService|LimitKind|CountableKind`)

// quotaTaskMark — признак ЗАДАЧИ #2135, дословно. Держится рядом не ради
// красоты: перепись печатает обе величины, и разница между ними есть слепая
// зона признака задачи, названная числом.
var quotaTaskMark = regexp.MustCompile(`quota|Quota|InternalLimit|LimitService`)

// Коды поверхностей. Латиницей — это идентификаторы (core #17).
const (
	quotaSurfaceAuthority = "A" // авторитет величин — уходит из службы доступа целиком
	quotaSurfaceOwners    = "B" // учёт у пяти доменов-владельцев — остаётся
	quotaSurfaceIAMLedger = "C" // учёт самой службы доступа — 6 уходят, 3 перевыражаются
	quotaSurfaceAdmission = "D" // предел скорости приёма аккаунтов — остаётся
	quotaSurfaceForeign   = "F" // чужой предмет под тем же словом — не квота ресурсов вовсе
	quotaSurfaceProse     = "P" // упоминание в прозе или ведомости
)

// quotaSurfaceVerdict — чем «найдено» является на каждой поверхности. Это и есть
// то, чего в одном числе нет: без вердикта перепись остаётся вторым числом.
var quotaSurfaceVerdict = map[string]string{
	quotaSurfaceAuthority: "ОСТАТОК — уходит из службы доступа целиком (стадия S4)",
	quotaSurfaceOwners:    "ЗАКОННЫЙ ЖИТЕЛЬ — счётчик живёт у владельца ресурса и не трогается",
	quotaSurfaceIAMLedger: "СМЕШАННО — шесть никогда не списывавшихся видов уходят, три действующих перевыражаются посадкой (стадия S3)",
	quotaSurfaceAdmission: "ЗАКОННЫЙ ЖИТЕЛЬ — это защита службы от злоупотребления, а не потолок количества",
	quotaSurfaceForeign:   "НЕ ПРЕДМЕТ — другой смысл того же слова; снос по списку задел бы его",
	quotaSurfaceProse:     "УПОМИНАНИЕ — машинерии нет; правится тогда, когда становится ложью",
}

// quotaCensusOrder — порядок, в котором выбирается ГЛАВНАЯ поверхность файла для
// разбиения. Членство при этом множественное: файл, называющий и авторитет, и
// учёт, принадлежит обоим, и обе величины печатаются. Разбиение нужно отдельно —
// чтобы числа складывались в осмотренное и «сумма больше целого» не выдавалась
// за находку.
//
// Чужой предмет идёт ПЕРВЫМ намеренно: если слово здесь означает не нашу квоту,
// то ни одна наша поверхность его не касается.
var quotaCensusOrder = []string{
	quotaSurfaceForeign,
	quotaSurfaceAdmission,
	quotaSurfaceAuthority,
	quotaSurfaceIAMLedger,
	quotaSurfaceOwners,
	quotaSurfaceProse,
}

// iamServicePrefix — служба доступа. Граница между поверхностями B и C проходит
// ПО МЕСТУ, и это не уловка классификатора: §2 приёмки различает их именно так —
// «у каждого владельца ресурса, в его схеме» против «служба доступа».
const iamServicePrefix = "services/iam/"

// quotaCensusRule — одно именованное правило отнесения.
//
// Правило обязано иметь ПРЕДМЕТ: правило, не совпавшее ни с одним кандидатом,
// есть послабление, пережившее то, что им обозначалось, и гейт роняет прогон на
// нём. Это тот же механизм самоистечения, что у ведомостей освобождений.
type quotaCensusRule struct {
	Surface string
	Code    string
	Why     string

	// Prefixes — путь сам по себе есть признак: каталог общего фундамента
	// учёта или каталог реализации величин в службе доступа.
	Prefixes []string
	// NotPrefixes — вычет из Prefixes для случая, когда внутри каталога живёт
	// файл ДРУГОЙ поверхности.
	NotPrefixes []string
	// Scope — если непусто, правило смотрит только на пути с этими приставками.
	Scope []string
	// NotScope — если непусто, правило НЕ смотрит на пути с этими приставками.
	NotScope []string
	// Token — признак по содержимому.
	Token *regexp.Regexp
	// Fallback — правило применяется только к файлу, который не отнесён ничем.
	Fallback bool
}

// quotaCensusRules — перечень правил.
//
// Каждый токен взят у дерева, а не выдуман: перечень собран перемером кандидатов
// с адъюдикацией остатка, и остаток доводился до нуля добавлением ПОЛОЖИТЕЛЬНЫХ
// правил, а не корзиной «прочее». Корзина «прочее» и есть то, что задача #2135
// запрещает: она возвращает одно число, только под другим именем.
var quotaCensusRules = []quotaCensusRule{
	// ---------- A. авторитет величин ----------
	{
		Surface: quotaSurfaceAuthority, Code: "A1",
		Why:   "объявление службы величин",
		Token: regexp.MustCompile(`InternalLimitService|LimitService`),
	},
	{
		Surface: quotaSurfaceAuthority, Code: "A2",
		Why:   "таблица величин службы доступа",
		Token: regexp.MustCompile(`kaname\.limits`),
	},
	{
		Surface: quotaSurfaceAuthority, Code: "A3",
		Why:   "право чтения величин и группа его держателей",
		Token: regexp.MustCompile(`quota_reader|module-quota-readers`),
	},
	{
		Surface: quotaSurfaceAuthority, Code: "A4",
		Why:   "закрытый каталог видов — предмет решения Д8",
		Token: regexp.MustCompile(`countableKinds|CountableKind|LimitCarrier|LimitKind`),
	},
	{
		Surface: quotaSurfaceAuthority, Code: "A5",
		Why:   "посеянные умолчания величин",
		Token: regexp.MustCompile(`quota_defaults`),
	},
	{
		Surface: quotaSurfaceAuthority, Code: "A6",
		Why: "реализация величин в службе доступа и её контракт",
		Prefixes: []string{
			"proto/kaname/cloud/iam/v1/limit",
			"services/iam/internal/apps/kaname/api/limit/",
			"services/iam/internal/domain/limit.go",
			"services/iam/internal/repo/kaname/pg/limit_repo.go",
			"services/iam/docs/content/api/limit.mdx",
		},
	},
	{
		Surface: quotaSurfaceAuthority, Code: "A7",
		Why:   "консольный раздел величин",
		Token: regexp.MustCompile(`LimitsPage`),
	},
	{
		Surface: quotaSurfaceAuthority, Code: "A8",
		Why:   "порт хранилища величин",
		Token: regexp.MustCompile(`LimitRepo`),
	},

	// ---------- B. учёт у пяти доменов-владельцев ----------
	{
		Surface: quotaSurfaceOwners, Code: "B1",
		Why:      "списывающий триггер и его столбец",
		NotScope: []string{iamServicePrefix},
		Token:    regexp.MustCompile(`kacho_quota_count|quota_used|quota_recount|quota_admit|quota_refuse`),
	},
	{
		Surface: quotaSurfaceOwners, Code: "B2",
		Why:      "таблица учёта у владельца",
		NotScope: []string{iamServicePrefix},
		Token:    regexp.MustCompile(`project_resource_quotas`),
	},
	{
		Surface: quotaSurfaceOwners, Code: "B3",
		Why:      "контракт отказа по учёту",
		NotScope: []string{iamServicePrefix},
		Token:    regexp.MustCompile(`QUOTA_EXCEEDED|QUOTA_NOT_PROVISIONED|ErrQuotaExceeded|ErrQuotaNotProvisioned|quotaRefusal|QuotaErr|quota exceeded`),
	},
	{
		Surface: quotaSurfaceOwners, Code: "B4",
		Why: "общий фундамент учёта и его контракт",
		Prefixes: []string{
			"pkg/quota/",
			"proto/kacho/cloud/quota/",
			"tools/quota-refusal-migration/",
		},
		// ЗДЕСЬ СТОЯЛО ИСКЛЮЧЕНИЕ на контракт чтения учёта личности: он лежал
		// внутри пакета общей формы, а поверхностью был не общей. Предмет
		// исключения УШЁЛ ИЗ ОБЛАСТИ — объявление переехало в собственный
		// контракт службы доступа (kacho#2362, решение `Д9`), — поэтому
		// исключение снято тем же изменением. Оставленное, оно исключало бы
		// несуществующее и унаследовалось бы следующим читателем как описание
		// действительности.
	},
	{
		Surface: quotaSurfaceOwners, Code: "B5",
		Why:      "арендаторское чтение учёта и его поверхность в консоли",
		NotScope: []string{iamServicePrefix},
		Token:    regexp.MustCompile(`v1/quotas|quota\.List|QuotasPage|quotas|Quotas|quota-view|quota\?\.href`),
	},
	{
		Surface: quotaSurfaceOwners, Code: "B6",
		Why:      "потребительская половина ребра к авторитету: объявление, тянущий, курсор",
		NotScope: []string{iamServicePrefix},
		Token:    regexp.MustCompile(`quotaAuthority|QuotaAuthority|QuotaConfig|quota_sync_cursor|LimitSync|quotaiam|quota\.authority|QUOTA_AUTHORITY`),
	},
	{
		Surface: quotaSurfaceOwners, Code: "B7",
		Why:      "страж, порты и разбор учёта у владельца",
		NotScope: []string{iamServicePrefix},
		Token:    regexp.MustCompile(`QuotaGuard|QuotaRepo|QuotaStore|QuotaExecutor|QuotaSchema|QuotaAdmit|quotaWriter|quotaReader|QuotaRow|quotaread|quotadetail|quotapb|quota\.Admit|shared/quota|quota_carrier_lifecycle`),
	},
	{
		Surface: quotaSurfaceOwners, Code: "B8",
		Why:      "имя общего контракта ответа об учёте",
		NotScope: []string{iamServicePrefix},
		Token:    regexp.MustCompile(`kacho\.cloud\.quota|"quota"|quota/v1`),
	},

	// ---------- C. учёт самой службы доступа ----------
	{
		Surface: quotaSurfaceIAMLedger, Code: "C1",
		Why:   "тот же учёт, но в службе доступа: граница B и C проходит по месту (§2)",
		Scope: []string{iamServicePrefix},
		Token: regexp.MustCompile(`kacho_quota_count|quota_used|quota_recount|quota_admit|quota_refuse|project_resource_quotas|QUOTA_EXCEEDED|QUOTA_NOT_PROVISIONED|ErrQuotaExceeded|ErrQuotaNotProvisioned|quotaRefusal|QuotaErr|quota exceeded|v1/quotas|quota\.List|QuotasPage|quotas|Quotas|quota-view|quotaAuthority|QuotaAuthority|QuotaConfig|quota_sync_cursor|LimitSync|quotaiam|QuotaGuard|QuotaRepo|QuotaStore|QuotaExecutor|QuotaSchema|QuotaAdmit|quotaWriter|quotaReader|QuotaRow|quotaread|quotadetail|quotapb|quota\.Admit|shared/quota|quota_carrier_lifecycle|kacho\.cloud\.quota|"quota"|quota/v1`),
	},
	{
		Surface: quotaSurfaceIAMLedger, Code: "C2",
		Why:   "чтение учёта личности — глагол службы доступа",
		Token: regexp.MustCompile(`IdentityQuota|identityQuota`),
	},
	{
		Surface: quotaSurfaceIAMLedger, Code: "C3",
		// Координата ПЕРЕЕХАЛА вместе с предметом (kacho#2362, решение `Д9`):
		// объявление службы чтения ушло из пакета общей формы ответа в
		// собственный контракт службы доступа. Предмет правила не исчез — он
		// сменил место, поэтому правило правится, а не снимается.
		Why:      "контракт чтения учёта личности",
		Prefixes: []string{"proto/kaname/cloud/iam/v1/identity_quota_service.proto"},
	},
	{
		Surface: quotaSurfaceIAMLedger, Code: "C4",
		Why:   "край ведёт пакет общей формы ответа на слушатель службы доступа",
		Scope: []string{"gateway/"},
		Token: regexp.MustCompile(`"quota"`),
	},

	// ---------- D. предел скорости приёма аккаунтов ----------
	{
		Surface: quotaSurfaceAdmission, Code: "D1",
		Why:   "предел скорости приёма аккаунтов: величина здесь — пара «событий и окно»",
		Token: regexp.MustCompile(`account_admission_rate_limit|AdmissionRate|admission_rate|ErrQuotaRateExceeded|QUOTA_RATE`),
	},

	// ---------- F. чужой предмет под тем же словом ----------
	{
		Surface: quotaSurfaceForeign, Code: "F1",
		Why:   "предел подписчика в потоке изменений — счёт подписок, а не ресурсов",
		Token: regexp.MustCompile(`RefusedSubjectQuota|refusedSubjectQuota`),
	},
	{
		Surface: quotaSurfaceForeign, Code: "F2",
		Why:   "ёмкость проекта в БАЙТАХ у хранилища — иной механизм, списывающего триггера нет",
		Token: regexp.MustCompile(`storage quota exceeded`),
	},
	{
		Surface: quotaSurfaceForeign, Code: "F3",
		Why:   "отказ чужой системы хранения, разбираемый по тексту",
		Token: regexp.MustCompile(`no space left on device`),
	},
	{
		Surface: quotaSurfaceForeign, Code: "F4",
		Why:   "предел браузерного хранилища",
		Token: regexp.MustCompile(`QuotaExceededError`),
	},
	{
		Surface: quotaSurfaceForeign, Code: "F5",
		Why:   "слово, в которое признак попал подстрокой: у него нет границы слова",
		Token: regexp.MustCompile(`quotation`),
	},

	// ---------- P. упоминание в прозе или ведомости ----------
	//
	// Правила отступления: они применяются к файлу, которого не отнесло ни одно
	// правило выше. Порядок здесь несущий — иначе страница, называющая
	// машинерию, ушла бы в «упоминание» и пропала бы из работы.
	//
	// Правила «страница документации» здесь НЕТ, и это замер, а не пропуск: оно
	// было написано, прогнано и снято тем же заходом, потому что предмета у него
	// не оказалось ни одного. Каждая страница-кандидат называет машинерию и
	// уходит на свою поверхность раньше отступления. Следствие для читателя:
	// новая страница, поминающая квоты одной прозой, станет НАХОДКОЙ — её отнесёт
	// человек, а не умолчание.
	{
		Surface: quotaSurfaceProse, Code: "P1",
		Why:      "имя приёмки, набора проб или ведомости",
		Fallback: true,
		Token:    regexp.MustCompile(`sub-phase-[a-zA-Z-]*quota|KAN-QUOTA|QUOTA-V2|api/operation/quota`),
	},
	{
		Surface: quotaSurfaceProse, Code: "P3",
		Why:      "упоминание только в комментарии",
		Fallback: true,
	},
}

// quotaCensusFile — один кандидат с отнесением.
type quotaCensusFile struct {
	Path     string
	Surfaces []string // множественное членство, отсортировано
	Primary  string
	Rules    []string // коды сработавших правил
	TaskMark bool     // виден ли признаку задачи #2135
}

// quotaCensusResult — итог обхода.
type quotaCensusResult struct {
	FilesWalked     int // отслеживаемых файлов подходящего расширения
	Candidates      []quotaCensusFile
	TaskCandidates  int // из них видимых признаку задачи #2135
	Unclassified    []string
	RulesWithoutHit []string
	PerSurface      map[string]int
	PerPrimary      map[string]int

	// OwnSourceSeen — исходник переписи найден в дереве. Ложь означает, что
	// исключение осталось без предмета: файл переехал или переименован.
	OwnSourceSeen bool
}

// hasAnyPrefix — путь начинается с одной из приставок. Пустая приставка
// совпадает со всем: так записано правило, чей отбор идёт не по пути.
func hasAnyPrefix(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if p == "" || strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// onlyMentionedInComments — все строки со словом являются комментарием.
//
// Признак судит СТРОКУ, а не файл: проверка по файлу целиком отвечала бы «да» на
// любом исходнике, где рядом с машинерией стоит объяснение.
func onlyMentionedInComments(content string) bool {
	total, commented := 0, 0
	for _, line := range strings.Split(content, "\n") {
		if !quotaCensusMark.MatchString(line) {
			continue
		}
		total++
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") ||
			strings.HasPrefix(t, "--") || strings.HasPrefix(t, "*") {
			commented++
		}
	}
	return total > 0 && total == commented
}

// classifyQuotaCandidate — отнесение одного кандидата. Возвращает поверхности и
// коды сработавших правил.
func classifyQuotaCandidate(rel, content string) (surfaces, rules []string) {
	seen := map[string]bool{}
	add := func(s, code string) {
		if !seen[s] {
			seen[s] = true
			surfaces = append(surfaces, s)
		}
		rules = append(rules, code)
	}

	for _, r := range quotaCensusRules {
		if r.Fallback {
			continue
		}
		if !quotaCensusRuleMatches(r, rel, content) {
			continue
		}
		add(r.Surface, r.Code)
	}
	if len(surfaces) > 0 {
		sort.Strings(surfaces)
		return surfaces, rules
	}

	// Отступление применяется только к неотнесённому.
	for _, r := range quotaCensusRules {
		if !r.Fallback {
			continue
		}
		switch r.Code {
		case "P3":
			if onlyMentionedInComments(content) {
				add(r.Surface, r.Code)
			}
		default:
			if quotaCensusRuleMatches(r, rel, content) {
				add(r.Surface, r.Code)
			}
		}
		if len(surfaces) > 0 {
			break
		}
	}
	sort.Strings(surfaces)
	return surfaces, rules
}

// quotaCensusRuleMatches — совпало ли правило.
func quotaCensusRuleMatches(r quotaCensusRule, rel, content string) bool {
	if len(r.NotScope) > 0 && hasAnyPrefix(rel, r.NotScope) {
		return false
	}
	if len(r.Scope) > 0 && !hasAnyPrefix(rel, r.Scope) {
		return false
	}
	if len(r.Prefixes) > 0 {
		if !hasAnyPrefix(rel, r.Prefixes) {
			return false
		}
		if len(r.NotPrefixes) > 0 && hasAnyPrefix(rel, r.NotPrefixes) {
			return false
		}
		if r.Token == nil {
			return true
		}
	}
	if r.Token == nil {
		return false
	}
	return r.Token.MatchString(content)
}

// quotaCensusEligible — файл участвует в обходе.
func quotaCensusEligible(rel string) bool {
	if strings.HasPrefix(rel, quotaCensusExcludedPrefix) {
		return false
	}
	if strings.HasSuffix(rel, "_test.go") {
		return false
	}
	ext := filepath.Ext(rel)
	for _, e := range quotaCensusExtensions {
		if ext == e {
			return true
		}
	}
	return false
}

// quotaCensusAccumulator — счётчик переписи.
//
// Вынесен отдельно от обхода намеренно: доказательство способности гейта упасть
// обязано подавать ему НАСТОЯЩИЕ формы из дерева, не поднимая дерева. Обход и
// счёт — разные предметы, и смешение их сделало бы инъекцию невозможной иначе
// как через синтетический репозиторий.
type quotaCensusAccumulator struct {
	res      *quotaCensusResult
	hitRules map[string]bool
}

func newQuotaCensusAccumulator() *quotaCensusAccumulator {
	return &quotaCensusAccumulator{
		res: &quotaCensusResult{
			PerSurface: map[string]int{},
			PerPrimary: map[string]int{},
		},
		hitRules: map[string]bool{},
	}
}

// Observe — один файл дерева. Неподходящее расширение и сгенерированные стабы
// в осмотренное не идут: единица счёта названа у признака задачи, и перепись
// держит её, чтобы числа были сравнимы.
func (a *quotaCensusAccumulator) Observe(rel, content string) {
	if rel == quotaCensusOwnSource {
		a.res.OwnSourceSeen = true
		return
	}
	if !quotaCensusEligible(rel) {
		return
	}
	a.res.FilesWalked++
	if !quotaCensusMark.MatchString(content) {
		return
	}

	surfaces, rules := classifyQuotaCandidate(rel, content)
	for _, code := range rules {
		a.hitRules[code] = true
	}
	cand := quotaCensusFile{
		Path:     rel,
		Surfaces: surfaces,
		Rules:    rules,
		TaskMark: quotaTaskMark.MatchString(content),
	}
	if cand.TaskMark {
		a.res.TaskCandidates++
	}
	if len(surfaces) == 0 {
		a.res.Unclassified = append(a.res.Unclassified, rel)
	} else {
		cand.Primary = quotaPrimarySurface(surfaces)
		a.res.PerPrimary[cand.Primary]++
		for _, s := range surfaces {
			a.res.PerSurface[s]++
		}
	}
	a.res.Candidates = append(a.res.Candidates, cand)
}

// Finish — итог. Заодно называет правила, которым нечего относить.
func (a *quotaCensusAccumulator) Finish() *quotaCensusResult {
	for _, r := range quotaCensusRules {
		if !a.hitRules[r.Code] {
			a.res.RulesWithoutHit = append(a.res.RulesWithoutHit, r.Code+" («"+r.Why+"»)")
		}
	}
	return a.res
}

// runQuotaSurfaceCensus — обход дерева и отнесение.
//
// Состав дерева берётся у индекса git, а не обходом диска: иначе вердикт стал бы
// свойством рабочего каталога (рабочие копии полос, распаковки чартов, отчёты
// прогонов), а не коммита.
func runQuotaSurfaceCensus(root string) (*quotaCensusResult, error) {
	files, err := treecorpus.Under(root)
	if err != nil {
		return nil, err
	}
	acc := newQuotaCensusAccumulator()
	for _, abs := range files {
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			return nil, fmt.Errorf("относительный путь для %s: %w", abs, rerr)
		}
		rel = filepath.ToSlash(rel)
		if !quotaCensusEligible(rel) {
			continue
		}
		raw, ferr := os.ReadFile(abs) // #nosec G304 -- путь из индекса git собственного дерева
		if ferr != nil {
			return nil, fmt.Errorf("чтение %s: %w", rel, ferr)
		}
		acc.Observe(rel, string(raw))
	}
	return acc.Finish(), nil
}

// quotaPrimarySurface — главная поверхность файла по объявленному порядку.
func quotaPrimarySurface(surfaces []string) string {
	for _, want := range quotaCensusOrder {
		for _, s := range surfaces {
			if s == want {
				return want
			}
		}
	}
	return surfaces[0]
}
