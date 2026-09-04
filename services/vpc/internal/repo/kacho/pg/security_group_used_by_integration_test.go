// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgxpool"

	referencev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/reference"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/securitygroup"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	// blank-import регистрирует трансферы repo-запись → proto.
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Обратная ссылка группы правил — «кем используется» — читается ЗАПРОСОМ по тем
// отношениям, которые база уже держит: набор групп на интерфейсе
// (`network_interfaces.security_group_ids`) и группа по умолчанию у сети
// (`networks.default_security_group_id`). Своей таблицы у этой ссылки нет и не
// заводится: отношение уже выражено, и вторая его копия разошлась бы с первой.
//
// Пробы ниже утверждают ЧЕТЫРЕ разных свойства, и каждое — обеими половинами:
//
//   1. значение появляется при живой ссылке и ИСЧЕЗАЕТ при снятой. Одна половина
//      без другой зеленеет на коде, который всегда возвращает одно и то же;
//   2. потребитель из ЧУЖОГО проекта не виден, и ответ на группу с чужим
//      потребителем побайтово равен ответу на группу без потребителей вовсе —
//      иначе по различию ответов чужой ресурс опознаётся, то есть скрытие не
//      скрывает. Рядом стоит положительный контроль: свой потребитель виден, —
//      без него «пусто» означало бы и «граница держит», и «производителя нет»;
//   3. ответ ОГРАНИЧЕН: сколько бы интерфейсов ни держало группу, наружу уезжает
//      не больше предела плюс одна строка, и эта лишняя строка — признак «есть
//      ещё». Признак проверяется в обе стороны: ровно на пределе его нет;
//   4. предикат обслуживается ИНДЕКСОМ (проба дешевизны) — без него обратная
//      ссылка становится полным перебором интерфейсов проекта на каждое чтение
//      карточки.
//
// Строки-потребители кладутся ПРЯМЫМ SQL, а не через use-case: чужепроектную
// ссылку use-case отвергает на пути запроса (`validateSGTargetCidrGroup` и
// соседи), поэтому через него состояние, которое проверяет проба №2, не
// построить вовсе. Ровно поэтому проба и нужна: она утверждает, что граница
// держится ЧТЕНИЕМ, а не только записью.

// sgUsedByEnv — минимальное дерево ресурсов под пробы обратной ссылки.
type sgUsedByEnv struct {
	pool      *pgxpool.Pool
	repo      *kachopg.Repository
	projectID string
	otherPrj  string
	networkID string
	subnetID  string
	// otherSubnetID — подсеть ЧУЖОГО проекта, в которой живёт чужой интерфейс.
	otherSubnetID string
	sgID          string
}

func newSGUsedByEnv(ctx context.Context, t *testing.T) *sgUsedByEnv {
	t.Helper()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	e := &sgUsedByEnv{
		pool:      pool,
		repo:      kachopg.New(pool, nil),
		projectID: "prj-sg-usedby-own",
		otherPrj:  "prj-sg-usedby-foreign",
		networkID: ids.NewID(ids.PrefixNetwork),
		subnetID:  ids.NewID(ids.PrefixSubnet),
		sgID:      ids.NewID(ids.PrefixSecurityGroup),
	}
	e.otherSubnetID = ids.NewID(ids.PrefixSubnet)
	otherNet := ids.NewID(ids.PrefixNetwork)

	mustExec(ctx, t, pool,
		`INSERT INTO networks(id, project_id, name, description, labels) VALUES ($1,$2,$3,'','{}'::jsonb)`,
		e.networkID, e.projectID, "net-own")
	mustExec(ctx, t, pool,
		`INSERT INTO networks(id, project_id, name, description, labels) VALUES ($1,$2,$3,'','{}'::jsonb)`,
		otherNet, e.otherPrj, "net-foreign")
	mustExec(ctx, t, pool,
		`INSERT INTO subnets(id, project_id, network_id, zone_id, placement_type, name, description, labels, v4_cidr_blocks, v6_cidr_blocks)
		 VALUES ($1,$2,$3,'zone-a','ZONAL',$4,'','{}'::jsonb, ARRAY['10.10.0.0/16']::text[], ARRAY[]::text[])`,
		e.subnetID, e.projectID, e.networkID, "sn-own")
	mustExec(ctx, t, pool,
		`INSERT INTO subnets(id, project_id, network_id, zone_id, placement_type, name, description, labels, v4_cidr_blocks, v6_cidr_blocks)
		 VALUES ($1,$2,$3,'zone-a','ZONAL',$4,'','{}'::jsonb, ARRAY['10.20.0.0/16']::text[], ARRAY[]::text[])`,
		e.otherSubnetID, e.otherPrj, otherNet, "sn-foreign")
	mustExec(ctx, t, pool,
		`INSERT INTO security_groups(id, project_id, network_id, name, description, labels) VALUES ($1,$2,$3,$4,'','{}'::jsonb)`,
		e.sgID, e.projectID, e.networkID, "sg-under-test")
	return e
}

func mustExec(ctx context.Context, t *testing.T, pool *pgxpool.Pool, q string, args ...any) {
	t.Helper()
	_, err := pool.Exec(ctx, q, args...)
	require.NoError(t, err)
}

// addNIC вставляет интерфейс, чей набор групп содержит перечисленные id.
func (e *sgUsedByEnv) addNIC(ctx context.Context, t *testing.T, projectID, subnetID, name string, sgIDs ...string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixNetworkInterface)
	arr := "[]"
	if len(sgIDs) > 0 {
		arr = `["` + strings.Join(sgIDs, `","`) + `"]`
	}
	mustExec(ctx, t, e.pool,
		`INSERT INTO network_interfaces(id, project_id, name, description, labels, subnet_id, security_group_ids, mac_address, status)
		 VALUES ($1,$2,$3,'','{}'::jsonb,$4,$5::jsonb,$6,'AVAILABLE')`,
		id, projectID, name, subnetID, arr, macFor(id))
	return id
}

// macFor — детерминированный MAC из идентификатора: колонка несёт cloud-wide
// UNIQUE, поэтому константа рассыпала бы вторую вставку.
func macFor(id string) string {
	h := uint64(1469598103934665603)
	for i := 0; i < len(id); i++ {
		h ^= uint64(id[i])
		h *= 1099511628211
	}
	return fmt.Sprintf("0e:%02x:%02x:%02x:%02x:%02x",
		byte(h), byte(h>>8), byte(h>>16), byte(h>>24), byte(h>>32))
}

// readSGProto читает группу ТЕМ ЖЕ путём, которым её читает вызывающий:
// use-case чтения → перевод записи в proto. Не репозиторием напрямую — иначе
// проба утверждала бы про метод, а не про то, что видит арендатор, и молчаливо
// пережила бы снятие вызова из use-case'а.
func (e *sgUsedByEnv) readSGProto(ctx context.Context, t *testing.T) *vpcv1.SecurityGroup {
	t.Helper()
	rec, err := securitygroup.NewGetSecurityGroupUseCase(e.repo).Execute(ctx, e.sgID)
	require.NoError(t, err)
	var pb *vpcv1.SecurityGroup
	require.NoError(t, dto.Transfer(dto.FromTo(*rec, &pb)))
	require.NotNil(t, pb)
	return pb
}

// TestSecurityGroupUsedBy_AppearsWithLiveReferenceAndVanishesWhenReleased —
// обе половины на одной группе: интерфейс берёт группу — потребитель появляется;
// интерфейс её отпускает — потребитель исчезает. Между половинами не меняется
// ничего, кроме самой ссылки.
func TestSecurityGroupUsedBy_AppearsWithLiveReferenceAndVanishesWhenReleased(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	e := newSGUsedByEnv(ctx, t)

	// Контроль исходного состояния: до ссылки потребителей нет. Без него
	// «появился» было бы неотличимо от «был всегда».
	require.Empty(t, e.readSGProto(ctx, t).GetUsedBy(), "до ссылки потребителей быть не должно")

	nicID := e.addNIC(ctx, t, e.projectID, e.subnetID, "nic-holder", e.sgID)

	got := e.readSGProto(ctx, t).GetUsedBy()
	require.Len(t, got, 1, "живая ссылка обязана дать ровно одного потребителя")
	assert.Equal(t, "network_interface", got[0].GetReferrer().GetType())
	assert.Equal(t, nicID, got[0].GetReferrer().GetId())
	assert.Equal(t, "nic-holder", got[0].GetReferrer().GetName())
	assert.Equal(t, referencev1.Reference_USED_BY, got[0].GetType())

	// Снятие ссылки — вторая половина. Интерфейс остаётся на месте: исчезнуть
	// обязан именно потребитель, а не строка.
	mustExec(ctx, t, e.pool,
		`UPDATE network_interfaces SET security_group_ids = '[]'::jsonb WHERE id = $1`, nicID)
	assert.Empty(t, e.readSGProto(ctx, t).GetUsedBy(), "снятая ссылка обязана убрать потребителя")
}

// TestSecurityGroupUsedBy_NetworkDefaultIsAConsumer — вторая полоса потребителей:
// сеть, у которой эта группа объявлена группой по умолчанию. Полоса ограничена
// по построению (у сети одна такая группа), поэтому проверяется точным
// равенством, а не наличием.
func TestSecurityGroupUsedBy_NetworkDefaultIsAConsumer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	e := newSGUsedByEnv(ctx, t)

	require.Empty(t, e.readSGProto(ctx, t).GetUsedBy())

	mustExec(ctx, t, e.pool,
		`UPDATE networks SET default_security_group_id = $1 WHERE id = $2`, e.sgID, e.networkID)

	got := e.readSGProto(ctx, t).GetUsedBy()
	require.Len(t, got, 1)
	assert.Equal(t, "network", got[0].GetReferrer().GetType())
	assert.Equal(t, e.networkID, got[0].GetReferrer().GetId())
	assert.Equal(t, "net-own", got[0].GetReferrer().GetName())

	// Снятие — в NULL, а не в пустую строку: колонка стала NULL-able миграцией
	// 0005, и внешний ключ не пропустил бы '' (группы с таким id не бывает).
	mustExec(ctx, t, e.pool,
		`UPDATE networks SET default_security_group_id = NULL WHERE id = $1`, e.networkID)
	assert.Empty(t, e.readSGProto(ctx, t).GetUsedBy(), "снятая группа по умолчанию обязана убрать потребителя")
}

// TestSecurityGroupUsedBy_ForeignProjectConsumerIsIndistinguishableFromNone —
// граница проекта. Чужой интерфейс, держащий группу, не показывается, и ответ
// обязан быть НЕОТЛИЧИМ от ответа на группу без потребителей: различие в ответе
// и есть оракул существования.
func TestSecurityGroupUsedBy_ForeignProjectConsumerIsIndistinguishableFromNone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	e := newSGUsedByEnv(ctx, t)

	// Снимок ответа, когда потребителей нет ВООБЩЕ — эталон неотличимости.
	none := e.readSGProto(ctx, t)

	// Чужой проект берёт группу. Такую строку не создать через use-case — он
	// отвергает чужепроектную ссылку на пути запроса; поэтому она кладётся
	// прямым SQL, и проба утверждает, что ЧТЕНИЕ тоже её не выдаёт.
	e.addNIC(ctx, t, e.otherPrj, e.otherSubnetID, "nic-foreign", e.sgID)
	// И сеть чужого проекта объявляет её группой по умолчанию — вторая полоса
	// обязана держать границу той же проверкой, а не «одна из двух».
	mustExec(ctx, t, e.pool,
		`UPDATE networks SET default_security_group_id = $1 WHERE project_id = $2`, e.sgID, e.otherPrj)

	withForeign := e.readSGProto(ctx, t)
	assert.Empty(t, withForeign.GetUsedBy(), "чужой потребитель не показывается")
	assert.Equal(t, none.String(), withForeign.String(),
		"ответ с чужим потребителем обязан быть побайтово равен ответу без потребителей")

	// Положительный контроль: тот же механизм на СВОЁМ потребителе отвечает.
	// Без него «пусто» означало бы и «граница держит», и «производителя нет».
	e.addNIC(ctx, t, e.projectID, e.subnetID, "nic-own", e.sgID)
	own := e.readSGProto(ctx, t).GetUsedBy()
	require.Len(t, own, 1, "свой потребитель обязан быть виден")
	assert.Equal(t, "nic-own", own[0].GetReferrer().GetName())
}

// TestSecurityGroupUsedBy_AnswerIsBoundedAndSignalsMore — потолок ответа.
// Ровно на пределе признака «есть ещё» нет; на один больше — есть. Обе стороны
// обязательны: проба только на переполнение зеленела бы на коде, который метит
// «есть ещё» всегда.
func TestSecurityGroupUsedBy_AnswerIsBoundedAndSignalsMore(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	e := newSGUsedByEnv(ctx, t)

	limit := kachorepo.SecurityGroupUsedByLimit
	for i := 0; i < limit; i++ {
		e.addNIC(ctx, t, e.projectID, e.subnetID, fmt.Sprintf("nic-%03d", i), e.sgID)
	}
	atLimit := e.readSGProto(ctx, t).GetUsedBy()
	assert.Len(t, atLimit, limit, "ровно на пределе ответ полон и признака «есть ещё» нет")

	e.addNIC(ctx, t, e.projectID, e.subnetID, "nic-overflow", e.sgID)
	over := e.readSGProto(ctx, t).GetUsedBy()
	assert.Len(t, over, limit+1,
		"за пределом ответ несёт ровно одну лишнюю запись — она и есть признак «есть ещё»")

	// И ещё десять сверх — длина ответа не растёт: запрос ограничен, а не
	// «обычно небольшой».
	for i := 0; i < 10; i++ {
		e.addNIC(ctx, t, e.projectID, e.subnetID, fmt.Sprintf("nic-extra-%03d", i), e.sgID)
	}
	assert.Len(t, e.readSGProto(ctx, t).GetUsedBy(), limit+1,
		"ответ обязан оставаться ограниченным независимо от числа потребителей")
}

// TestSecurityGroupUsedBy_PredicateIsServedByAnIndex — проба дешевизны.
//
// Утверждается не время, а ПЛАН: интерфейсы отбираются индексом по набору групп,
// а не последовательным чтением таблицы. Время ничего не доказывает — на
// маленькой таблице перебор действительно дешевле, и это верное поведение
// планировщика, а не дефект.
//
// Проба несёт проверку СВОЕЙ предпосылки (индексы существуют) и контроль в обе
// стороны (предикат без индекса обязан дать перебор) — иначе «в плане есть имя
// индекса» означало бы лишь, что EXPLAIN что-то напечатал.
func TestSecurityGroupUsedBy_PredicateIsServedByAnIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	e := newSGUsedByEnv(ctx, t)

	// Предпосылка: индексы, ради которых заведена миграция, в базе есть. Без неё
	// «плана без имени индекса» не отличить от «индекс не создан вовсе».
	for _, idx := range []string{
		"network_interfaces_security_group_ids_gin",
		"networks_default_security_group_idx",
	} {
		var n int
		require.NoError(t, e.pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_indexes WHERE schemaname='kacho_vpc' AND indexname=$1`, idx).Scan(&n))
		require.Equal(t, 1, n, "индекс %s обязан быть создан миграцией", idx)
	}

	// Данных должно быть достаточно, чтобы перебор перестал быть дешевле индекса.
	// Вставка одним стейтментом: 5000 отдельных вставок стоили бы дороже самой
	// пробы, а её предмет — план, а не скорость вставки.
	mustExec(ctx, t, e.pool, `
		INSERT INTO network_interfaces(id, project_id, name, description, labels, subnet_id, security_group_ids, mac_address, status)
		SELECT 'nicbulk' || lpad(g::text, 13, '0'), $1, 'nb' || g, '', '{}'::jsonb, $2, '[]'::jsonb,
		       '0e:' || lpad(to_hex(g/16777216%256),2,'0') || ':' || lpad(to_hex(g/65536%256),2,'0') || ':' ||
		       lpad(to_hex(g/256%256),2,'0') || ':' || lpad(to_hex(g%256),2,'0') || ':01',
		       'AVAILABLE'
		  FROM generate_series(1, 5000) g`, e.projectID, e.subnetID)
	e.addNIC(ctx, t, e.projectID, e.subnetID, "nic-indexed", e.sgID)

	// То же и для сетей: на таблице из двух строк перебор ДЕЙСТВИТЕЛЬНО дешевле
	// индекса, и требовать от планировщика обратного значило бы требовать
	// неверного решения. Сети заводятся со СВОЕЙ группой по умолчанию у каждой —
	// то есть индекс наполняется так же, как в живом развёртывании, а не остаётся
	// почти пустым, отчего выбор в его пользу был бы предрешён формой фикстуры.
	mustExec(ctx, t, e.pool, `
		INSERT INTO networks(id, project_id, name, description, labels)
		SELECT 'netbulk' || lpad(g::text, 13, '0'), $1, 'nw' || g, '', '{}'::jsonb
		  FROM generate_series(1, 3000) g`, e.projectID)
	mustExec(ctx, t, e.pool, `
		INSERT INTO security_groups(id, project_id, network_id, name, description, labels)
		SELECT 'sgrbulk' || lpad(g::text, 13, '0'), $1, 'netbulk' || lpad(g::text, 13, '0'), 'sg' || g, '', '{}'::jsonb
		  FROM generate_series(1, 3000) g`, e.projectID)
	mustExec(ctx, t, e.pool, `
		UPDATE networks
		   SET default_security_group_id = replace(id, 'netbulk', 'sgrbulk')
		 WHERE id LIKE 'netbulk%'`)

	mustExec(ctx, t, e.pool, `ANALYZE kacho_vpc.network_interfaces`)
	mustExec(ctx, t, e.pool, `ANALYZE kacho_vpc.networks`)
	mustExec(ctx, t, e.pool, `ANALYZE kacho_vpc.security_groups`)

	// Объясняется ТОТ ЖЕ текст запроса, который исполняет репозиторий, — иначе
	// проба утверждала бы про план другого запроса.
	plan := explain(ctx, t, e.pool, kachopg.SecurityGroupReferrersSQL,
		[]string{e.sgID}, kachorepo.SecurityGroupUsedByFetch)
	assert.Contains(t, plan, "network_interfaces_security_group_ids_gin",
		"предикат «набор групп содержит эту группу» обязан обслуживаться индексом; план:\n"+plan)
	assert.Contains(t, plan, "networks_default_security_group_idx",
		"предикат «сеть держит эту группу по умолчанию» обязан обслуживаться индексом; план:\n"+plan)
	assert.NotContains(t, plan, "Seq Scan on network_interfaces",
		"последовательное чтение интерфейсов на каждое чтение карточки — это и есть неограниченный запрос; план:\n"+plan)

	// Контроль в обратную сторону на ТЕХ ЖЕ данных: предикат, под который индекса
	// нет, обязан дать перебор. Без него «в плане есть Index Scan» могло бы
	// означать, что планировщик на этой таблице иначе и не умеет.
	unindexed := explain(ctx, t, e.pool,
		`SELECT id FROM network_interfaces WHERE description = $1`, "нет такого")
	assert.Contains(t, unindexed, "Seq Scan on network_interfaces",
		"проба обязана уметь показать перебор, иначе она не различает планы; план:\n"+unindexed)
}

// explain возвращает текстовый план запроса.
func explain(ctx context.Context, t *testing.T, pool *pgxpool.Pool, q string, args ...any) string {
	t.Helper()
	rows, err := pool.Query(ctx, "EXPLAIN "+q, args...)
	require.NoError(t, err)
	defer rows.Close()
	var sb strings.Builder
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, sb.String(), "пустой план — проба не прочитала ничего")
	return sb.String()
}
