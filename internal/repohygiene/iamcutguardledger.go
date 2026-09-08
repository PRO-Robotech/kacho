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
//	               по которым его сюда отнесли, оказались синтетическими;
//	SubjectMoved — предмет ПЕРЕНЕСЁН в дерево службы вместе со своим единственным
//	               исполнителем. Отличается от SubjectLeaves тем, ЧТО именно
//	               произошло: там предмет остаётся у платформы и просто перестаёт
//	               быть нашим, здесь платформенной координаты больше нет вовсе —
//	               файл переехал. Запись называет НОВУЮ координату, и она обязана
//	               существовать, пока служба в дереве: иначе перенос объявлен и
//	               не сделан.
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
	iamCutSubjectMoved iamCutVerdict = "предмет перенесён к службе"
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
	// SuccessorFile — файл преемника в дереве платформы (Transferred, Covered)
	// ЛИБО новая координата самого предмета (SubjectMoved).
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
		SuccessorIssue:  2398,
		Why: "РАЗДЕЛЕНО НАПОЛОВИНУ (#2375). Форма связи выбрана и ПОСАЖЕНА: объявление " +
			"`pkg/moduleselfgating` называет отношения, которые модуль платформы гейтит " +
			"своим кодом, живёт в фундаменте (оба продукта тянут его зависимостью), а " +
			"истинность держит платформенный гейт — сверка двусторонняя, обе её координаты " +
			"платформенные, разрез она переживает. Вторая половина — чтение объявления " +
			"носителем — одним изменением с первой landing НЕ МОЖЕТ: служба тянет фундамент " +
			"ПИНОМ, и пакета, заведённого этим же изменением, в пиннутой ревизии нет by " +
			"construction. Замер, ради которого объявление заведено: из 109 пар единственным " +
			"читателем был прод-код ЧУЖОГО модуля у ШЕСТИ — ровно они стали бы ложными " +
			"находками при простом сужении полосы",
	},
	{
		Carrier: "services/iam/internal/authzmap/nonverb_relation_has_reader_test.go",
		Verdict: iamCutReclassified,
		Why: "ЗАМЕР ОПРОВЕРГ ПОСЫЛКУ (#2375): координату платформы носитель читал, а " +
			"предмета за ней не было. Из 30 неглагольных отношений, читаемых только " +
			"прод-кодом, прод-код ЧУЖОГО модуля был единственным читателем у НУЛЯ — пять " +
			"читает сама служба, двадцать пять она и соседи. Полоса сужена до обхода " +
			"СОБСТВЕННОГО модуля, и вердикт не изменился ни на единицу: 30 читаются кодом, " +
			"мёртвых 0, — проверено прогоном в клоне без дерева платформы. Направление " +
			"возможной ошибки названо у носителя: станет чужой модуль единственным " +
			"читателем — гейт даст КРАСНОЕ, а не молчание, и это чинится записью в ведомость",
	},
	{
		Carrier:         "services/iam/internal/authzguard/public_caller_policy_test.go",
		PlatformSubject: "services",
		Verdict:         iamCutCovered,
		SuccessorFile:   "internal/repohygiene/platformmodulevocabulary_test.go",
		SuccessorTest:   "TestPlatformModuleVocabularyMatchesTheTree",
		Why: "РАЗДЕЛЕНО (#2376). Носитель выводил перечень законных отправителей ОБХОДОМ " +
			"каталога служб платформы — после разреза выводить его было бы неоткуда, и " +
			"круг отправителей остался бы без сторожа МОЛЧА. Источником перечня стало " +
			"объявление фундамента (pkg/platformmodules, колонка коротких имён служб — " +
			"по определению «каталог services/<X> и короткое имя SAN его mTLS»); фундамент " +
			"оба продукта тянут зависимостью, поэтому половина «допуск называет объявленный " +
			"модуль» исполняется в клоне службы. Половина «объявление сходится с деревом» " +
			"у платформы УЖЕ была под сторожем — чужой работой, и он судит только " +
			"платформенные координаты, значит разрез переживает",
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
		Verdict:         iamCutCovered,
		SuccessorFile:   "deploy/iam_module_manifest_producer_test.go",
		SuccessorTest:   "TestModuleManifestConfigMapHasAProducer",
		Why: "РАЗДЕЛЕНО (#2377). Сверяет объявленные манифестом роли с живой базой: " +
			"манифест у платформы, база и миграции у службы, и после разреза сверка была " +
			"бы невыразима — расхождение объявления с фактом перестало бы находиться. " +
			"Перечень манифестов стал ФУНКЦИЕЙ ПОСАДКИ (internal/testsupport/modulemanifests): " +
			"в дереве платформы читаются все шесть, как прежде — числа переписи не " +
			"изменились, — в самостоятельном клоне читается манифест самого модуля, " +
			"входящий в его поставку. Сверка сужается, но НЕ ИСЧЕЗАЕТ, и её объём вместе с " +
			"посадкой печатается отдельной строкой. Копии соседних манифестов в дерево " +
			"службы не заводятся намеренно: они доезжают ДОСТАВКОЙ в рантайме, и у " +
			"доставки уже есть платформенный сторож — он судит популяцию стендов, ключи " +
			"один к одному с деревом и побайтовые тела, то есть ровно ту сторону, которая " +
			"у платформы остаётся",
	},
	{
		Carrier:         "services/iam/internal/moduleseedparity/parity_test.go",
		PlatformSubject: "services/vpc/manifest.yaml",
		Verdict:         iamCutCovered,
		SuccessorFile:   "deploy/iam_module_manifest_producer_test.go",
		SuccessorTest:   "TestModuleManifestConfigMapHasAProducer",
		Why: "вторая половина того же паритета — посев; исход и механизм те же, что у " +
			"соседней записи (#2377), и заведены они одним изменением",
	},
	{
		Carrier:       "services/iam/internal/scopesourcecensus/census_integration_test.go",
		Verdict:       iamCutSubjectMoved,
		SuccessorFile: "services/iam/tools/scope-source-census.sh",
		Why: "ПРИБОР ПЕРЕНЕСЁН В ДЕРЕВО СЛУЖБЫ (#2378). Он лежал под deploy/ платформы, " +
			"а исполнитель у него был ровно один — эта проба, внутри службы. Читателей вне " +
			"службы ноль, поэтому после разреза прибор остался бы у платформы без единого " +
			"исполнителя: инструмент, которого не зовут, неотличим от исправного — он не " +
			"падает. Предмет прибора целиком служебный (представление цепи областей, " +
			"проекция журнала, перечень типов у генератора её же модуля), поэтому переезд " +
			"к исполнителю, а не заведение читателя у платформы. Координата в пробе берётся " +
			"теперь у резолва посадки и потому одна для обеих",
	},
	{
		Carrier:         "services/iam/internal/apps/kaname/api/access_binding/reconcile/tuples_create_grant_scope_test.go",
		PlatformSubject: "gateway/internal/middleware/embed/permission_catalog.json",
		Verdict:         iamCutCovered,
		SuccessorFile:   "internal/repohygiene/catalogparity_test.go",
		SuccessorTest:   "TestCatalogMatchesTheAnnotationsItWasGeneratedFrom",
		Why: "РАЗДЕЛЕНО (#2379). Стороны сквозного утверждения лежали в разных деревьях: " +
			"популяция у каталога прав КРАЯ, тип у канона в каталоге контрактов, кортежи у " +
			"эмиттера службы. Расширение доступа сверх выданного — класс тихий (каждая " +
			"сторона согласована сама с собой), поэтому замолчавший сторож здесь неотличим " +
			"от исправного. Обе внешние стороны взяты теперь из ПОСТАВКИ модуля: каталог — " +
			"его побайтовая вшитая копия, канон — резолв authzplan, спрашивающий контракт " +
			"первым и переходящий к вшитой копии только там, где каталога контрактов нет " +
			"вовсе (в монорепо вторая ветвь недостижима, числа переписей не изменились). " +
			"Половина «две вшитые копии каталога не разошлись» у платформы УЖЕ была под " +
			"сторожем — он судит обе координаты платформы и разрез переживает",
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
