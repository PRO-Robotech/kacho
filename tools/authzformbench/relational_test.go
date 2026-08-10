// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFormEConstructorAnswersModelNotRequired — «модель не требуется» законный
// ответ, а не отказ, и он НЕ покрывает опечатку в имени формы.
//
// Пара положительного с отрицательным здесь несущая: одиночное «для формы E
// ошибки нет» зеленело бы и в конструкторе, который перестал отвечать ошибкой
// вообще, — то есть ровно тогда, когда сломано всё.
func TestFormEConstructorAnswersModelNotRequired(t *testing.T) {
	_, canon, err := ResolveCanonicalModel()
	require.NoError(t, err)

	dsl, note, err := ModelFor(FormE, string(canon))
	require.ErrorIs(t, err, ErrModelNotRequired,
		"конструктор обязан ответить именно «модель не требуется» — не ошибкой и не молчанием")
	require.Empty(t, dsl, "у формы, которой модель не требуется, не может быть текста модели")
	require.NotEmpty(t, note, "ответ обязан нести причину — её печатает отчёт")

	_, _, err = ModelFor(Form("no-such-form"), string(canon))
	require.Error(t, err, "неизвестная форма обязана оставаться ошибкой")
	require.NotErrorIs(t, err, ErrModelNotRequired,
		"опечатка в имени формы получила ответ «модель не требуется» — тогда этот ответ "+
			"покрывает и её, и шестая форма заводится из любой строки")
}

// TestFormEIsNamedInEveryPlaceTheMatrixCounts — форма заведена в СЛОВАРЯХ, а не
// только в реализации.
//
// Ячейка, которой нет в словаре операций, не входит в сумму категорий и не
// печатается: отчёт был бы полон по своему собственному счёту и молчал бы о том,
// ради чего замер и делается.
func TestFormEIsNamedInEveryPlaceTheMatrixCounts(t *testing.T) {
	require.Contains(t, AllForms, FormE, "шестая форма не попала в перечень форм")

	for _, op := range []Op{OpInlineGrant, OpInlineRevoke, OpCascade} {
		require.Containsf(t, opsAll, op, "операция %s не заведена в словаре — её ячейка не печатается", op)
	}
	require.Len(t, opsAll, len(opsWrite)+len(opsRead),
		"словарь операций разошёлся с полосами записи и чтения — какая-то ячейка не будет снята")

	sc := NewScenario(10, 3, 4, "editor", DefaultVerbs())
	require.Equal(t, len(sc.Subjects)+2, ExpectedGrantTuples(FormE, sc),
		"объявленная арифметика выдачи формы E — привязка, её субъекты и селектор")
	require.Len(t, Grant(FormE, sc), ExpectedGrantTuples(FormE, sc),
		"произведённое намерение разошлось с объявленной до прогона арифметикой")
	require.Len(t, RevokeSubject(FormE, sc, sc.Subjects[0]), 1)
	require.Len(t, RelabelOne(FormE, sc, sc.Object(sc.N)), 1)
}

// TestStmtProducersPassControlInBothDirections — производитель величины `StmtSQL`
// на КАЖДОМ месте снятия, проверенный исполнением.
//
// Обе половины обязательны и по отдельности бессмысленны: счётчик, который
// двигается сам по себе, мерит фон; счётчик, который не двигается на заведомом
// стейтменте, мерит что-то другое. Величина, у входа которой нет производителя,
// печатается нулём, неотличимым от измеренного, — поэтому исход контроля
// печатается в отчёте всегда, а не только при провале.
func TestStmtProducersPassControlInBothDirections(t *testing.T) {
	if testing.Short() {
		t.Skip("поднимает контейнеры")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	// Сторона движка. Контроль прогнан при подъёме стека; здесь он ПЕРЕПРОГОНЯЕТСЯ
	// независимо — исход, взятый из чужого поля, не является проверкой.
	_, again := VerifyEngineStmtProducer(ctx, stack.DB)
	require.Equal(t, stack.StmtProducer.OK, again.OK,
		"повторный контроль производителя движка дал другой ответ — величина недетерминирована")
	if !stack.StmtProducer.OK {
		// Провал контроля — законный, заранее названный исход, а не неудача пробы:
		// колонка тогда не печатается вовсе. Но он обязан быть ВИДЕН.
		t.Logf("производитель StmtSQL движка контроль НЕ прошёл: %s", stack.StmtProducer.Note)
		t.Logf("следствие объявлено заранее: колонка q этого места не печатается, "+
			"формулировка «на общем для форм уровне» из отчёта снимается (%s)", again.Note)
	}

	// Сторона формы E. Контроль прогоняется при открытии КАЖДОГО хранилища и
	// роняет открытие; здесь проверяется, что он именно исполняется, а не объявлен.
	cfg := DefaultConfig()
	cfg.Forms = []Form{FormE}
	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)
	sc := NewScenario(5, 2, 2, "editor", DefaultVerbs())
	st, err := r.NewSeededStore(ctx, FormE, sc, true, "stmt-control-E")
	require.NoError(t, err)
	defer func() { _ = st.Teardown(ctx) }()

	require.True(t, st.StmtProducer().OK)
	require.Empty(t, stmtNote(st))

	// Один вопрос — один стейтмент: вердикт формы E обязан быть ОДНИМ запросом,
	// и колонка обязана это показывать. Пообъектный обход страницы прошёл бы все
	// утверждения о вердиктах и провалился бы здесь.
	_, c, err := st.Check(ctx, sc.Subjects[0], "v_get", sc.Object(0))
	require.NoError(t, err)
	require.Equal(t, 1, c.StmtSQL, "одиночная проверка формы E стоила не один стейтмент")
	require.Zero(t, c.ReqEngine, "у формы E не может быть обращений к движку")

	page := sc.Objects()
	_, parts, pc, err := st.CheckPage(ctx, sc.Subjects[0], "v_get", page, 50, 8)
	require.NoError(t, err)
	require.Equal(t, 1, parts, "страница формы E разложена не на одну часть")
	require.Equal(t, 1, pc.StmtSQL,
		"страница формы E стоила больше одного стейтмента — предикат применяется к странице "+
			"идентификаторов ОДНИМ запросом, иначе меряется способ вызова, а не форма")
}

// TestFormEInlineTransactionIsMeasuredAndEngineSaysNotApplicable — четвёртый
// исход на своём месте.
//
// «Неприменимо by construction» здесь не оговорка, а самый содержательный
// результат: общей транзакции между БД предмета выдачи и внешним движком не
// бывает, и это единственная ячейка, где разница форм не в скорости.
func TestFormEInlineTransactionIsMeasuredAndEngineSaysNotApplicable(t *testing.T) {
	if testing.Short() {
		t.Skip("поднимает контейнеры")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	cfg := DefaultConfig()
	cfg.Forms = []Form{FormA, FormE}
	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)
	sc := NewScenario(5, 3, 2, "editor", DefaultVerbs())
	obj := sc.Object(sc.N + sc.Spare) // за пределами засева: объект заводится самой транзакцией
	data, grant := InlineIntent(sc, obj)

	stE, err := r.NewSeededStore(ctx, FormE, sc, true, "inline-E")
	require.NoError(t, err)
	defer func() { _ = stE.Teardown(ctx) }()
	cnt, err := stE.InlineGrant(ctx, data, grant)
	require.NoError(t, err, "у формы E выдача в транзакции писателя — обычная запись")
	require.Positive(t, cnt.StmtSQL)

	// И право, выданное этой транзакцией, ДЕЙСТВУЕТ: иначе «неприменимо у соседа»
	// сравнивалось бы с операцией, которая ничего не сделала.
	ok, _, err := stE.Check(ctx, sc.Subjects[0], "v_get", obj)
	require.NoError(t, err)
	require.True(t, ok, "объект, заведённый вместе с выдачей, не разрешён — встраиваемая "+
		"операция отчиталась о работе, которой не сделала")

	stA, err := r.NewSeededStore(ctx, FormA, sc, true, "inline-A")
	require.NoError(t, err)
	defer func() { _ = stA.Teardown(ctx) }()
	_, err = stA.InlineGrant(ctx, data, grant)
	require.ErrorIs(t, err, ErrNotApplicable)
	o, reason := classify(err)
	require.Equal(t, NotApplicable, o,
		"неприменимость по построению свернулась в другой исход — отчёт скажет «не-измеренных N», "+
			"и читатель решит, что замер не доехал")
	require.NotEmpty(t, reason, "четвёртая категория печатается С ПРИЧИНОЙ, а не строкой без неё")
}

// TestCascadeHoldsOnBothSidesWithEmptyMaterialization — утверждение о ПАРИТЕТЕ,
// а не о преимуществе формы E.
//
// Три верхних уровня доступа разрешаются каскадом намеренно: если бы права
// администратора облака материализовались, то при отставшем конвейере человек,
// обязанный чинить аварию, сам остался бы без прав — именно тогда, когда он
// нужен. Независимость каскада от конвейера — свойство ДЕЙСТВУЮЩЕЙ формы, уже
// закреплённое в продукте; замер, объявляющий преимуществом одной формы то, что
// есть у обеих, замером не является.
//
// Состояние «материализация не проходила» здесь конструируется прямо: хранилище
// засеяно структурно и БЕЗ единой строки выдачи.
func TestCascadeHoldsOnBothSidesWithEmptyMaterialization(t *testing.T) {
	if testing.Short() {
		t.Skip("поднимает контейнеры")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	cfg := DefaultConfig()
	cfg.Forms = []Form{FormA, FormE}
	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)
	sc := NewScenario(6, 2, 3, "editor", DefaultVerbs())

	for _, f := range []Form{FormA, FormE} {
		st, err := r.NewSeededStore(ctx, f, sc, false, "cascade-empty-"+string(f))
		require.NoErrorf(t, err, "засев %s", f)
		_, err = st.Write(ctx, CascadeSeed(f, sc))
		require.NoErrorf(t, err, "каскадный принципал %s", f)

		allowed, _, err := st.Check(ctx, cascadeAdmin, "v_get", sc.Object(0))
		require.NoErrorf(t, err, "каскадный вопрос %s", f)
		require.Truef(t, allowed,
			"форма %s отказала каскадному принципалу на непройденной материализации — "+
				"аварийный путь у неё зависит от конвейера, а у соседа нет", f)

		// Парный отрицательный на КАЖДОЙ форме: без него «разрешено каскадному»
		// неотличимо от «разрешено всем».
		denied, _, err := st.Check(ctx, sc.Subjects[0], "v_get", sc.Object(0))
		require.NoErrorf(t, err, "контрольный вопрос %s", f)
		require.Falsef(t, denied,
			"форма %s разрешила обычному арендатору при пустой выдаче — тогда её каскад "+
				"ничего не сужает и утверждение о паритете вакуумно", f)

		// Второй отрицательный, и он про ДРУГОЕ: первый отличает каскадного
		// принципала от обычного арендатора, этот — «администратор ЭТОГО аккаунта»
		// от «администратора любого». Пока в фикстуре был один аккаунт, различить их
		// было нечем: корреляция аккаунта в каскадной ветви снималась без единой
		// красной пробы, и обе формы отвечали бы одинаково правильно по случайности.
		foreign, _, err := st.Check(ctx, cascadeAdmin, "v_get", sc.ForeignObject())
		require.NoErrorf(t, err, "кросс-аккаунтный каскадный вопрос %s", f)
		require.Falsef(t, foreign,
			"форма %s разрешила администратору аккаунта объект ЧУЖОГО аккаунта — её каскад "+
				"не сверяет аккаунт, то есть даёт доступ ко всему кластеру уровнем ниже кластера", f)

		require.NoError(t, st.Teardown(ctx))
	}
}

// TestAccountScopedGrantReachesObjectsOfItsProjects — вложенность области
// транзитивна, и второй родительский указатель зеркала имеет читателя.
//
// Колонка, которую не читает ни один запрос, — мёртвый вес, который тем не менее
// попадёт в объём и польстит соседу; колонка, которую читает запрос без пробы, —
// ветвь, о которой замер молчит. Здесь проверяется, что аккаунтная выдача
// накрывает объекты проектов аккаунта, а посторонний по-прежнему получает отказ.
func TestAccountScopedGrantReachesObjectsOfItsProjects(t *testing.T) {
	if testing.Short() {
		t.Skip("поднимает контейнеры")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	cfg := DefaultConfig()
	cfg.Forms = []Form{FormE}
	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)
	sc := NewScenario(5, 2, 2, "editor", DefaultVerbs())

	st, err := r.NewSeededStore(ctx, FormE, sc, false, "scope-account-E")
	require.NoError(t, err)
	defer func() { _ = st.Teardown(ctx) }()

	_, err = st.Write(ctx, GrantScoped(sc, accountObj))
	require.NoError(t, err)

	ok, _, err := st.Check(ctx, sc.Subjects[0], "v_get", sc.Object(0))
	require.NoError(t, err)
	require.True(t, ok, "аккаунтная выдача не накрыла объект проекта этого аккаунта — "+
		"вложенность области не транзитивна, и родительский указатель аккаунта ничего не решает")

	ok, _, err = st.Check(ctx, "user:stranger-not-in-any-binding", "v_get", sc.Object(0))
	require.NoError(t, err)
	require.False(t, ok, "аккаунтная выдача разрешила постороннему — она не сужает ничего")

	ok, _, err = st.Check(ctx, sc.Subjects[0], "v_get", sc.Object(sc.N))
	require.NoError(t, err)
	require.False(t, ok, "аккаунтная выдача накрыла объект ВНЕ набора — правило-селектор "+
		"не сужает, и метка перестала что-либо значить")

	// Аккаунтный близнец кросс-арендного вопроса вопросника — и единственное
	// утверждение, от которого зависит КОРРЕЛЯЦИЯ области в аккаунтной ветви.
	//
	// Два предыдущих отрицания её не держат: первое снимает субъект, второе метку,
	// и оба остаются красными при `b.scope_id = m.parent_account_id`, замененном на
	// истину. Здесь выполнено всё, кроме принадлежности аккаунту: тот же субъект,
	// тот же глагол, та же метка — и другой аккаунт.
	//
	// У форм A–BCD аккаунтной области нет вовсе: их выдача материализуется на
	// объекты, и «чужой объект» у них отказывает отсутствием кортежа — спрашивать
	// у них тут нечего, что и делает эту пробу пробой ФОРМЫ E, а не сравнением.
	ok, _, err = st.Check(ctx, sc.Subjects[0], "v_get", sc.ForeignObject())
	require.NoError(t, err)
	require.False(t, ok, "аккаунтная выдача накрыла помеченный объект проекта ЧУЖОГО аккаунта — "+
		"область не сужает ничего, и выдача одного арендатора действует у соседа")
}

// TestFormERefusesIntentItDoesNotUnderstand — общий словарь не может расходиться
// молча.
//
// Перевод намерения живёт на стороне формы E, и молча проглоченная строка дала бы
// форму, выигравшую запись тем, что она её не сделала. Отрицание идёт в паре с
// положительным: одиночное «неизвестное отношение отвергнуто» зеленело бы и у
// формы, отвергающей всё.
func TestFormERefusesIntentItDoesNotUnderstand(t *testing.T) {
	if testing.Short() {
		t.Skip("поднимает контейнеры")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	cfg := DefaultConfig()
	cfg.Forms = []Form{FormE}
	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)
	sc := NewScenario(4, 2, 2, "editor", DefaultVerbs())

	st, err := r.NewSeededStore(ctx, FormE, sc, false, "vocabulary-E")
	require.NoError(t, err)
	defer func() { _ = st.Teardown(ctx) }()

	// Положительный: словарь, который форма понимает, применяется и МЕНЯЕТ вердикт.
	before, _, err := st.Check(ctx, sc.Subjects[0], "v_get", sc.Object(0))
	require.NoError(t, err)
	require.False(t, before)
	_, err = st.Write(ctx, Grant(FormE, sc))
	require.NoError(t, err)
	after, _, err := st.Check(ctx, sc.Subjects[0], "v_get", sc.Object(0))
	require.NoError(t, err)
	require.True(t, after, "понятное намерение не изменило ни одного вердикта — "+
		"отрицание ниже утверждало бы про форму, которая не делает ничего")

	// Отрицательный: чужое отношение — ошибка, а не пропуск.
	_, err = st.Write(ctx, []Tuple{{User: "user:x", Relation: "no_such_relation", Object: bindingObj}})
	require.Error(t, err, "неизвестное отношение проглочено молча — намерение, которого форма "+
		"не поняла, засчиталось бы ей как выполненная работа")
	require.NotErrorIs(t, err, ErrNotApplicable,
		"расхождение словаря выдано за неприменимость by construction — это разные вещи: "+
			"первое дефект перевода, второе результат замера")
	require.False(t, errors.Is(err, ErrModelNotRequired))
}
