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

// --- ось 4: граница B и C — ОДНОФАКТНАЯ разница пути ----------------------

func TestQSC_OwnerAndAccessServiceDifferByPathAlone(t *testing.T) {
	t.Parallel()
	const body = "const reasonQuotaExceeded = \"QUOTA_EXCEEDED\"\n"

	owner := surfacesOf("services/vpc/internal/apps/kacho/shared/quota.go", body)
	access := surfacesOf("services/iam/internal/apps/kaname/shared/quota.go", body)

	require.Contains(t, owner, quotaSurfaceOwners)
	require.NotContains(t, owner, quotaSurfaceIAMLedger)
	require.Contains(t, access, quotaSurfaceIAMLedger)
	require.NotContains(t, access, quotaSurfaceOwners,
		"содержимое побайтово одно, различается ТОЛЬКО путь — и это единственный факт, "+
			"которым §2 приёмки различает поверхности B и C")
}

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
