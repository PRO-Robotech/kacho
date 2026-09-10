// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_freshness_census_test.go — ОКНО СВЕЖЕСТИ АУТЕНТИФИКАЦИИ: перепись
// потребителей и защита отрицательного утверждения от беспредметности.
// Приёмка IAM-INT-1, сценарий 26 (S3).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У края есть механизм свежести: требование, чтобы церемония аутентификации
// была проведена НЕ ДАВНЕЕ заданного окна. Он живой и решает
// (`grpcsrv.EvaluateStepUp`, арм 3; `StepUpGate.CheckAssurance`;
// `BuildStepUpChallenge` дорисовывает `max_age=`).
//
// Записей-потребителей у него НОЛЬ. Это СЕГОДНЯШНЕЕ СОСТОЯНИЕ, зафиксированное
// осознанно, а не упущение: введение окна свежести вынесено приёмкой за объём
// (§5) и требует отдельного решения владельца со своей приёмкой — «какие
// операции требуют свежей церемонии» есть продуктовый вопрос, а не следствие
// этой под-фазы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ У ОТРИЦАНИЯ ЗДЕСЬ ОБЯЗАНА БЫТЬ ПРЕДПОСЫЛКА
//
// «Записей ноль» — утверждение ОТРИЦАТЕЛЬНОЕ, а такие замолкают вместе со своим
// предметом (`testing.md` §«Гейт на класс», п. 9). Сними механизм свежести — и
// строка «потребителей ноль» станет истинной BY CONSTRUCTION: считать будет
// нечего, проба останется зелёной, счётчик утверждений продолжит расти, и
// отличить это от исправной работы будет нельзя ничем.
//
// Поэтому гейт краснеет в ОБЕ стороны, и обе названы:
//
//	(1) появилась запись, объявляющая окно, — а решения владельца не было;
//	(2) сняли МЕХАНИЗМ, пока отрицание о нём высказывается.
//
// Второе утверждается ИСХОДОМ, а не объявлением: правило зовётся и его вердикт
// сверяется. Разбор исходника показал бы, что слова на месте, — а слова остаются
// на месте и в комментарии, объясняющем снятую ветвь.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАМЕР НА МОМЕНТ ЗАВЕДЕНИЯ (перемеряй предикатами ниже, а не памятью)
//
// Цепочка от объявления до решения оборвана В ДВУХ местах, и это шире, чем
// «нет записей»:
//
//	proto-опция свежести           — не существует
//	запись каталога                — 0 из 350 в ОБЕИХ вшитых копиях
//	поле `CatalogEntry.RequiresMFAFresh` — объявлено, читателей 0
//	`PermissionRequirement.MFAMaxAge`    — в прод-коде не присваивается нигде
//	`grpcsrv.EvaluateStepUp` арм 3       — ЖИВОЙ, решает
//
// Гейт судит первое звено (запись каталога) и последнее (механизм). Средние два
// он НЕ судит намеренно: их провязка — это и есть работа, которую введение окна
// выполнит, и гейт, требующий их отсутствия, краснел бы на правильной правке.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// freshnessCensus — что перепись увидела. Печатается ЦЕЛИКОМ: «ноль находок»
// обязано быть отличимо от «ноль прочитанного», а для этого объём осмотренного
// есть отдельное утверждение, а не подразумеваемое.
type freshnessCensus struct {
	copyName    string
	shape       string // форма верхнего уровня, которую увидел разбор
	entriesRead int
	consumers   []string // FQN записей, объявивших окно свежести
}

// catalogEntryShape — запись каталога, разобранная ровно настолько, насколько
// нужно этому гейту.
//
// Разбор СВОЙ, а не через продовый загрузчик, и это решение: загрузчик отдаёт
// `CatalogEntry`, и «поле не выставлено» у него неотличимо от «ключа в JSON нет
// вовсе». Второе — сегодняшнее состояние (генератор ключ не эмитит), и перепись
// обязана уметь их различить, иначе она не заметит, как ключ начнёт эмититься
// со значением `false`: это уже провязка, и о ней надо знать.
type catalogEntryShape struct {
	FQN              string `json:"fqn"`
	RequiresMFAFresh bool   `json:"requires_mfa_fresh"`
}

// censusFreshnessConsumers — ПРЕДИКАТ гейта, отделённый от источника входа.
//
// Отделён затем, чтобы инъекция гоняла ТУ ЖЕ функцию, что судит дерево. Проба,
// доказывающая способность падать на своей копии предиката, доказывает свойство
// копии.
// ОБЕ ФОРМЫ ВЕРХНЕГО УРОВНЯ разбираются ровно так, как их разбирает продовый
// загрузчик (`PermissionCatalog.LoadFromBytes`): сперва объектная
// `{"entries": [...]}`, затем голый массив. Сегодня генератор эмитит МАССИВ.
//
// Знать обе обязательно, и это не запас на будущее: предикат, знающий одну
// форму, на другой не краснеет и не зеленеет — он МОЛЧИТ, потому что видит нуль
// записей и честно сообщает «потребителей ноль». Ровно так эта проба и была
// написана в первой редакции; поймала её инъекция, а не чтение.
//
// Форма ВОЗВРАЩАЕТСЯ вызывающему и печатается: смена формы генератором обязана
// быть видна, а не поглощена терпимостью.
func censusFreshnessConsumers(name string, raw []byte) (freshnessCensus, error) {
	var asObject struct {
		Entries []catalogEntryShape `json:"entries"`
	}
	var entries []catalogEntryShape
	shape := ""
	if err := json.Unmarshal(raw, &asObject); err == nil && len(asObject.Entries) > 0 {
		entries, shape = asObject.Entries, `объектная {"entries": [...]}`
	} else {
		var asArray []catalogEntryShape
		if err := json.Unmarshal(raw, &asArray); err != nil {
			return freshnessCensus{copyName: name}, err
		}
		entries, shape = asArray, "массив [...]"
	}

	out := freshnessCensus{copyName: name, shape: shape, entriesRead: len(entries)}
	for _, e := range entries {
		if e.RequiresMFAFresh {
			out.consumers = append(out.consumers, e.FQN)
		}
	}
	return out, nil
}

// TestStepUpFreshness_NoCatalogEntryDeclaresAWindow — сторона (1): запись,
// появившаяся без решения владельца, роняет гейт и называет FQN.
func TestStepUpFreshness_NoCatalogEntryDeclaresAWindow(t *testing.T) {
	copies := map[string][]byte{
		"gateway/internal/middleware/embed/permission_catalog.json": middleware.EmbeddedPermissionCatalogJSON(),
	}

	// Копий каталога в ЭТОМ дереве одна. Прежде их было две — вторую нёс посев
	// службы доступа, и обе судились ОТДЕЛЬНО, а не одна с опорой на
	// байт-идентичность (опора на чужое утверждение сделала бы вердикт этого
	// гейта функцией того, прогнали ли соседа). Довод остаётся нормой; вторая
	// копия уехала вместе со службой (задача #1111) и судится её деревом.
	//
	// Число здесь ВЫВОДИТСЯ из состава карты, а не сравнивается с литералом:
	// требование «копий ровно две» пережило бы свой предмет молча, а требование
	// непустоты держит то, ради чего оно стояло, — пустая перепись не даёт
	// зелёного.
	require.NotEmpty(t, copies, "перепись обязана осмотреть хотя бы одну вшитую копию каталога")

	for path, raw := range copies {
		c, err := censusFreshnessConsumers(path, raw)
		require.NoError(t, err, "каталог %s не разобрался — вердикта о нём нет", path)

		// Предпосылка: пустой обход не даёт зелёного. Каталог из нуля записей
		// сделал бы «потребителей ноль» истинным by construction.
		require.Positive(t, c.entriesRead,
			"обход каталога %s пуст — вердикт беспредметен, а не чист", path)

		t.Logf("перепись: копия %s · форма %s · записей прочитано %d · объявляют окно свежести %d",
			c.copyName, c.shape, c.entriesRead, len(c.consumers))

		assert.Emptyf(t, c.consumers,
			"запись объявила окно свежести аутентификации, а решения владельца не было: %s\n"+
				"Введение окна вынесено за объём приёмки IAM-INT-1 (§5) и требует отдельного\n"+
				"решения владельца со своей приёмкой: какие операции требуют СВЕЖЕЙ церемонии —\n"+
				"вопрос продуктовый. Решили ввести — правьте этот гейт ТЕМ ЖЕ изменением,\n"+
				"назвав решение; ведомости прощённых записей здесь нет намеренно.",
			strings.Join(c.consumers, ", "))
	}
}

// TestStepUpFreshness_MechanismIsAliveSoTheNegativeIsNotVacuous — сторона (2):
// сняли механизм, пока отрицание о нём высказывается, — гейт краснеет.
//
// Утверждается ИСХОД правила, а не наличие слов в исходнике: три вердикта,
// различающиеся ровно одним фактом каждый.
func TestStepUpFreshness_MechanismIsAliveSoTheNegativeIsNotVacuous(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	const window = time.Hour

	// База: пол пройден, окно объявлено, церемония свежая — проход.
	fresh := grpcsrv.StepUpInput{
		PrincipalType: "user",
		PresentedACR:  "2",
		RequiredACR:   "2",
		AuthTime:      now.Add(-10 * time.Minute),
		MFAMaxAge:     window,
		Now:           now,
	}
	require.Equal(t, grpcsrv.StepUpAllow, grpcsrv.EvaluateStepUp(fresh),
		"положительный контроль: свежая церемония в объявленном окне обязана проходить — "+
			"без него отрицания ниже зеленели бы на правиле, отвергающем всё")

	// Отличие ровно в одном факте: церемония старше окна.
	stale := fresh
	stale.AuthTime = now.Add(-2 * window)
	require.Equal(t, grpcsrv.StepUpDenyMFAStale, grpcsrv.EvaluateStepUp(stale),
		"МЕХАНИЗМ СВЕЖЕСТИ СНЯТ ИЛИ ПЕРЕСТАЛ РЕШАТЬ: церемония старше окна обязана "+
			"отвергаться. Пока этот вердикт производится, утверждение «записей-потребителей "+
			"ноль» имеет предмет; перестанет — оно станет истинным by construction, и "+
			"перепись выше замолчит, не сообщив об этом")

	// Отличие ровно в одном факте: отметки о времени церемонии нет вовсе.
	missing := fresh
	missing.AuthTime = time.Time{}
	require.Equal(t, grpcsrv.StepUpDenyAuthTimeMissing, grpcsrv.EvaluateStepUp(missing),
		"МЕХАНИЗМ СВЕЖЕСТИ СНЯТ ИЛИ ПЕРЕСТАЛ РЕШАТЬ: отсутствие отметки времени "+
			"церемонии при объявленном окне обязано отвергаться fail-closed")

	// Окно НЕ объявлено — те же входы обязаны проходить. Законный близнец:
	// без него два отрицания выше зеленели бы и на правиле, отвергающем всякую
	// несвежую церемонию независимо от того, объявлено ли окно.
	noWindow := stale
	noWindow.MFAMaxAge = 0
	require.Equal(t, grpcsrv.StepUpAllow, grpcsrv.EvaluateStepUp(noWindow),
		"законный близнец: без объявленного окна давность церемонии не решает ничего — "+
			"иначе окно было бы включено для всех, а не для названных записей")

	// Вызов виден и вызывающему: требование дорисовывает `max_age` в вызов
	// повторной аутентификации. Без этого клиент не узнает, ЧТО от него хотят.
	challenge := middleware.BuildStepUpChallenge(
		middleware.PermissionRequirement{RequiredACRMin: "2", MFAMaxAge: window}, "1")
	require.Contains(t, challenge, `max_age="3600"`,
		"МЕХАНИЗМ СВЕЖЕСТИ СНЯТ С ПРОТОКОЛЬНОЙ СТОРОНЫ: объявленное окно обязано "+
			"доезжать до вызывающего в вызове повторной аутентификации")

	withoutWindow := middleware.BuildStepUpChallenge(
		middleware.PermissionRequirement{RequiredACRMin: "2"}, "1")
	require.NotContains(t, withoutWindow, "max_age",
		"законный близнец: без объявленного окна вызов не вправе требовать свежести")

	t.Logf("перепись предпосылки: вердиктов правила проверено 4 · форм вызова 2 · "+
		"окно %s, момент %s", window, now.Format(time.RFC3339))
}

// TestStepUpFreshness_PredicateFindsAnInjectedConsumer — ДОКАЗАТЕЛЬСТВО
// способности предиката падать, на входе той же формы, что и настоящий каталог.
//
// Инъекция меняет ОДИН факт против законного близнеца: та же запись, тот же
// набор полей, различается только объявление окна.
func TestStepUpFreshness_PredicateFindsAnInjectedConsumer(t *testing.T) {
	const twin = `{"entries":[
		{"fqn":"kaname.cloud.iam.v1.UserTokenService/Issue","required_acr_min":"2"},
		{"fqn":"kacho.cloud.vpc.v1.NetworkService/Get","required_acr_min":"1"}
	]}`
	const injected = `{"entries":[
		{"fqn":"kaname.cloud.iam.v1.UserTokenService/Issue","required_acr_min":"2","requires_mfa_fresh":true},
		{"fqn":"kacho.cloud.vpc.v1.NetworkService/Get","required_acr_min":"1"}
	]}`

	clean, err := censusFreshnessConsumers("законный близнец", []byte(twin))
	require.NoError(t, err)
	require.Equal(t, 2, clean.entriesRead)
	assert.Empty(t, clean.consumers,
		"законный близнец обязан молчать: записи те же, окна не объявляет ни одна")

	found, err := censusFreshnessConsumers("инъекция", []byte(injected))
	require.NoError(t, err)
	require.Equal(t, 2, found.entriesRead,
		"инъекция обязана быть той же формы — иначе она проверяет разбор, а не предикат")
	require.Equal(t, []string{"kaname.cloud.iam.v1.UserTokenService/Issue"}, found.consumers,
		"предикат обязан НАЙТИ запись, объявившую окно, и НАЗВАТЬ её FQN")

	// Явно объявленное `false` потребителем НЕ является: ключ эмитится, окна нет.
	// Без этой пары гейт краснел бы на генераторе, начавшем эмитить ключ всегда.
	const explicitFalse = `{"entries":[
		{"fqn":"kacho.cloud.vpc.v1.NetworkService/Get","required_acr_min":"1","requires_mfa_fresh":false}
	]}`
	off, err := censusFreshnessConsumers("явное false", []byte(explicitFalse))
	require.NoError(t, err)
	require.Equal(t, 1, off.entriesRead)
	assert.Empty(t, off.consumers,
		"объявленное `false` — не потребитель: окно не объявлено, ключ лишь эмитится")

	// Пустой обход не даёт зелёного и здесь: предикат обязан ОТДАТЬ 0 записей,
	// а вызывающий — упасть на этом, что и делает проба дерева выше.
	empty, err := censusFreshnessConsumers("пустой каталог", []byte(`[]`))
	require.NoError(t, err)
	require.Zero(t, empty.entriesRead,
		"на пустом каталоге предикат обязан сообщить 0 прочитанных, а не промолчать")

	// Объектная форма с ПУСТЫМ перечнем — ошибка разбора, и это НЕ дефект
	// предиката: продовый загрузчик ведёт себя ровно так же (`len(Entries) > 0`
	// не выполнено ⇒ падение в массивную ветвь ⇒ объект массивом не разбирается).
	// Утверждается здесь затем, чтобы поведение было ЗАФИКСИРОВАНО, а не
	// обнаружено следующим читателем как неожиданность.
	_, err = censusFreshnessConsumers("объект с пустым перечнем", []byte(`{"entries":[]}`))
	require.Error(t, err,
		"объектная форма с пустым перечнем обязана быть ошибкой разбора — как у продового "+
			"загрузчика; молчаливый ноль здесь означал бы «прочитано ничего», выданное за «чисто»")

	// ТА ЖЕ ПАРА В ФОРМЕ, КОТОРУЮ ЭМИТИТ ГЕНЕРАТОР СЕГОДНЯ — голый массив.
	// Без неё инъекция доказывала бы предикат на форме, которой в дереве нет:
	// объектная ветвь разобралась бы, массивная осталась бы непроверенной, и
	// слепота к ней не покраснела бы ничем. Ровно так эта проба и была написана
	// в первой редакции.
	const twinArray = `[
		{"fqn":"kaname.cloud.iam.v1.UserTokenService/Issue","required_acr_min":"2"},
		{"fqn":"kacho.cloud.vpc.v1.NetworkService/Get","required_acr_min":"1"}
	]`
	const injectedArray = `[
		{"fqn":"kaname.cloud.iam.v1.UserTokenService/Issue","required_acr_min":"2","requires_mfa_fresh":true},
		{"fqn":"kacho.cloud.vpc.v1.NetworkService/Get","required_acr_min":"1"}
	]`

	cleanArr, err := censusFreshnessConsumers("законный близнец (массив)", []byte(twinArray))
	require.NoError(t, err)
	require.Equal(t, "массив [...]", cleanArr.shape,
		"разбор обязан НАЗВАТЬ увиденную форму — иначе смена формы пройдёт молча")
	require.Equal(t, 2, cleanArr.entriesRead)
	assert.Empty(t, cleanArr.consumers, "законный близнец в форме массива обязан молчать")

	foundArr, err := censusFreshnessConsumers("инъекция (массив)", []byte(injectedArray))
	require.NoError(t, err)
	require.Equal(t, "массив [...]", foundArr.shape)
	require.Equal(t, []string{"kaname.cloud.iam.v1.UserTokenService/Issue"}, foundArr.consumers,
		"предикат обязан находить потребителя и в той форме, которую эмитит генератор")

	t.Logf("перепись инъекции: входов проверено 6 · форм верхнего уровня 2 · " +
		"найдено потребителей 2 · законных близнецов промолчало 4")
}
