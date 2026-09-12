// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// quotasurfacecensus_injection_test.go — доказательство того, что перепись
// СПОСОБНА упасть и способна смолчать.
//
// Каждая ось подаётся НАСТОЯЩЕЙ формой из дерева и сопровождается законным
// близнецом: без близнеца «молчит» неотличимо от «не различает ничего».
//
// Отдельная ось — ОДНОФАКТНОСТЬ границы B и C: миры отличаются ровно приставкой
// пути, содержимое побайтово одно. Иначе «граница проходит по месту» осталась бы
// заявлением документа, а не свойством кода.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// surfacesOf — короткая форма для проб.
func surfacesOf(rel, content string) []string {
	s, _ := classifyQuotaCandidate(rel, content)
	return s
}

// --- ось 1: каждая поверхность узнаётся настоящей формой из дерева ----------

func TestQSC_AuthorityIsRecognisedByTheServiceName(t *testing.T) {
	t.Parallel()
	// Форма из `services/compute/internal/clients/limit_client.go`.
	got := surfacesOf("services/compute/internal/clients/limit_client.go",
		"// клиент iam.v1.InternalLimitService на внутреннем слушателе\n")
	require.Contains(t, got, quotaSurfaceAuthority,
		"имя службы величин обязано относить файл к авторитету: именно эти места снимает S4")
}

func TestQSC_AuthorityIsRecognisedByTheCatalogueVocabulary(t *testing.T) {
	t.Parallel()
	// Форма из `services/iam/internal/domain/limit.go` — файла, которого признак
	// задачи #2135 НЕ видит вовсе.
	got := surfacesOf("services/iam/internal/domain/limit.go",
		"var countableKinds = []CountableKind{\n\t{\"iam.account\", CarrierIdentity},\n}\n")
	require.Contains(t, got, quotaSurfaceAuthority,
		"каталог видов — предмет решения Д8 и пунктов 13–15 условия готовности S4")
}

func TestQSC_OwnerLedgerIsRecognisedByItsTrigger(t *testing.T) {
	t.Parallel()
	// Форма из миграций владельцев.
	got := surfacesOf("services/vpc/internal/migrations/0031_quota.sql",
		"CREATE TRIGGER t AFTER INSERT ON networks FOR EACH ROW EXECUTE FUNCTION kacho_quota_count('vpc.network');\n")
	require.Contains(t, got, quotaSurfaceOwners)
	require.NotContains(t, got, quotaSurfaceIAMLedger,
		"учёт у владельца не есть учёт службы доступа: их различает место")
}

func TestQSC_AdmissionRateIsItsOwnSurface(t *testing.T) {
	t.Parallel()
	// Форма из `0001_initial.sql`.
	got := surfacesOf("services/iam/internal/migrations/0002_x.sql",
		"CREATE TABLE kaname.account_admission_rate_limits (id text PRIMARY KEY);\n")
	require.Contains(t, got, quotaSurfaceAdmission,
		"предел скорости приёма — не потолок количества и из службы доступа не уходит")
}

func TestQSC_ForeignSubjectIsNotOurLedger(t *testing.T) {
	t.Parallel()
	// Форма из `gateway/internal/subscriptionstream/handler.go`.
	got := surfacesOf("gateway/internal/subscriptionstream/handler.go",
		"// RefusedSubjectQuota — субъект исчерпал СВОЙ предел, а не предел реплики.\n\tRefusedSubjectQuota uint64\n")
	require.Contains(t, got, quotaSurfaceForeign,
		"предел подписчика потока — чужой предмет под тем же словом")
	require.NotContains(t, got, quotaSurfaceOwners,
		"отнеся его к учёту, перепись отдала бы его под снос вместе с квотами")
}

func TestQSC_ProseMentionIsNotMachinery(t *testing.T) {
	t.Parallel()
	// Форма из `services/vpc/.../serviceerr/reservedcidr.go`.
	got := surfacesOf("services/vpc/internal/apps/kacho/shared/serviceerr/reservedcidr.go",
		"// Тот же выбор уже сделан для отказа учёта (`quota.go`), и повторять его\n// здесь незачем.\n")
	require.Equal(t, []string{quotaSurfaceProse}, got,
		"упоминание только в комментарии машинерией не является")
}

// --- ось 2: неизвестная форма — НАХОДКА, а не молчание --------------------

func TestQSC_UnknownFormIsAFinding(t *testing.T) {
	t.Parallel()
	got := surfacesOf("services/vpc/internal/thing.go",
		"const x = quotaFlibbertigibbet // форма, которой перепись не знает\n")
	require.Empty(t, got,
		"кандидат, которого не относит ни одно правило, обязан остаться НЕОТНЕСЁННЫМ: "+
			"именно это роняет гейт и заставляет человека назвать поверхность")
}

// TestQSC_UnknownFormReachesTheResultAsAFinding — та же ось, но через счётчик:
// гейт судит по res.Unclassified, а не по возвращаемому срезу.
func TestQSC_UnknownFormReachesTheResultAsAFinding(t *testing.T) {
	t.Parallel()
	acc := newQuotaCensusAccumulator()
	acc.Observe("services/vpc/internal/thing.go", "const x = quotaFlibbertigibbet\n")
	res := acc.Finish()
	require.Equal(t, []string{"services/vpc/internal/thing.go"}, res.Unclassified)
	require.Equal(t, 1, res.FilesWalked)
}

// TestQSC_KnownFormLeavesNoFinding — законный близнец предыдущей.
func TestQSC_KnownFormLeavesNoFinding(t *testing.T) {
	t.Parallel()
	acc := newQuotaCensusAccumulator()
	acc.Observe("services/vpc/internal/thing.go", "err := guard.QuotaGuard(ctx)\n")
	res := acc.Finish()
	require.Empty(t, res.Unclassified,
		"известная форма находкой быть не должна — иначе гейт краснеет на исправном дереве")
	require.Equal(t, 1, res.PerSurface[quotaSurfaceOwners])
}

// --- ось 3: правило без предмета — находка --------------------------------

func TestQSC_RuleWithoutSubjectIsAFinding(t *testing.T) {
	t.Parallel()
	acc := newQuotaCensusAccumulator()
	acc.Observe("services/vpc/internal/thing.go", "err := guard.QuotaGuard(ctx)\n")
	res := acc.Finish()

	require.NotEmpty(t, res.RulesWithoutHit,
		"на корпусе из одного файла почти всем правилам относить нечего — "+
			"перепись обязана это сказать, иначе послабление переживёт свой предмет")
	joined := strings.Join(res.RulesWithoutHit, " ")
	require.Contains(t, joined, "A1", "находка обязана НАЗЫВАТЬ правило, а не только их число")
	require.NotContains(t, joined, "B7 (",
		"правило, которому было что относить, в находки попадать не должно")
}

// --- ось 4 СНЯТА ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ -------------------------------
//
// Здесь стояла проба «граница B и C — ОДНОФАКТНАЯ разница пути»: два файла с
// побайтово одним содержимым, различающиеся только каталогом, обязаны попадать
// на разные поверхности. Единственным правилом поверхности C, сужённым по пути,
// было C1 («тот же учёт, но в службе доступа»); оно снято вместе с областью
// `services/iam`, которой в дереве больше нет.
//
// Отличать поверхности ПО ПУТИ стало нечем — и это не ослабление: правил
// поверхности C осталось три (C2 · C3 · C4), и все три ключуются на ПРИЗНАКЕ
// (глагол учёта личности, контракт чтения, пакет формы ответа у края), а не на
// каталоге. Проба, оставленная без своего правила, утверждала бы различение,
// которого распознаватель не производит — то есть зеленела бы вакуумно либо
// краснела на верном дереве.
//
// Ось вернётся вместе с правилом, сужённым по пути, если такое заведут снова.

// --- ось 5: слепая зона признака ЗАДАЧИ названа числом, а не унаследована ---

func TestQSC_TaskMarkIsBlindToTheCatalogueFile(t *testing.T) {
	t.Parallel()
	// Дословная форма из `services/iam/internal/domain/limit.go`: строчного
	// `quota` и `Quota` там нет ни одного.
	const catalogue = "var countableKinds = []CountableKind{}\n// QUOTA_NOT_PROVISIONED\n"

	require.True(t, quotaCensusMark.MatchString(catalogue),
		"признак переписи обязан видеть файл каталога — вокруг него стоят пункты 13–15 DoD S4")
	require.False(t, quotaTaskMark.MatchString(catalogue),
		"признак задачи #2135 этого файла не видит; перепись, унаследовавшая его признак, "+
			"молча потеряла бы предмет работы")
}

// --- ось 6: единица счёта — та же, что у задачи ----------------------------

func TestQSC_EligibilityHoldsTheTaskUnit(t *testing.T) {
	t.Parallel()
	require.False(t, quotaCensusEligible("services/vpc/internal/x_test.go"),
		"проба Go в единицу счёта задачи не входит")
	require.False(t, quotaCensusEligible("pkg/api/kacho/cloud/iam/v1/limit.pb.go"),
		"сгенерированное дерево стабов правится генерацией, а не руками")
	require.False(t, quotaCensusEligible("deploy/helm/x/Chart.lock"),
		"расширение вне объявленного набора в осмотренное не идёт")
	require.True(t, quotaCensusEligible("services/vpc/internal/x.go"),
		"законный близнец: обычный исходник участвует")
	require.True(t, quotaCensusEligible("ui-future/shared/src/x.test.tsx"),
		"проба TypeScript ИЗ единицы задачи не исключена — исключены только пробы Go")
}

// --- ось 7: главная поверхность выбирается объявленным порядком ------------

func TestQSC_PrimaryPrefersForeignOverOurLedger(t *testing.T) {
	t.Parallel()
	require.Equal(t, quotaSurfaceForeign,
		quotaPrimarySurface([]string{quotaSurfaceForeign, quotaSurfaceOwners}),
		"если слово здесь означает не нашу квоту, ни одна наша поверхность его не касается")
	require.Equal(t, quotaSurfaceAuthority,
		quotaPrimarySurface([]string{quotaSurfaceAuthority, quotaSurfaceOwners}),
		"файл, называющий и авторитет, и учёт, разбирается со стороны того, что СНИМАЕТСЯ")
}

// --- ось 8: перепись не судит собственное описание -------------------------

// TestQSC_OwnSourceIsNotItsOwnSubject — исходник переписи несёт признаки ВСЕХ
// поверхностей: без них он не мог бы их различать. Судя его наравне с предметом,
// перепись прибавляла бы к каждой поверхности по единице собственного описания.
func TestQSC_OwnSourceIsNotItsOwnSubject(t *testing.T) {
	t.Parallel()
	const body = "InternalLimitService kacho_quota_count RefusedSubjectQuota\n"

	acc := newQuotaCensusAccumulator()
	acc.Observe(quotaCensusOwnSource, body)
	res := acc.Finish()

	require.True(t, res.OwnSourceSeen, "исходник переписи обязан быть замечен, а не пропущен молча")
	require.Empty(t, res.Candidates, "своё описание кандидатом не является")
	require.Zero(t, res.FilesWalked, "и в осмотренное оно тоже не идёт")
}

// TestQSC_ANeighbouringGateIsStillJudged — законный близнец предыдущей.
//
// Исключён именно ИСХОДНИК ПЕРЕПИСИ, а не каталог гейтов: соседний гейт держит
// свойство учёта по-настоящему, и снятие таких гейтов — часть работы S4.
func TestQSC_ANeighbouringGateIsStillJudged(t *testing.T) {
	t.Parallel()
	const body = "InternalLimitService kacho_quota_count RefusedSubjectQuota\n"

	acc := newQuotaCensusAccumulator()
	acc.Observe("internal/repohygiene/quotakindproducer.go", body)
	res := acc.Finish()

	require.Len(t, res.Candidates, 1,
		"соседний гейт из осмотра не выпадает: спрятав каталог целиком, перепись "+
			"потеряла бы из виду гейты, которые S4 обязана снять вместе с предметом")
	require.False(t, res.OwnSourceSeen)
}

// --- ось 5: расширения, добавленные задачей #2135 ---------------------------
//
// Каждая ось подаётся НАСТОЯЩЕЙ формой из дерева и несёт законного близнеца:
// без близнеца «расширение участвует в обходе» неотличимо от «обход берёт всё».

func TestQSC_AuthzModelFileIsWalkedAndCarriesTheAuthority(t *testing.T) {
	t.Parallel()
	const rel = "proto/kaname/cloud/iam/v1/fga_model.fga"
	require.True(t, quotaCensusEligible(rel),
		"модель прав обязана участвовать в обходе: в ней ОБЪЯВЛЕНО отношение "+
			"чтения величин, и до расширения списка перепись его не видела вовсе")
	// Форма из самого файла.
	got := surfacesOf(rel, "    define quota_reader: [service_account, group#member] or system_admin\n")
	require.Contains(t, got, quotaSurfaceAuthority,
		"объявление права чтения величин — остаток поверхности «A»: оно уходит вместе с авторитетом")
}

func TestQSC_AuthzModelWithoutTheRelationIsNotACandidate(t *testing.T) {
	t.Parallel()
	// Законный близнец: та же модель, соседнее отношение. Кандидатом не является.
	acc := newQuotaCensusAccumulator()
	acc.Observe("proto/kaname/cloud/iam/v1/fga_model.fga",
		"    define fga_writer: [service_account] or system_admin\n")
	res := acc.Finish()
	require.Equal(t, 1, res.FilesWalked, "файл обязан быть ОСМОТРЕН")
	require.Empty(t, res.Candidates,
		"осмотренный файл без предмета кандидатом не становится — иначе расширение "+
			"списка означало бы «берём всё» и числа поверхностей потеряли бы смысл")
}

func TestQSC_ForeignSubjectIsRecognisedByItsKnobName(t *testing.T) {
	t.Parallel()
	// Форма из `gateway/deploy/values.yaml`: ИМЯ РУЧКИ предела подписчика.
	got := surfacesOf("gateway/deploy/values.yaml",
		"  # вызывающему, у которого своя квота (`maxStreamsPerSubject` ниже)\n")
	require.Contains(t, got, quotaSurfaceForeign,
		"предел подписчика потока — чужой предмет; по имени ручки он обязан узнаваться так же, "+
			"как по имени счётчика отказов")
	require.NotContains(t, got, quotaSurfaceProse,
		"чужая машинерия не есть наша проза: отнеся её к упоминанию, перепись сказала бы, "+
			"что машинерии здесь нет")
}

// --- ось 6: русская половина признака --------------------------------------

func TestQSC_RussianOnlyMentionIsSeenAndCounted(t *testing.T) {
	t.Parallel()
	// Форма из `services/vpc/internal/clients/iam_client.go`: латинского
	// написания предмета в файле нет ни одного.
	acc := newQuotaCensusAccumulator()
	acc.Observe("services/vpc/internal/clients/iam_client.go",
		"// невидима аккаунтной дельте (приёмка квот, V2-4).\n")
	res := acc.Finish()
	require.Len(t, res.Candidates, 1,
		"корпус двуязычен: признак на одном языке терял бы такие места МОЛЧА")
	require.Equal(t, 1, res.RussianOnly,
		"размер слепой зоны обязан быть НАЗВАН числом, иначе расширение признака "+
			"неотличимо от холостого")
}

func TestQSC_LatinMentionDoesNotInflateTheRussianOnlyCount(t *testing.T) {
	t.Parallel()
	// Законный близнец: тот же предмет, названный латиницей. Кандидат — да,
	// в слепую зону — нет.
	acc := newQuotaCensusAccumulator()
	acc.Observe("services/vpc/internal/clients/limit_client.go",
		"// клиент InternalLimitService на внутреннем слушателе\n")
	res := acc.Finish()
	require.Len(t, res.Candidates, 1)
	require.Zero(t, res.RussianOnly,
		"счётчик слепой зоны обязан считать ЯЗЫК, а не регистр и не словарь каталога видов")
}

// --- ось 7: пункт навигации в раздел величин --------------------------------

func TestQSC_ConsoleMenuEntryToTheLimitsSectionIsAuthority(t *testing.T) {
	t.Parallel()
	// Форма из `ui-future/system/src/navigation.ts`: пункт несёт АДРЕС раздела,
	// а имени компонента страницы не называет.
	got := surfacesOf("ui-future/system/src/navigation.ts",
		"        key: \"system-limits\",\n        path: \"/system/limits\",\n")
	require.Contains(t, got, quotaSurfaceAuthority,
		"пункт меню — машинерия: снятие авторитета без него оставит в консоли "+
			"ссылку на снятую страницу, и увидит это арендатор, а не перепись")
}

func TestQSC_ANeighbouringConsoleMenuEntryIsNotAuthority(t *testing.T) {
	t.Parallel()
	// Законный близнец: соседний пункт того же перечня.
	acc := newQuotaCensusAccumulator()
	acc.Observe("ui-future/system/src/navigation.ts",
		"        key: \"system-tokens\",\n        path: \"/system/tokens\",\n")
	res := acc.Finish()
	require.Empty(t, res.Candidates,
		"соседний раздел консоли к величинам отношения не имеет: правило обязано "+
			"судить адрес, а не близость строк")
}

// --- ось 8: проза страницы -------------------------------------------------

func TestQSC_PageWithProseOnlyIsAMention(t *testing.T) {
	t.Parallel()
	// Форма из `services/iam/docs/content/api/project.mdx`: страница называет
	// предмет одной прозой, машинерии не несёт.
	got := surfacesOf("services/iam/docs/content/api/project.mdx",
		"задаёт границу для квот и (главное) — **уровень выдачи прав**\n")
	require.Equal(t, []string{quotaSurfaceProse}, got,
		"страница, называющая предмет прозой, не уезжает — она становится ЛОЖЬЮ, "+
			"и это другой род работы")
}

func TestQSC_PageNamingMachineryKeepsItsOwnSurface(t *testing.T) {
	t.Parallel()
	// Законный близнец: та же форма файла, но страница называет машинерию.
	// Правило прозы — отступление, поэтому оно обязано уступить.
	got := surfacesOf("services/iam/docs/content/api/limit.mdx",
		"`InternalLimitService.Resolve` отдаёт действующие величины\n")
	require.Contains(t, got, quotaSurfaceAuthority)
	require.NotContains(t, got, quotaSurfaceProse,
		"отступление, взявшее страницу с машинерией, спрятало бы работу стадии S4 в «упоминание»")
}

func TestQSC_ProseRuleDoesNotSwallowUnknownMachinery(t *testing.T) {
	t.Parallel()
	// АНТИМАСКА: правило прозы отбирает по окончанию имени, поэтому исходник с
	// неизвестной формой машинерии остаётся находкой. Без этой пробы правило
	// прозы было бы корзиной «прочее» под другим именем.
	acc := newQuotaCensusAccumulator()
	acc.Observe("services/vpc/internal/thing.go", "const x = quotaFlibbertigibbet\n")
	res := acc.Finish()
	require.Equal(t, []string{"services/vpc/internal/thing.go"}, res.Unclassified,
		"неизвестная форма в ИСХОДНИКЕ обязана оставаться неотнесённой")
}

// --- ось 9: совпадение подстроки не прячет настоящую машинерию ------------

func TestQSC_SubstringAccidentDoesNotHideRealMachinery(t *testing.T) {
	t.Parallel()
	// Форма из `services/vpc/docs/engineering/architecture/11-resource-count-quotas.md`:
	// слово, в которое признак попал подстрокой, стоит рядом с настоящим именем
	// службы величин.
	got := surfacesOf("services/vpc/docs/engineering/architecture/11-resource-count-quotas.md",
		"a quotation from the owner doc\n`InternalLimitService` отдаёт действующие величины\n")
	require.Contains(t, got, quotaSurfaceAuthority,
		"настоящая машинерия обязана пережить случайное совпадение подстроки")
	require.NotContains(t, got, quotaSurfaceForeign,
		"иначе файл уходит под вердикт «не предмет», а работа в нём как раз нужна")
}

func TestQSC_SubstringAccidentAloneStaysForeign(t *testing.T) {
	t.Parallel()
	// Законный близнец: то же совпадение БЕЗ машинерии. Объяснять больше нечего,
	// и отступление обязано сработать — иначе файл станет находкой на пустом месте.
	got := surfacesOf("services/vpc/docs/engineering/architecture/README.md",
		"a quotation from the owner doc\n")
	require.Equal(t, []string{quotaSurfaceForeign}, got,
		"совпадение подстроки без машинерии — по-прежнему «не предмет»")
}

func TestQSC_ForeignStorageRefusalKeepsItsSurface(t *testing.T) {
	t.Parallel()
	// Отказ по ЁМКОСТИ чужой системы хранения. Общая фраза отказа по счёту
	// является его подстрокой, поэтому файл числится и в учёте владельцев, — но
	// чужая машинерия названа явно и в разбиении обязана победить.
	acc := newQuotaCensusAccumulator()
	acc.Observe("services/storage/internal/repo/pg/volume_repo.go",
		"return fmt.Errorf(\"storage quota exceeded\")\n")
	res := acc.Finish()
	require.Equal(t, 1, res.PerPrimary[quotaSurfaceForeign],
		"ёмкость в байтах квотой счёта ресурсов не является: отдав её учёту, "+
			"перепись назначила бы работу там, где её нет")
}

// --- ось: описание процесса, зовущее самопроверку (правило P4) --------------
//
// Три входа вместо одного, потому что предмет правила — ИСПОЛНЯЕМЫЙ ВЫЗОВ в
// описании процесса, и каждое из трёх слов в этой фразе надо опровергнуть
// порознь. Форма входов взята у настоящего шага `selftest-quota-posture` в
// `.github/workflows/console-e2e.yml`.

func TestQSC_WorkflowCallingASelftestIsAMention(t *testing.T) {
	t.Parallel()
	got := surfacesOf(".github/workflows/console-e2e.yml",
		"      - name: гейт — самопроверка решения о посадке домена величин\n"+
			"        id: selftest-quota-posture\n"+
			"        working-directory: ui-future/e2e\n"+
			"        run: node scripts/quota-posture-selftest.ts\n")
	require.Equal(t, []string{quotaSurfaceProse}, got,
		"описание процесса называет ИМЯ ФАЙЛА пробы, а не величину: машинерии в нём нет, "+
			"и снос авторитета величин его не затронет")
}

func TestQSC_WorkflowWithRealMachineryStaysAFinding(t *testing.T) {
	t.Parallel()
	// Законный близнец правила P4: тот же каталог, отличается РОВНО одним
	// фактом — вместо вызова пробы стоит машинерия величин. Без него P4
	// доказывало бы лишь то, что описания процессов оно относит, — а не то, что
	// относит ровно вызов пробы.
	got := surfacesOf(".github/workflows/console-e2e.yml",
		"        env:\n          KACHO_VPC_QUOTA_NETWORKS: \"12\"\n")
	require.Empty(t, got,
		"описание процесса, выставляющее ВЕЛИЧИНУ, обязано остаться НЕОТНЕСЁННЫМ: "+
			"иначе P4 становится корзиной «прочее» для всего каталога описаний процессов")
}

func TestQSC_SelftestCallOutsideTheWorkflowDirIsNotAMentionByP4(t *testing.T) {
	t.Parallel()
	// Та же строка вызова, но не в описании процесса: область правила несущая,
	// иначе любой скрипт, зовущий пробу, уехал бы в «упоминание».
	got := surfacesOf("scripts/local/run-console-selftests.sh",
		"node scripts/quota-posture-selftest.ts\n")
	require.NotContains(t, got, quotaSurfaceProse,
		"P4 сработало вне .github/workflows — область правила потеряна")
}
