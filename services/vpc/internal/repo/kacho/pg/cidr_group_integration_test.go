// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc/codes"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/cidrgroup"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Интеграционные пробы именованного набора префиксов на НАСТОЯЩЕМ Postgres.
//
// Здесь проверяется ровно то, что не выражается unit-тестом: потолок, который
// держит база; ссылка правила, которую держит внешний ключ; и поведение двух
// писателей, столкнувшихся на одной строке. Всё три — свойства конструкции, а не
// кода: мок их не воспроизводит и воспроизводить не должен.

func newCidrGroup(projectID, name string, v4, v6 []string) *domain.CidrGroup {
	return &domain.CidrGroup{
		ID:           ids.NewHyphenID(ids.PrefixCidrGroupHyphen),
		ProjectID:    projectID,
		Name:         domain.RcNameVPC(name),
		V4CidrBlocks: v4,
		V6CidrBlocks: v6,
	}
}

// seedCidrGroup создаёт набор одной writer-TX и возвращает его запись.
func seedCidrGroup(ctx context.Context, t *testing.T, r *kachopg.Repository, g *domain.CidrGroup) *kacho.CidrGroupRecord {
	t.Helper()
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	rec, err := w.CidrGroups().Insert(ctx, g)
	require.NoError(t, err)
	require.NoError(t, w.Commit())
	return rec
}

// seedNetworkAndSG создаёт сеть и группу правил, чьи правила ссылаются на
// набор. Ссылку в проекцию кладёт ТРИГГЕР — тест её руками не пишет, иначе он
// проверял бы собственную вставку вместо механизма.
func seedNetworkAndSG(ctx context.Context, t *testing.T, r *kachopg.Repository, projectID, cidrGroupID string, rules int) *kacho.SecurityGroupRecord {
	t.Helper()
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	net := newNetwork(projectID, "net-for-sg")
	_, err = w.Networks().Insert(ctx, net)
	require.NoError(t, err)

	sg := &domain.SecurityGroup{
		ID:        ids.NewID(ids.PrefixSecurityGroup),
		ProjectID: projectID,
		NetworkID: net.ID,
		Name:      domain.RcNameVPC("sg-with-ref"),
	}
	for i := 0; i < rules; i++ {
		sg.Rules = append(sg.Rules, domain.SecurityGroupRule{
			ID:             ids.NewID(ids.PrefixSecurityGroup),
			Direction:      domain.SecurityGroupRuleDirectionIngress,
			FromPort:       domain.AnyPort,
			ToPort:         domain.AnyPort,
			ProtocolNumber: domain.AnyProtocolNumber,
			CidrGroupID:    cidrGroupID,
		})
	}
	rec, err := w.SecurityGroups().Insert(ctx, sg)
	require.NoError(t, err)
	require.NoError(t, w.Commit())
	return rec
}

// TestCidrGroup_DeleteRefusedWhileReferenced — набор с живой ссылкой не
// удаляется, а набор без ссылок удаляется.
//
// Отрицание стоит В ПАРЕ с положительным контролем: без него «удаление
// отвергнуто» было бы неотличимо от «удаление не работает вовсе».
func TestCidrGroup_DeleteRefusedWhileReferenced(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)

	referenced := seedCidrGroup(ctx, t, r,
		newCidrGroup("prj-1", "referenced", []string{"203.0.113.0/24"}, nil))
	free := seedCidrGroup(ctx, t, r,
		newCidrGroup("prj-1", "free", []string{"198.51.100.0/24"}, nil))

	sg := seedNetworkAndSG(ctx, t, r, "prj-1", referenced.ID, 2)

	// Отрицание: набор держит правило — удаление отвергнуто состоянием.
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	err = w.CidrGroups().Delete(ctx, referenced.ID)
	w.Abort()
	require.Error(t, err, "набор с живой ссылкой удалён — внешний ключ не держит")
	assert.ErrorIs(t, err, repo.ErrFailedPrecondition,
		"отказ обязан быть по СОСТОЯНИЮ, а не внутренней ошибкой базы")

	// Положительный контроль: набор без ссылок удаляется тем же кодом.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	require.NoError(t, w2.CidrGroups().Delete(ctx, free.ID),
		"набор без ссылок не удалился — отрицание выше ничего не доказывает")
	require.NoError(t, w2.Commit())

	// И проекция ссылок ведёт себя как проекция: она называет ГРУППУ и число её
	// правил, а не идентификаторы правил.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.CidrGroups().Get(ctx, referenced.ID)
	require.NoError(t, err)
	require.Len(t, got.UsedBy, 1)
	assert.Equal(t, sg.ID, got.UsedBy[0].SecurityGroupID)
	assert.Equal(t, 2, got.UsedBy[0].Rules)
}

// TestCidrGroup_ReferenceReleasedWithTheRule — снятие правила освобождает набор.
//
// Без этой пробы предыдущая доказывала бы только «удалить нельзя никогда»:
// проекция, которая не убирается вместе с правилом, делала бы набор вечным.
func TestCidrGroup_ReferenceReleasedWithTheRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)

	group := seedCidrGroup(ctx, t, r,
		newCidrGroup("prj-1", "grp", []string{"203.0.113.0/24"}, nil))
	sg := seedNetworkAndSG(ctx, t, r, "prj-1", group.ID, 1)

	// Снимаем единственное правило — набор обязан освободиться.
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.SecurityGroups().UpdateRules(ctx, sg.ID, []string{sg.Rules[0].ID}, nil)
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	require.NoError(t, w2.CidrGroups().Delete(ctx, group.ID),
		"набор остался занят после снятия правила — проекция ссылок не убирается вместе с ним")
	require.NoError(t, w2.Commit())
}

// TestCidrGroup_ConcurrentAddCannotExceedTheCap — два писателя, добавляющих
// блоки одновременно, не переступают потолок и не теряют записей друг друга.
//
// Проба нацелена на КОНСТРУКЦИЮ, а не на код: потолок держит условный инкремент
// счётчика под блокировкой строки. Программная проверка «прочитал 62 → вставил»
// прошла бы у обоих писателей — они читают одно и то же число.
func TestCidrGroup_ConcurrentAddCannotExceedTheCap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)

	// Набор заполнен до предела минус два, и оба писателя просят по два блока:
	// пройти обязан ровно один.
	seed := make([]string, 0, domain.MaxCidrGroupBlocks-2)
	for i := 0; i < domain.MaxCidrGroupBlocks-2; i++ {
		seed = append(seed, fmt.Sprintf("10.%d.0.0/24", i))
	}
	group := seedCidrGroup(ctx, t, r, newCidrGroup("prj-1", "brim", seed, nil))

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
		refused  int
	)
	add := func(blocks []string) {
		defer wg.Done()
		w, werr := r.Writer(ctx)
		if werr != nil {
			return
		}
		defer w.Abort()
		if _, aerr := w.CidrGroups().AddBlocks(ctx, group.ID, blocks, nil); aerr != nil {
			mu.Lock()
			refused++
			mu.Unlock()
			return
		}
		if cerr := w.Commit(); cerr != nil {
			mu.Lock()
			refused++
			mu.Unlock()
			return
		}
		mu.Lock()
		accepted++
		mu.Unlock()
	}
	wg.Add(2)
	go add([]string{"192.0.2.0/25", "192.0.2.128/25"})
	go add([]string{"198.51.100.0/25", "198.51.100.128/25"})
	wg.Wait()

	assert.Equal(t, 1, accepted, "потолок пропустил обоих писателей — сериализации нет")
	assert.Equal(t, 1, refused, "второй писатель не получил отказа")

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.CidrGroups().Get(ctx, group.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.MaxCidrGroupBlocks, len(got.V4CidrBlocks),
		"состав разошёлся с потолком: одна из транзакций записала мимо предиката")
}

// TestCidrGroup_AddIsIdempotentAndDoesNotEatTheCap — повтор добавления того же
// члена не меняет состав и НЕ расходует потолок.
//
// Второе — не мелочь: счётчик инкрементируется до вставки, поэтому без
// приведения его к фактическому составу идемпотентный повтор «съедал» бы предел,
// ничего не добавив, и набор упирался бы в потолок при половине состава.
func TestCidrGroup_AddIsIdempotentAndDoesNotEatTheCap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)

	group := seedCidrGroup(ctx, t, r, newCidrGroup("prj-1", "idem", []string{"203.0.113.0/24"}, nil))

	for i := 0; i < 5; i++ {
		w, werr := r.Writer(ctx)
		require.NoError(t, werr)
		rec, aerr := w.CidrGroups().AddBlocks(ctx, group.ID, []string{"203.0.113.0/24"}, nil)
		require.NoError(t, aerr, "повторное добавление присутствующего блока обязано быть успехом")
		require.NoError(t, w.Commit())
		assert.Equal(t, []string{"203.0.113.0/24"}, rec.V4CidrBlocks)
		assert.Equal(t, 1, rec.CidrGroupBlockCount())
	}

	// Потолок по-прежнему доступен целиком: пять повторов ничего не израсходовали.
	rest := make([]string, 0, domain.MaxCidrGroupBlocks-1)
	for i := 0; i < domain.MaxCidrGroupBlocks-1; i++ {
		rest = append(rest, fmt.Sprintf("10.%d.0.0/24", i))
	}
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	rec, err := w.CidrGroups().AddBlocks(ctx, group.ID, rest, nil)
	require.NoError(t, err, "потолок был израсходован идемпотентными повторами")
	require.NoError(t, w.Commit())
	assert.Equal(t, domain.MaxCidrGroupBlocks, len(rec.V4CidrBlocks))
}

// TestCidrGroup_CapIsPerFamilyAndRefusalNamesTheNumbers — потолок считается ПО
// СЕМЕЙСТВАМ, а отказ называет текущий размер, запрошенное и предел.
func TestCidrGroup_CapIsPerFamilyAndRefusalNamesTheNumbers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)

	full := make([]string, 0, domain.MaxCidrGroupBlocks)
	for i := 0; i < domain.MaxCidrGroupBlocks; i++ {
		full = append(full, fmt.Sprintf("10.%d.0.0/24", i))
	}
	group := seedCidrGroup(ctx, t, r, newCidrGroup("prj-1", "full-v4", full, nil))

	// Положительный контроль: второе семейство потолком первого не связано.
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	rec, err := w.CidrGroups().AddBlocks(ctx, group.ID, nil, []string{"2001:db8::/32"})
	require.NoError(t, err, "потолок посчитан по обоим семействам сразу")
	require.NoError(t, w.Commit())
	assert.Equal(t, 1, len(rec.V6CidrBlocks))

	// Отрицание: своё семейство переполнить нельзя, и отказ называет числа.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.CidrGroups().AddBlocks(ctx, group.ID, []string{"192.0.2.0/24"}, nil)
	w2.Abort()
	require.Error(t, err)
	assert.ErrorIs(t, err, repo.ErrFailedPrecondition)
	assert.Contains(t, err.Error(), fmt.Sprintf("v4: %d present, 1 requested", domain.MaxCidrGroupBlocks))
	assert.Contains(t, err.Error(), fmt.Sprintf("limit %d per family", domain.MaxCidrGroupBlocks))
}

// TestCidrGroup_NameUniquePerProject — имя уникально в проекте, пустое имя
// дублей не образует.
func TestCidrGroup_NameUniquePerProject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)

	seedCidrGroup(ctx, t, r, newCidrGroup("prj-1", "office", nil, nil))

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.CidrGroups().Insert(ctx, newCidrGroup("prj-1", "office", nil, nil))
	w.Abort()
	require.Error(t, err, "имя занято в проекте, а вставка прошла")
	assert.ErrorIs(t, err, repo.ErrAlreadyExists)

	// Положительный контроль №1: то же имя в ДРУГОМ проекте законно.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.CidrGroups().Insert(ctx, newCidrGroup("prj-2", "office", nil, nil))
	require.NoError(t, err, "уникальность оказалась глобальной, а не проектной")
	require.NoError(t, w2.Commit())

	// Здесь стоял положительный контроль «два безымянных набора в одном проекте
	// законны». Его предмета больше нет: миграция 715001 сняла частичный индекс
	// `WHERE name <> ''` вместе с самой возможностью пустого имени, потому что
	// форма имени (#715) пустую строку не принимает. Контроль, переживший свой
	// предмет, утверждал бы о дереве неправду — поэтому он заменён на проверку
	// того, что теперь верно: безымянный набор не заводится вовсе.
	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w3.Abort()
	_, err = w3.CidrGroups().Insert(ctx, newCidrGroup("prj-1", "", nil, nil))
	w3.Abort()
	require.Error(t, err, "пустое имя больше не является допустимым значением")

	// Полоса сменилась с ErrInvalidArg на ErrInternal осознанно (задача о двух
	// смыслах нарушения формы). Проба бьёт writer'ом МИМО use-case, то есть
	// воспроизводит ровно тот случай, ради которого ограничение таблицы и стоит:
	// «сервис пропустил негодное имя». Настоящий вызывающий этого пути не
	// проходит — его имя судит доменный newtype до вставки, — поэтому обвинять
	// его INVALID_ARGUMENT значит утверждать неправду, да ещё и предлагать
	// исправить то, чего он не присылал.
	assert.ErrorIs(t, err, repo.ErrInternal, "форма имени — дефект сервиса, а не ввод вызывающего")
	assert.NotErrorIs(t, err, repo.ErrInvalidArg, "дефект сервиса не обвиняет вызывающего: %v", err)
}

// TestCidrGroup_EmptyingAReferencedSetIsRefused — снять ПОСЛЕДНИЙ префикс
// набора, на который ссылается живое правило, нельзя.
//
// Проба закрывает обход отказа удаления с другой стороны. Пустой набор в ссылке
// правила либо заставляет правило выпасть целиком (разрешение молча сужается),
// либо заставляет фильтр исчезнуть из него (молча расширяется) — оба исхода суть
// защита с формой и без содержания. Без этой пробы запрет на удаление выглядел бы
// исполненным, а обойти его можно было бы опустошением.
func TestCidrGroup_EmptyingAReferencedSetIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)

	group := seedCidrGroup(ctx, t, r,
		newCidrGroup("prj-1", "held", []string{"203.0.113.0/24", "198.51.100.0/24"}, nil))
	seedNetworkAndSG(ctx, t, r, "prj-1", group.ID, 1)

	uc := cidrgroup.NewRemoveCidrBlocksUseCase(r, repomock.NewOpsRepo())

	// Сузить набор, оставив хотя бы один префикс, — законно (положительный
	// контроль: иначе отрицание ниже зеленело бы на «сужение сломано вовсе»).
	op, err := uc.Execute(ctx, group.ID, []string{"198.51.100.0/24"}, nil)
	require.NoError(t, err)
	require.Nil(t, op.Error, "частичное сужение отвергнуто: %v", op.Error)

	// Снять ПОСЛЕДНИЙ префикс — отказ по состоянию, называющий держателей.
	op2, err := uc.Execute(ctx, group.ID, []string{"203.0.113.0/24"}, nil)
	require.NoError(t, err)
	require.NotNil(t, op2.Error, "последний префикс набора с живой ссылкой снят — запрет обходится опустошением")
	assert.Equal(t, int32(codes.FailedPrecondition), op2.Error.Code)
	assert.Contains(t, op2.Error.Message, "security groups: 1")

	// И состав действительно не изменился — отказ не «залогирован», а применён.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.CidrGroups().Get(ctx, group.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"203.0.113.0/24"}, got.V4CidrBlocks)
}
