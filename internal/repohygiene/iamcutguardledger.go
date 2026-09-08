// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iamcutguardledger.go — ВЕДОМОСТЬ сторожей, которых унесёт разрез службы
// доступа (задача продукта #2361, адъюдикация #2330).
//
// # Зачем ведомость, а не молчание
//
// Поимённая адъюдикация носителей гейтов внутри `services/iam` нашла те, что
// судят координату ВНЕ службы. После разреза они уедут вместе с ней и в её
// клоне честно объявят третий исход («условие не создано»). Предмет при этом
// никуда не денется — он у платформы, — а сторожа у него не станет.
//
// Это не поломка, а ПОТЕРЯ, и она молчаливая: ни красного, ни зелёного.
// Молчащий сторож неотличим от исправного, поэтому по каждому носителю назван
// ИСХОД, и он записан здесь — в дереве, которое разрез переживёт.
//
// # Пять исходов, шестого нет
//
//	Transferred  — утверждение ПЕРЕЕХАЛО в платформу; назван преемник, и он
//	               обязан существовать;
//	Covered      — преемник у платформы УЖЕ был, заведён чужой работой; чинить
//	               закрытое не надо, но назвать его надо, иначе следующий заведёт
//	               второе место об одном предмете;
//	SubjectLeaves— предмет уезжает вместе со службой; в платформе его не
//	               останется, и сторожить будет нечего;
//	Remainder    — предмет остаётся, преемника СЕГОДНЯ нет; назван номер задачи,
//	               которая его заводит;
//	Reclassified — носитель платформенного предмета не стерёг вовсе; координаты,
//	               по которым его сюда отнесли, оказались синтетическими.
//
// «Оставить как есть» исходом не является.
//
// # Чем ведомость ИСТЕКАЕТ САМА
//
// Гейт-сосед требует от каждой записи трёх вещей: пока служба в дереве —
// носитель существует; предмет платформы, которым запись объяснена, —
// существует; названный преемник — существует и объявляет названную пробу.
// Запись, потерявшая любое из трёх, есть НАХОДКА, а не «стало лучше»: иначе
// ведомость переживёт то, что ею обозначалось, ровно как переживает свой
// предмет комментарий.
//
// На ПУСТОЙ ведомости гейт проходит: пустая ведомость — это цель, а не поломка.
package repohygiene

// iamCutVerdict — исход по одному носителю.
type iamCutVerdict string

const (
	iamCutTransferred  iamCutVerdict = "перенесено"
	iamCutCovered      iamCutVerdict = "преемник уже был"
	iamCutSubjectLeave iamCutVerdict = "предмет уезжает"
	iamCutRemainder    iamCutVerdict = "остаток"
	iamCutReclassified iamCutVerdict = "переклассифицировано"
)

// iamCutGuard — одна запись ведомости.
type iamCutGuard struct {
	// Carrier — носитель внутри службы, от корня дерева платформы.
	Carrier string
	// PlatformSubject — координата ПЛАТФОРМЫ, которую носитель судил. Пусто
	// допустимо только у Reclassified: там платформенного предмета нет вовсе.
	PlatformSubject string
	// Verdict — исход.
	Verdict iamCutVerdict
	// SuccessorFile — файл преемника в дереве платформы (Transferred, Covered).
	SuccessorFile string
	// SuccessorTest — имя пробы, которую преемник обязан объявлять.
	SuccessorTest string
	// SuccessorIssue — номер задачи-преемника (Remainder).
	SuccessorIssue int
	// Why — чем исход обоснован. Проза для читателя, не предикат.
	Why string
}

// iamCutGuardLedger — ведомость целиком.
//
// Порядок записей — тот же, что в адъюдикации #2330: перечень читают рядом с
// ней, и перестановка сделала бы сверку ручной.
var iamCutGuardLedger = []iamCutGuard{
	{
		Carrier:         "services/iam/internal/authzmap/materialized_relation_has_reader_test.go",
		PlatformSubject: "gateway/internal/middleware/embed/permission_catalog.json",
		Verdict:         iamCutRemainder,
		SuccessorIssue:  2375,
		Why: "читателей отношения ищет в каталоге края и в прод-коде служб; после разреза " +
			"объявление и читатели окажутся в разных деревьях, и вопрос станет некому задать",
	},
	{
		Carrier:         "services/iam/internal/authzmap/nonverb_relation_has_reader_test.go",
		PlatformSubject: "services",
		Verdict:         iamCutRemainder,
		SuccessorIssue:  2375,
		Why:             "то же для неглагольных отношений: литерал ищется по прод-коду всех служб платформы",
	},
	{
		Carrier:         "services/iam/internal/authzguard/public_caller_policy_test.go",
		PlatformSubject: "services",
		Verdict:         iamCutRemainder,
		SuccessorIssue:  2376,
		Why: "перечень законных отправителей выводится обходом каталога служб платформы; " +
			"в самостоятельной посадке выводить его будет неоткуда",
	},
	{
		Carrier:         "services/iam/internal/check/contract_id_form_test.go",
		PlatformSubject: "internal/repohygiene/ct2_docs_idform.go",
		Verdict:         iamCutCovered,
		SuccessorFile:   "internal/repohygiene/ct2_docs_idform_test.go",
		SuccessorTest:   "TestDocsIDFormMatchesWhatTheCodeMints",
		Why: "предмет носителя — примеры в контракте службы, и он уезжает с ней; " +
			"платформенный класс «показанная форма id — та, которую чеканит код» " +
			"уже под сторожем, и словарь там ВЫВОДИТСЯ из дерева",
	},
	{
		Carrier:         "services/iam/internal/check/retired_block_storage_test.go",
		PlatformSubject: "gateway/internal/middleware/embed/permission_catalog.json",
		Verdict:         iamCutTransferred,
		SuccessorFile:   "internal/repohygiene/retiredblockstorageedgecatalog_test.go",
		SuccessorTest:   "TestRetiredBlockStorageIsNotInTheEdgePermissionCatalog",
		Why: "РАЗДЕЛЕНО: половина края переехала в платформу, половина службы (вшитая " +
			"копия каталога, словари, модель) осталась у носителя",
	},
	{
		Carrier:         "services/iam/internal/check/literal_origin_test.go",
		PlatformSubject: "Makefile",
		Verdict:         iamCutSubjectLeave,
		Why: "судит, не производит ли цель сборки вшитый литерал службы из строк каталога; " +
			"литерал и его пакет уезжают со службой, и в корневом файле сборки платформы " +
			"предмета не останется",
	},
	{
		Carrier:         "services/iam/internal/manifest/producerdelivery_test.go",
		PlatformSubject: "deploy/stacks.txt",
		Verdict:         iamCutCovered,
		SuccessorFile:   "deploy/iam_module_manifest_producer_test.go",
		SuccessorTest:   "TestModuleManifestConfigMapHasAProducer",
		Why: "предмет носителя — приём доставленного СТАРТОВЫМ ЧИТАТЕЛЕМ службы, и он " +
			"уезжает с ней; сторона производителя (популяция стендов из таблицы, ключи " +
			"один к одному с деревом, побайтовый круг) у платформы уже под сторожем",
	},
	{
		Carrier:         "services/iam/internal/manifest/peekmodule_internal_test.go",
		PlatformSubject: "services/vpc/manifest.yaml",
		Verdict:         iamCutTransferred,
		SuccessorFile:   "internal/repohygiene/modulemanifestreadable_test.go",
		SuccessorTest:   "TestModuleManifestsOfThePlatformAreReadable",
		Why: "премиса двухступенчатого обхода читала ЖИВЫЕ манифесты платформы; " +
			"разбираемость и самоописание документов платформы переехали к ней",
	},
	{
		Carrier:         "services/iam/internal/manifest/peekobjecttypes_internal_test.go",
		PlatformSubject: "services/vpc/manifest.yaml",
		Verdict:         iamCutTransferred,
		SuccessorFile:   "internal/repohygiene/modulemanifestreadable_test.go",
		SuccessorTest:   "TestModuleManifestsOfThePlatformAreReadable",
		Why: "близнец предыдущей записи по типам объектов; столкновение типов между " +
			"модулями судится теперь у платформы",
	},
	{
		Carrier:         "services/iam/internal/modelcompose/compose_test.go",
		PlatformSubject: "services/vpc/manifest.yaml",
		Verdict:         iamCutSubjectLeave,
		Why: "предмет — КОМПОЗИЦИЯ модели из манифестов, механизм службы; манифесты " +
			"здесь только популяция. Сторону стендов и чартов платформа судит своим " +
			"гейтом допуска композиции",
	},
	{
		Carrier:         "services/iam/internal/moduleroleparity/parity_test.go",
		PlatformSubject: "services/vpc/manifest.yaml",
		Verdict:         iamCutRemainder,
		SuccessorIssue:  2377,
		Why: "сверяет объявленные манифестом роли с живой базой; манифест у платформы, " +
			"база и миграции у службы — после разреза сверка невыразима",
	},
	{
		Carrier:         "services/iam/internal/moduleseedparity/parity_test.go",
		PlatformSubject: "services/vpc/manifest.yaml",
		Verdict:         iamCutRemainder,
		SuccessorIssue:  2377,
		Why:             "вторая половина того же паритета — посев",
	},
	{
		Carrier:         "services/iam/internal/scopesourcecensus/census_integration_test.go",
		PlatformSubject: "deploy/load-tests/iam-scope-source-census.sh",
		Verdict:         iamCutRemainder,
		SuccessorIssue:  2378,
		Why: "исполняет прибор, лежащий в дереве платформы; читателей вне службы у " +
			"прибора ноль, и после разреза он останется без единого исполнителя",
	},
	{
		Carrier:         "services/iam/internal/apps/kaname/api/access_binding/reconcile/tuples_create_grant_scope_test.go",
		PlatformSubject: "gateway/internal/middleware/embed/permission_catalog.json",
		Verdict:         iamCutRemainder,
		SuccessorIssue:  2379,
		Why: "популяция берётся из каталога края, тип из канона службы, кортежи у её " +
			"эмиттера; после разреза стороны окажутся в разных деревьях",
	},
	{
		Carrier:         "services/iam/internal/apps/kaname/api/session_revocations/is_revoked_doc_test.go",
		PlatformSubject: "gateway/internal/clients/session_revocations_client.go",
		Verdict:         iamCutTransferred,
		SuccessorFile:   "internal/repohygiene/edgerevocationreader_test.go",
		SuccessorTest:   "TestEdgeRevocationLaneHasAReaderOnTheRequestPath",
		Why: "РАЗДЕЛЕНО: живость читателя у края переехала в платформу, согласие " +
			"комментариев службы с деревом осталось у носителя",
	},
	{
		Carrier:         "services/iam/internal/apps/kaname/api/session_revocations/is_revoked_doc_injection_test.go",
		PlatformSubject: "gateway/internal/clients/session_revocations_client.go",
		Verdict:         iamCutTransferred,
		SuccessorFile:   "internal/repohygiene/edgerevocationreader_injection_test.go",
		SuccessorTest:   "TestEdgeRevocationGate_SilentOnALiveLane",
		Why:             "доказательство предыдущей записи; переехало вместе с нею",
	},
	{
		Carrier:         "services/iam/tools/void_message_names_the_walk_root_test.go",
		PlatformSubject: "",
		Verdict:         iamCutReclassified,
		Why: "координаты, по которым носитель отнесли к платформенным, СИНТЕТИЧЕСКИЕ: " +
			"дерево строится во временном каталоге самой пробой. Единственная живая " +
			"координата — обёртка внутри службы, и она уезжает с ней",
	},
}
