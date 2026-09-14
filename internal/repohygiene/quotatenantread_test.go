// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/treecorpus"

	"github.com/PRO-Robotech/corelib/contractroot"
	"github.com/PRO-Robotech/corelib/platformmodules"

	"github.com/PRO-Robotech/kacho/internal/contractsource"
)

// quotaContractFile — одно описание контракта: координата в форме
// `proto/<корень>/…` и путь, по которому его читать.
type quotaContractFile struct {
	// Rel — координата, какой её называет читатель и печатает находка. Она НЕ
	// зависит от того, на каком диске файл нашёлся.
	Rel string
	// Abs — где его открыть.
	Abs string
}

// quotaContractFiles — описания контрактов ВСЕХ объявленных корней дерева.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПО КОРНЯМ, А НЕ ОБХОДОМ `proto/` ЦЕЛИКОМ
//
// Прежняя редакция брала `treecorpus.UnderWithSuffix(root+"/proto", ".proto")` —
// один обход одного каталога, потому что все контракты лежали под ним. Решением
// владельца kacho#2616 (исход C, 2026-09-13) контракты службы доступа уехали в
// её репозиторий и приезжают модулем `github.com/PRO-Robotech/kaname`:
// утверждение «все контракты лежат под `proto/` этого дерева» ОТМЕНЕНО, и
// координату каждого корня резолвит `internal/contractsource`.
//
// ЦЕНА НЕПЕРЕВОДА БЫЛА НАЗВАНА ЧИСЛОМ, а не предположена: `InternalLimitService`
// объявлен в дереве РОВНО ОДИН раз, и объявление это лежит теперь в модуле.
// Обход одного каталога находил владельцев величин НОЛЬ — и предпосылка
// «владелец ровно один», на которой стоит освобождение владельца от догоняющего
// снимка, роняла прогон, хотя владелец существует и пять доменов этого дерева
// списывают против него.
//
// ЕДИНИЦА СЧЁТА СМЕНИЛАСЬ, и это сказано прямо, потому что перепись печатает
// число: было «все `.proto` под `proto/`» — 87 файлов, включая 4 вендоренных
// `google/**`; стало «`.proto` под ОБЪЯВЛЕННЫМИ корнями» (`contractroot.Roots`
// — kacho · kaname · corelib) — 81 + 41 + 2 = 124. Вендоренное третьей стороны
// выпало намеренно: `service QuotaService` там объявить нельзя, а его сегменты
// пути к форме `proto/<корень>/cloud/<каталог>/v1` не приводятся, то есть в
// `answering` он попадал бы мусорным ключом.
//
// ПУСТОЙ СОСТАВ — ОТКАЗ, а не пустой срез: его даёт сам `contractsource.Files`,
// и вызывающему не приходится отличать «файлов нет» от «прочитано не то дерево».
func quotaContractFiles(t *testing.T, root string) []quotaContractFile {
	t.Helper()
	var out []quotaContractFile
	for _, r := range contractroot.Roots {
		dir, err := contractsource.Dir(root, r)
		require.NoErrorf(t, err, "каталог дерева контрактов корня %q", r)
		files, err := contractsource.Files(root, r, ".proto")
		require.NoErrorf(t, err, "состав дерева контрактов корня %q", r)

		// Каталог `proto/` того дерева, в котором корень нашёлся: у корня этого
		// репозитория свой, у приехавшего модулем — модульный.
		base := filepath.Dir(dir)
		for _, abs := range files {
			rel, rerr := filepath.Rel(base, abs)
			require.NoErrorf(t, rerr, "путь %s относительно %s", abs, base)
			out = append(out, quotaContractFile{
				Rel: "proto/" + filepath.ToSlash(rel),
				Abs: abs,
			})
		}
	}
	require.NotEmptyf(t, out, "ни одного описания контракта под корнями %v — "+
		"обход пуст, и всякий вердикт по нему был бы свойством непрочитанного",
		contractroot.Roots)
	return out
}

// Владелец, который СПИСЫВАЕТ, обязан отвечать на ЧТЕНИЕ — и его снимок обязан
// догонять авторитет.
//
// ПРЕДМЕТ (задача `PRO-Robotech/kacho#412`). Предел, который ограничивает и
// которого не видно, для арендатора неотличим от сбоя платформы: он узнаёт о
// квоте только когда упирается в неё отказом. На день заведения гейта списывали
// пять доменов, отвечал на чтение один.
//
// ПОЧЕМУ ДВА УСЛОВИЯ, А НЕ ОДНО. Чтение без синхронизатора хуже отсутствия
// чтения: строка снимка заводится материализацией ОДИН раз и дальше живёт со
// своей величиной вечно, потому что промаха больше не случается, а запись идёт
// `ON CONFLICT DO NOTHING`. Показать такую строку арендатору значит громко
// назвать величину, которая никогда не догонит ту, что назначил администратор.
// Поэтому «отвечает на чтение» здесь означает пару: поверхность И курсор дельты.
//
// ПОЧЕМУ СВЕРКА ИДЁТ В ОБЕ СТОРОНЫ. Домен, который отвечает на чтение и не
// списывает, — тоже находка, и она тише: его ответ описывает потолок, под
// который ничего не считается, то есть число, которое не наполнится никогда.
func TestEveryQuotaChargingOwnerAnswersTheTenantRead(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)

	// Соответствие здесь ОДНО — «служба ↔ модуль каталога»: у домена `nlb`
	// каталог контракта называется `loadbalancer`, так его назвал сам контракт,
	// и переименовать его нельзя (сломалась бы форма на проводе). Оно ОБЩЕЕ для
	// платформы и берётся у словаря имён модулей (pkg/platformmodules).
	//
	// ЗДЕСЬ СТОЯЛО ВТОРОЕ, ЧАСТНОЕ: `protoDirOf["iam"] = "quota"`. Оно было верно,
	// пока служба чтения величин `iam` жила в пакете ОБЩЕЙ формы ответа, и
	// объяснялось тем, что та форма зависит от `iam.v1`, а обратная ссылка
	// замкнула бы пакеты друг на друга.
	//
	// Довод пережил свой предмет: импортов у общей формы сегодня НЕТ вовсе, и
	// служба переехала в собственный контракт службы доступа
	// (`kaname/cloud/iam/v1`, kacho#2362, решение `Д9`). Запись снята ТЕМ ЖЕ
	// изменением, что и переезд: без этого держатель роняет прогон обеими своими
	// половинами сразу — «отвечает, но не списывает» о новом месте и «списывает,
	// но не показывает» о старом.
	//
	// Координата названа БЕЗ приставки `proto/` намеренно: с kacho#2616 (исход C,
	// 2026-09-13) этот корень лежит не под `proto/` данного дерева, а в модуле
	// `github.com/PRO-Robotech/kaname`, и «proto/kaname/…» читалось бы как путь
	// этого репозитория — то есть как приглашение пойти туда, где файла нет.
	protoDirOf := platformmodules.AliasesByService()

	sqlFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".sql")
	require.NoError(t, err, "перечень миграций берётся у индекса дерева, а не обходом диска")

	charging := map[string]string{} // домен → миграция, где найдено списание
	cursor := map[string]string{}   // домен → миграция, где заведён курсор дельты

	chargeRe := regexp.MustCompile(`kacho_quota_count\(`)
	cursorRe := regexp.MustCompile(`quota_sync_cursor`)

	migrationsSeen := 0
	for _, path := range sqlFiles {
		if !strings.Contains(path, "/internal/migrations/") {
			continue
		}
		raw, rerr := os.ReadFile(path)
		require.NoError(t, rerr, "чтение %s", path)
		migrationsSeen++
		body := string(raw)

		rel := path
		if i := strings.Index(path, "/services/"); i >= 0 {
			rel = path[i+1:]
		}
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			continue
		}
		domain := parts[1]

		if chargeRe.MatchString(body) {
			charging[domain] = rel
		}
		if cursorRe.MatchString(body) {
			cursor[domain] = rel
		}
	}

	// Кто отвечает на чтение: домен, чей контракт объявляет `QuotaService`.
	protoFiles := quotaContractFiles(t, root)

	answering := map[string]string{} // каталог контракта → файл
	// Имён у службы чтения два, и второе — не синоним: `IdentityQuotaService`
	// отвечает о носителе, который не является ни проектом, ни аккаунтом, поэтому
	// у него другая форма запроса (полей нет вовсе). Искать только первое имя
	// значило бы объявить находкой домен, который читать как раз ДАЁТ.
	serviceRe := regexp.MustCompile(`(?m)^service (Quota|IdentityQuota)Service\b`)
	protosSeen := 0
	for _, f := range protoFiles {
		raw, rerr := os.ReadFile(f.Abs) // #nosec G304 -- путь из индекса git либо из кэша модулей
		require.NoError(t, rerr, "чтение %s", f.Rel)
		protosSeen++
		if !serviceRe.MatchString(string(raw)) {
			continue
		}
		// proto/<корень>/cloud/<каталог>/v1/…
		seg := strings.Split(f.Rel, "/")
		if len(seg) > 3 {
			answering[seg[3]] = f.Rel
		}
	}

	require.NotZero(t, migrationsSeen,
		"гейт не прочитал НИ ОДНОЙ миграции — он объявил бы «ноль находок», ничего не осмотрев")
	require.NotZero(t, protosSeen,
		"гейт не прочитал НИ ОДНОГО контракта — «ноль находок» было бы «ноль прочитанного»")
	require.NotEmpty(t, charging,
		"гейт не нашёл ни одного домена со списанием: либо имя триггера сменилось, "+
			"либо предикат перестал его ловить. Осмотрено миграций: %d", migrationsSeen)
	require.NotEmpty(t, answering,
		"гейт не нашёл НИ ОДНОГО объявления `service QuotaService`: предикат перестал "+
			"ловить предмет, и молчание такого гейта неотличимо от согласия. Осмотрено контрактов: %d",
		protosSeen)
	// ВЛАДЕЛЬЦА ВЕЛИЧИН БОЛЬШЕ НЕТ, и поиск его снят вместе с предметом.
	//
	// Прежде требовалось, чтобы владелец был ровно один: освобождение от
	// догоняющего опиралось на то, что авторитет и снимок лежат в ОДНОЙ базе.
	// Авторитет ушёл из продукта целиком (kacho#2117), объявлений службы величин
	// нет ни в одном дереве, и требование «ровно один» стало недостижимым при
	// любом входе: гейт судил бы не дерево, а собственную устаревшую предпосылку.
	//
	// Ветвь освобождения снята тем же изменением — освобождать некого. Требование
	// догоняющего курсора ОСТАЁТСЯ у каждого списывающего: оно не про авторитет, а
	// про то, что снимок величины кем-то обновляется.

	protoDir := func(domain string) string {
		if d, ok := protoDirOf[domain]; ok {
			return d
		}
		return domain
	}

	var findings []string
	for domain, where := range charging {
		if _, ok := answering[protoDir(domain)]; !ok {
			findings = append(findings, domain+
				" — списывает квоту ("+where+"), но арендатору её не показывает: "+
				"нет `service QuotaService` ни в одном контракте модуля `"+protoDir(domain)+"`")
		}
		if _, ok := cursor[domain]; !ok {
			// Владельцу ВЕЛИЧИН догоняющий не нужен, и это не послабление, а
			// отсутствие предмета: авторитет лежит в той же базе, что снимок, и
			// списание обновляет снимок тем же оператором. Дельты к самому себе не
			// существует.
			//
			// ПРЕДПОСЫЛКА ОСВОБОЖДЕНИЯ ПРОВЕРЯЕТСЯ, а не принимается на слово: файл,
			// определяющий списание, обязан читать таблицу величин ЭТОГО ЖЕ домена.
			// Замени владелец живое чтение на снимок из дельты — освобождение
			// перестанет иметь основание, и гейт скажет об этом здесь.
			findings = append(findings, domain+
				" — списывает квоту ("+where+"), но снимок величины не догоняет авторитет: "+
				"нет таблицы `quota_sync_cursor` в его миграциях")
		}
	}

	// СЛУЖБЫ, ЖИВУЩИЕ В ЭТОМ ДЕРЕВЕ. Зеркальная форма ниже спрашивает «почему
	// отвечает и не списывает», и вопрос этот имеет смысл только там, где
	// списывать ЕСТЬ ЧЕМ: миграции владельца лежат в его каталоге службы.
	//
	// Контракт и реализация теперь бывают в РАЗНЫХ деревьях, и с 2026-09-13 —
	// в разных РЕПОЗИТОРИЯХ. Прежняя редакция говорила «её контракт остался
	// здесь»; утверждение ОТМЕНЕНО решением владельца kacho#2616 (исход C):
	// контракт службы доступа уехал вместе с ней и приезжает модулем
	// `github.com/PRO-Robotech/kaname`. Пользуются им по-прежнему пять доменов
	// этого дерева и край, поэтому из популяции он не изъят — его подаёт
	// `quotaContractFiles`.
	//
	// Модуль, у которого контракт есть, а каталога службы нет, списывать не может
	// BY CONSTRUCTION: его миграции лежат в чужом репозитории, и «не списывает» о
	// нём есть свойство раскладки, а не дефект.
	//
	// Признак ЖИВОЙ, а не имя: перечень выводится из индекса дерева. Появится
	// каталог службы — модуль вернётся под зеркальную меру сам, без правки
	// здесь. Пропишись имя `iam` в исключение — оно пережило бы свой предмет
	// молча в тот день, когда служба вернулась бы.
	serviceFiles, err := treecorpus.Under(filepath.Join(root, "services"))
	require.NoError(t, err, "состав каталога служб берётся у индекса дерева")
	serviceInTree := map[string]bool{}
	for _, path := range serviceFiles {
		rel := path
		if i := strings.Index(path, "/services/"); i >= 0 {
			rel = path[i+1:]
		}
		if seg := strings.Split(rel, "/"); len(seg) > 1 {
			serviceInTree[seg[1]] = true
		}
	}
	require.NotEmpty(t, serviceInTree,
		"под services/ не опознано ни одной службы — зеркальная мера ниже освободила бы "+
			"ВСЕХ, и её молчание означало бы пустой обход, а не отсутствие находок")

	// protoDirServed — модуль контракта, чья служба живёт в этом дереве.
	protoDirServed := func(dir string) bool {
		if svc, ok := platformmodules.ServiceOfCatalogModule(dir); ok {
			return serviceInTree[svc]
		}
		return serviceInTree[dir]
	}

	// Зеркальная форма: ответ про потолок, под который никто не считает.
	chargingProtoDirs := map[string]bool{}
	for domain := range charging {
		chargingProtoDirs[protoDir(domain)] = true
	}
	skippedForeign := make([]string, 0, 1)
	for dir, where := range answering {
		if !protoDirServed(dir) {
			skippedForeign = append(skippedForeign, dir)
			continue
		}
		if !chargingProtoDirs[dir] {
			findings = append(findings, dir+
				" — отвечает на чтение квот ("+where+"), но не списывает ни одного вида: "+
				"ответ называет потребление, которое не наполнится никогда")
		}
	}
	sort.Strings(findings)

	sort.Strings(skippedForeign)
	t.Logf("перепись: миграций осмотрено %d, контрактов %d; служб в дереве %d; "+
		"списывают %d (%s); отвечают на чтение %d (%s); догоняют величину %d (%s); "+
		"модулей контракта без своей службы в этом дереве %d (%s) — зеркальной мерой не судятся",
		migrationsSeen, protosSeen, len(serviceInTree),
		len(charging), joinKeys(charging),
		len(answering), joinKeys(answering),
		len(cursor), joinKeys(cursor),
		len(skippedForeign), strings.Join(skippedForeign, ", "))

	require.Empty(t, findings,
		"предел, который ограничивает и которого не видно, неотличим для арендатора от сбоя "+
			"платформы — он узнаёт о квоте только упершись в неё отказом:\n%s",
		strings.Join(findings, "\n"))
}

// localAuthorityReadByCharger — читает ли механизм списания этого домена таблицу
// величин ЕГО ЖЕ базы.
//
// Это ПРЕДПОСЫЛКА освобождения владельца величин от догоняющего, и проверяется
// она, а не принимается на слово. Освобождение защитимо ровно потому, что
// авторитет и снимок лежат в одной базе и обновляются одним оператором; замени
// живое чтение на снимок из дельты — предпосылка исчезнет, а освобождение
// осталось бы, и снимок отставал бы молча.
//
// Предмет — файл, ОПРЕДЕЛЯЮЩИЙ списание (`kacho_quota_count`), а не любой файл
// домена: величину читают многие, а вопрос ровно один — читает ли её тот, кто
// принимает решение о месте.
func localAuthorityReadByCharger(t *testing.T, root, domain string) bool {
	t.Helper()

	files, err := treecorpus.UnderWithSuffix(
		filepath.Join(root, "services", domain, "internal", "migrations"), ".sql")
	require.NoError(t, err, "перечень миграций домена %s берётся у индекса дерева", domain)

	chargeRe := regexp.MustCompile(`kacho_quota_count\(`)
	// Таблица величин у владельца ровно одна и называется `limits` в его схеме.
	// Имя схемы в предикат не зашивается: оно частность владельца, а предмет —
	// «читает СВОЮ таблицу величин».
	authorityRe := regexp.MustCompile(`\blimits\b`)

	seen := 0
	found := false
	for _, path := range files {
		raw, rerr := os.ReadFile(path)
		require.NoError(t, rerr, "чтение %s", path)
		body := string(raw)
		if !chargeRe.MatchString(body) {
			continue
		}
		seen++
		if authorityRe.MatchString(body) {
			found = true
		}
	}
	require.NotZerof(t, seen,
		"у домена %s не нашлось ни одного файла, определяющего списание, — предпосылка "+
			"этой проверки сломана, и её молчание ничего не доказывает", domain)
	return found
}
