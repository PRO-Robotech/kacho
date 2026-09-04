// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotausedwriter_test.go — репо-широкий гейт: потребление квоты двигает ТОЛЬКО
// её триггер.
//
// ЗАЧЕМ ОН СУЩЕСТВУЕТ. Потолок на число ресурсов держится не ограничением схемы,
// а предикатом `used < limit_value` в условном `UPDATE` триггера учёта. Так
// сделано намеренно: `CHECK (used <= limit_value)` запрещал бы состояние «занято
// больше, чем разрешено» и тем самым делал бы невыразимым САМО ПОНИЖЕНИЕ предела
// — штатное административное действие стало бы заложником того, кого оно
// ограничивает (измерено на прецеденте
// `services/compute/internal/migrations/0031_project_instance_limit.sql`).
//
// У этого выбора есть цена, и она названа в шапке миграции учёта: раз инвариант
// держит предикат, а не схема, то путь, который запишет `used` мимо этого
// предиката, потолок обойдёт — молча и не отказав ни разу. Гейт и есть то, чем
// цена оплачивается: он доказывает, что такого пути нет, вместо того чтобы
// полагаться на внимание следующего контрибьютора.
//
// ЧТО СЧИТАЕТСЯ НАХОДКОЙ. Любой `UPDATE` таблицы учёта, трогающий `used`, вне
// файла, определяющего триггерную функцию. Плюс `INSERT`, задающий `used`
// ненулевым литералом: материализация заводит строку с нулём, и это законно, а
// вставка сразу с потреблением — тот же обход, только с другой стороны.
//
// ЕДИНИЦА РАЗРЕШЕНИЯ — ФАЙЛ, И ЭТО РЕШЕНИЕ, А НЕ ГРУБОСТЬ РАЗБОРА. Соблазнительно
// звучит противоположное: «законный писатель стоит в теле `CREATE FUNCTION`,
// научи распознаватель это видеть — и ведомость станет не нужна». Гипотеза
// ОПРОВЕРГНУТА замером: у vpc законные записи стоят на строках 204 и 214
// `services/vpc/internal/migrations/0040_project_resource_quotas.sql`, то есть
// ВНУТРИ тела `kacho_quota_count` (167–245). Тела функций — это и есть место,
// где живёт КАЖДЫЙ писатель: и законный, и обходной, потому что сам предикат
// `used < limit_value` стоит в условном `UPDATE` внутри такого тела. Пропусти
// разбор тела функции — и гейт не найдёт писателей нигде, все записи ведомости
// станут «нечего исключать», а проверка окажется вакуумной, сохранив форму.
//
// Отсюда же ответ на вопрос, законно ли разрешать целый ФАЙЛ схемы, когда он
// сведён из всей цепочки миграций сервиса (случай iam). Законно, и держится это
// не доверием, а запретом #5: применённую миграцию не правят, поэтому в
// разрешённый файл нельзя дописать нового писателя. Новый писатель обязан
// приехать НОВОЙ миграцией — а она не в ведомости и осматривается наравне со
// всем деревом. Разрешение файла сужается его неизменяемостью.
//
// ЧЕГО ГЕЙТ НЕ ЛОВИТ, И ЭТО СКАЗАНО ЧЕСТНО. Он читает текст запросов в дереве,
// поэтому запрос, собранный из кусков в рантайме, ему не виден. Тот же предел
// есть у всякой проверки этого класса здесь; закрывается он тем, что запросы в
// этом дереве пишутся литералами — и первое же отступление от этого само станет
// находкой для читателя, а не для гейта.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// quotaUsageTables — таблицы учёта потребления. Перечень ВЫПИСАН, а не выведен:
// имя таблицы — координата, и вывести его из дерева можно только тем же поиском,
// который гейт и делает, — то есть предикат совпал бы с предметом.
//
// Запись, которой больше нечего исключать, роняет гейт (см. ниже): перечень
// самоистекающий, поэтому таблица, снятая вместе со своим механизмом, не оставит
// здесь мёртвой строки.
// `project_instance_quotas` (прецедент compute) отсюда СНЯТА вместе со своим
// предметом: таблица дропнута миграцией
// `services/compute/internal/migrations/0036_project_resource_quotas.sql`,
// списание в Go-функции репозитория удалено, механизма под этим именем в дереве
// нет. Имя ещё встречается — в применённой 0031, которую править нельзя (ban #5),
// и в снимающей её 0036, — но ни та, ни другая `used` не пишут. Watch без
// механизма был бы записью, которой нечего исключать, и его унаследовала бы
// следующая слепая зона.
var quotaUsageTables = []string{
	"project_resource_quotas",
}

// quotaTriggerDefiningFiles — файлы, которым писать `used` РАЗРЕШЕНО, потому что
// они этот механизм и определяют. Ключ — таблица, значение — путь от корня
// репозитория ЦЕЛИКОМ: сопоставление точное (`rel == allowed`), не по суффиксу.
//
// Слово «целиком» здесь несущее, и цена у него измерена. Имя `0001_initial.sql`
// носят ШЕСТЬ файлов дерева — по одному у compute, geo, iam, nlb, storage, vpc.
// Запись, укороченная до имени файла в расчёте на суффикс, разрешила бы писать
// `used` всем шестерым сразу, и расхождение было бы молчаливым: гейт остался бы
// зелёным, потеряв пять доменов из наблюдения.
//
// У `project_instance_quotas` (прецедент compute) записей БОЛЬШЕ НЕТ, и это не
// упущение: прецедент снят вместе с заведением канонического механизма
// (`services/compute/internal/migrations/0036_project_resource_quotas.sql`), его
// списание в Go-функции репозитория удалено, и писать `used` там стало некому.
// Само имя таблицы остаётся в наблюдаемых ниже — оно ещё встречается в дереве
// (применённая 0031 и снимающая её 0036), и запись сторожит его возвращение.
var quotaTriggerDefiningFiles = map[string][]string{
	"project_resource_quotas": {
		// Механизм списания живёт в ДВУХ файлах, и это не дубль: 0040 завела
		// триггерную функцию, 0041 заменила её тело через `CREATE OR REPLACE`,
		// переведя классификацию отказа на единственного производителя, общего с
		// совещательной полосой. Применённую миграцию править нельзя (ban #5),
		// поэтому у одной функции два места определения во времени — и оба обязаны
		// быть названы здесь: пропусти 0041, и гейт объявит находкой сам механизм;
		// сними 0040, и его откат (восстанавливающий прежнее тело) станет обходом.
		"services/vpc/internal/migrations/0040_project_resource_quotas.sql",
		"services/vpc/internal/migrations/0041_quota_refusal_single_producer.sql",

		// Имя таблицы у storage то же, а схема другая (`kacho_storage`), поэтому
		// гейт видит оба домена одним перечнем и ни один из них не выпадает
		// из-под наблюдения. Разойдись имена — расхождение было бы молчаливым:
		// гейт искал бы писателей по имени, которого во втором домене нет, и
		// отчитался бы «ноль находок».
		"services/storage/internal/migrations/0023_project_resource_quotas.sql",

		// iam — ЕДИНСТВЕННЫЙ владелец, который и назначает величину, и списывает
		// по ней. Его триггер заводит строку учёта сам, с потреблением ПО ФАКТУ и
		// с величиной из авторитета, лежащего в этой же базе, — поэтому запись
		// `used` здесь идёт из того же файла, что и сам механизм, а не из
		// материализации на стороне Go, которой у него нет.
		//
		// ЗДЕСЬ СТОЯЛО ДВЕ ЗАПИСИ — `484002_account_quota_identity_carrier.sql`
		// (носитель-принципал) и `20260824230000_credential_ceiling_per_principal.sql`
		// (потолок удостоверений, `PRO-Robotech/kacho#1191`, там же затравка). У
		// одной функции было два места определения во времени, потому что тело
		// заменялось через `CREATE OR REPLACE`, а применённую миграцию править
		// нельзя (ban #5).
		//
		// Предмет у обеих НЕ исчез — он ПЕРЕЕХАЛ. Цепочка миграций iam сведена в
		// одну первичную, собранную по сыгравшей базе: два места определения во
		// времени схлопнулись в одно, и механизм списания (`kacho_quota_count`,
		// `kacho_quota_carrier_lifecycle`) вместе с затравкой лежит теперь здесь.
		// Поэтому записей стало не ноль, а одна: это не снятие послабления, а
		// смена его координаты.
		"services/iam/internal/migrations/0001_initial.sql",

		// Вложенная ось vpc: та же функция списания, заменённая целиком через
		// `CREATE OR REPLACE`, — применённую миграцию править нельзя (ban #5),
		// поэтому у одной функции два места определения во времени, и оба
		// обязаны быть названы здесь.
		"services/vpc/internal/migrations/353002_nested_quota_carrier.sql",

		"services/nlb/internal/migrations/0032_project_resource_quotas.sql",
		"services/registry/internal/migrations/0015_project_resource_quotas.sql",
		"services/compute/internal/migrations/0036_project_resource_quotas.sql",

		// Пересчёт (`PRO-Robotech/kacho#419`) — ЕДИНСТВЕННЫЙ писатель `used`,
		// который не является триггером, и разрешён он по причине, которую надо
		// назвать прямо, иначе следующий читатель снимет запись как непонятную.
		//
		// Триггер умеет ровно две вещи: прибавить единицу и вычесть единицу. Он не
		// умеет ПРИВЕСТИ счётчик к факту — а именно это требуется, когда учёт
		// приходит на непустую базу и строка заводится при уже существующих
		// ресурсах. Прежде такая строка навсегда оставалась заниженной, и предел
		// переставал ограничивать.
		//
		// Почему это не обход предиката, ради которого гейт написан: пересчёт не
		// назначает `used` величину «от себя», он читает её у тех же таблиц и тем
		// же отображением «вид → таблица», которым ведёт списание, — отображение
		// выводится из самих триггеров. То есть здесь не второй источник истины, а
		// тот же самый, применённый к строке, которую триггер уже не догонит.
		//
		// Запись самоистекает штатным механизмом ниже: перестанет файл писать
		// `used` — гейт назовёт запись находкой.
		//
		// Файл один и тот же у всех пятерых — он отличается ровно именем схемы,
		// потому что отображение «вид → таблица» он не выписывает, а читает у
		// триггеров. Поэтому и записей здесь пять, а не одна: дефект был общим,
		// и лечится он у каждого владельца своей применённой миграцией.
		"services/vpc/internal/migrations/0042_quota_usage_seeded_from_rows.sql",
		"services/storage/internal/migrations/0024_quota_usage_seeded_from_rows.sql",
		"services/nlb/internal/migrations/0033_quota_usage_seeded_from_rows.sql",
		"services/registry/internal/migrations/0016_quota_usage_seeded_from_rows.sql",
		"services/compute/internal/migrations/0037_quota_usage_seeded_from_rows.sql",
	},
}

var (
	// `UPDATE <что-то> … SET … used` — перевод строки допускается, поэтому (?s).
	quotaUpdateUsedRe = regexp.MustCompile(`(?is)UPDATE\s+[a-z_.]*%s\b[^;]{0,400}?\bSET\b[^;]{0,400}?\bused\s*=`)
	// `INSERT INTO <учёт> (… used …) VALUES (… <ненулевой литерал> …)`.
	quotaInsertUsedRe = regexp.MustCompile(`(?is)INSERT\s+INTO\s+[a-z_.]*%s\b`)
)

// scanRootsForQuotaWriters — где искать. Пустой обход — провал, а не «чисто».
var scanRootsForQuotaWriters = []string{"services", "pkg", "gateway"}

// forEachQuotaScannedFile отдаёт содержимое каждого ОТСЛЕЖИВАЕМОГО файла под
// корнями обхода.
//
// Состав берётся у индекса дерева (`internal/treecorpus`), а не обходом диска.
// Разница не косметическая: под `services/` на всякой машине, где поднимали
// стенд, лежат распаковки чартов, отчёты прогонов и рабочие копии агентов —
// обход диска прочитал бы их и объявил находкой чужой файл (или, что тише,
// раздул бы перепись, сделав «осмотрено» неправдой). Гейт дерева
// `TestTreeWalkersAskTheIndex` это требование и держит.
func forEachQuotaScannedFile(t *testing.T, root string, fn func(rel string, body []byte)) {
	t.Helper()
	for _, sub := range scanRootsForQuotaWriters {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		files, err := treecorpus.UnderWithSuffix(base, ".go", ".sql")
		if err != nil {
			t.Fatalf("состав дерева под %s: %v", sub, err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("чтение %s: %v", path, readErr)
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				t.Fatalf("относительный путь %s: %v", path, relErr)
			}
			fn(filepath.ToSlash(rel), body)
		}
	}
}

// TestQuotaUsedIsWrittenOnlyByItsTrigger — потребление двигает только механизм,
// который его и определяет.
func TestQuotaUsedIsWrittenOnlyByItsTrigger(t *testing.T) {
	root := repoRoot(t)

	scannedFiles := 0
	mentionsByTable := map[string]int{}
	var findings []string

	forEachQuotaScannedFile(t, root, func(rel string, body []byte) {
		scannedFiles++
		text := string(body)
		for _, table := range quotaUsageTables {
			if !strings.Contains(text, table) {
				continue
			}
			mentionsByTable[table]++
			if quotaFileMayWriteUsed(rel, table) {
				continue
			}
			updRe := regexp.MustCompile(strings.Replace(quotaUpdateUsedRe.String(), "%s", regexp.QuoteMeta(table), 1))
			if loc := updRe.FindStringIndex(text); loc != nil {
				findings = append(findings, rel+":"+strconv.Itoa(lineOfOffset(text, loc[0]))+
					" — UPDATE "+table+" … SET used вне механизма, определяющего списание")
			}
			insRe := regexp.MustCompile(strings.Replace(quotaInsertUsedRe.String(), "%s", regexp.QuoteMeta(table), 1))
			for _, loc := range insRe.FindAllStringIndex(text, -1) {
				stmt := quotaStatementAt(text, loc[0])
				if quotaInsertSetsNonZeroUsed(stmt) {
					findings = append(findings, rel+":"+strconv.Itoa(lineOfOffset(text, loc[0]))+
						" — INSERT INTO "+table+" с ненулевым used: обход предиката с другой стороны")
				}
			}
		}
	})

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного»: сломанный обход
	// зеленеет ровно так же, как чистое дерево.
	if scannedFiles == 0 {
		t.Fatalf("гейт не прочитал ни одного файла в %v — предпосылка обхода сломана, "+
			"молчание ничего не доказывает", scanRootsForQuotaWriters)
	}

	// Самоистечение: запись перечня, которой больше нечего исключать, — находка.
	// Иначе снятый прецедент оставил бы здесь мёртвую строку, которую унаследует
	// следующая слепая зона.
	for _, table := range quotaUsageTables {
		if mentionsByTable[table] == 0 {
			t.Errorf("таблица учёта %q не встречается в дереве ни разу: механизм снят, "+
				"а запись о нём в этом гейте осталась — снимите её вместе с предметом", table)
		}
	}
	for table, files := range quotaTriggerDefiningFiles {
		for _, f := range files {
			body, err := os.ReadFile(filepath.Join(root, f))
			if err != nil {
				t.Errorf("послаблению %q (таблица %s) больше нечего исключать: файла нет — "+
					"снимите запись вместе с её предметом", f, table)
				continue
			}
			// Существование файла — ещё НЕ предмет послабления. Предмет —
			// запись `used` в нём. Файл, переставший писать, оставляет
			// послабление, которое ничего не исключает: оно молча разрешит
			// будущему автору этого файла обойти предикат потолка. Проверка
			// существования такое не ловит — ровно этот случай и возник, когда
			// списание compute переехало из Go-функции в триггер, а запись о
			// ней осталась бы висеть.
			updRe := regexp.MustCompile(strings.Replace(
				quotaUpdateUsedRe.String(), "%s", regexp.QuoteMeta(table), 1))
			if !updRe.MatchString(string(body)) {
				t.Errorf("послабление %q (таблица %s) не имеет предмета: файл существует, "+
					"но `used` в нём не пишется — снимите запись вместе с её предметом", f, table)
			}
		}
	}

	t.Logf("осмотрено файлов: %d; упоминаний таблиц учёта: %v", scannedFiles, mentionsByTable)

	if len(findings) > 0 {
		t.Fatalf("потребление квоты пишется мимо своего механизма (%d):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

func quotaFileMayWriteUsed(rel, table string) bool {
	for _, allowed := range quotaTriggerDefiningFiles[table] {
		if rel == allowed {
			return true
		}
	}
	return false
}

// quotaStatementAt возвращает текст оператора, начинающегося на offset, до `;`.
func quotaStatementAt(text string, offset int) string {
	end := strings.IndexByte(text[offset:], ';')
	if end < 0 {
		end = len(text) - offset
	}
	return text[offset : offset+end]
}

// quotaInsertSetsNonZeroUsed — вставка называет `used` и даёт ему не ноль.
//
// Ноль законен: материализация заводит строку с нулевым потреблением. Ненулевой
// литерал означает «строка появилась уже потраченной», то есть тот же обход
// предиката, только со стороны вставки.
func quotaInsertSetsNonZeroUsed(stmt string) bool {
	lower := strings.ToLower(stmt)
	if !strings.Contains(lower, "used") {
		return false
	}
	open := strings.IndexByte(lower, '(')
	if open < 0 {
		return false
	}
	closeIdx := strings.IndexByte(lower[open:], ')')
	if closeIdx < 0 {
		return false
	}
	cols := strings.Split(lower[open+1:open+closeIdx], ",")
	usedIdx := -1
	for i, c := range cols {
		if strings.TrimSpace(c) == "used" {
			usedIdx = i
			break
		}
	}
	if usedIdx < 0 {
		return false
	}
	rest := lower[open+closeIdx:]
	vOpen := strings.Index(rest, "values")
	if vOpen < 0 {
		return false
	}
	tupleStart := strings.IndexByte(rest[vOpen:], '(')
	if tupleStart < 0 {
		return false
	}
	tupleEnd := strings.IndexByte(rest[vOpen+tupleStart:], ')')
	if tupleEnd < 0 {
		return false
	}
	vals := strings.Split(rest[vOpen+tupleStart+1:vOpen+tupleStart+tupleEnd], ",")
	if usedIdx >= len(vals) {
		return false
	}
	v := strings.TrimSpace(vals[usedIdx])
	// Параметр ($1) не литерал — судить о нём статически нечем, и объявлять его
	// находкой значило бы ловить форму вместо существа.
	if strings.HasPrefix(v, "$") {
		return false
	}
	return v != "0"
}

func lineOfOffset(text string, off int) int {
	return strings.Count(text[:off], "\n") + 1
}
