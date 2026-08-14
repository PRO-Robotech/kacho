// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/safeconv"
	"github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// cidrGroupCols / scanCidrGroup живут ЗДЕСЬ, а не в `repo/helpers`, где стоят
// колоночные списки остальных ресурсов.
//
// Причина названа прямо, чтобы её не приняли за небрежность: у именованного
// набора состав лежит в ДОЧЕРНЕЙ таблице, поэтому одной строкой родителя запись
// не собирается — состав дочитывается вторым запросом (пакетно на страницу), и
// «сканирование строки» перестаёт быть самодостаточной операцией. Держать
// половину сборки в общем пакете, а половину здесь значило бы разложить один
// предмет по двум местам. Единственный потребитель — этот файл.
const cidrGroupCols = `id, project_id, created_at, name, description, labels, v4_count, v6_count`

// scanCidrGroup — row-scanner родительской строки набора. Состав НЕ заполняет:
// его дочитывает вызывающий (loadBlocks / loadBlocksForGroups).
func scanCidrGroup(row helpers.Scannable) (*kacho.CidrGroupRecord, error) {
	var (
		rec         kacho.CidrGroupRecord
		name        string
		description string
		labelsJSON  []byte
		v4Count     int
		v6Count     int
	)
	if err := row.Scan(
		&rec.ID, &rec.ProjectID, &rec.CreatedAt, &name, &description, &labelsJSON,
		&v4Count, &v6Count,
	); err != nil {
		return nil, err
	}
	rec.Name = domain.RcNameVPC(name)
	rec.Description = domain.RcDescription(description)
	var labels map[string]string
	if err := helpers.UnmarshalJSONB(labelsJSON, &labels, "CidrGroup.labels"); err != nil {
		return nil, err
	}
	rec.Labels = domain.LabelsFromMap(labels)
	// Счётчики в запись НЕ переносятся: наружу уезжает состав, а счётчики — это
	// то, чем база держит потолок. Два источника одного числа разошлись бы молча,
	// поэтому `cidr_block_count` считается по фактическому составу
	// (`domain.CidrGroup.CidrGroupBlockCount`), а счётчики остаются внутри SQL.
	return &rec, nil
}

// cidrGroupReader — Get/List поверх произвольной pgx.Tx (read-only или RW).
type cidrGroupReader struct {
	tx pgx.Tx
}

// Get — well-formed-but-absent → NotFound с "CidrGroup <id> not found".
func (r *cidrGroupReader) Get(ctx context.Context, id string) (*kacho.CidrGroupRecord, error) {
	q := fmt.Sprintf(`SELECT %s FROM cidr_groups WHERE id = $1`, cidrGroupCols)
	rec, err := scanCidrGroup(r.tx.QueryRow(ctx, q, id))
	if err != nil {
		return nil, helpers.WrapPgErr(err, "CidrGroup", id)
	}
	if err := r.loadBlocks(ctx, rec); err != nil {
		return nil, err
	}
	refs, err := r.ReferrersFor(ctx, []string{rec.ID})
	if err != nil {
		return nil, err
	}
	rec.UsedBy = refs[rec.ID]
	return rec, nil
}

// List — cursor-based pagination + filter.Parse.
func (r *cidrGroupReader) List(ctx context.Context, f kacho.CidrGroupFilter, p kacho.Pagination) ([]*kacho.CidrGroupRecord, string, error) {
	pageSize, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}

	args := []any{}
	conditions := []string{}
	argIdx := 1

	if f.ProjectID != "" {
		conditions = append(conditions, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, f.ProjectID)
		argIdx++
	}
	if f.Name != "" {
		conditions = append(conditions, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, f.Name)
		argIdx++
	}
	if f.Filter != "" {
		// Белый список — пригодные КОЛОНКИ этой таблицы. Состав в него не входит:
		// он лежит в дочерней таблице, и равенство по нему выражало бы не тот
		// вопрос, который задаёт вызывающий («набор содержит такой-то префикс» —
		// это оператор вложенности, которого разборщик не знает).
		ast, perr := filter.Parse(f.Filter, []string{"name", "project_id"})
		if perr != nil {
			return nil, "", helpers.InvalidFilterErr(perr)
		}
		if ast != nil {
			frag, fargs := ast.ToSQL(argIdx)
			conditions = append(conditions, frag)
			args = append(args, fargs...)
			argIdx += len(fargs)
		}
	}
	if p.PageToken != "" {
		ts, id, derr := helpers.DecodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", helpers.InvalidPageTokenErr(derr)
		}
		conditions = append(conditions, fmt.Sprintf("(created_at, id) > ($%d, $%d)", argIdx, argIdx+1))
		args = append(args, ts, id)
		argIdx += 2
	}

	var where string
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	q := fmt.Sprintf(`SELECT %s FROM cidr_groups %s ORDER BY created_at ASC, id ASC LIMIT $%d`,
		cidrGroupCols, where, argIdx)
	args = append(args, pageSize+1)

	rows, err := r.tx.Query(ctx, q, args...)
	if err != nil {
		return nil, "", helpers.WrapPgErr(err, "CidrGroup", "")
	}
	defer rows.Close()

	var result []*kacho.CidrGroupRecord
	for rows.Next() {
		rec, serr := scanCidrGroup(rows)
		if serr != nil {
			return nil, "", helpers.WrapPgErr(serr, "CidrGroup", "")
		}
		result = append(result, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, "", helpers.WrapPgErr(err, "CidrGroup", "")
	}

	var nextToken string
	if int64(len(result)) > pageSize {
		last := result[pageSize-1]
		nextToken = helpers.EncodePageToken(last.CreatedAt, last.ID)
		result = result[:pageSize]
	}
	if err := r.loadBlocksForGroups(ctx, result); err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(result))
	for _, rec := range result {
		ids = append(ids, rec.ID)
	}
	refs, err := r.ReferrersFor(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	for _, rec := range result {
		rec.UsedBy = refs[rec.ID]
	}
	return result, nextToken, nil
}

// ReferrersFor — потребители наборов, ОДНИМ запросом на всю страницу.
//
// Считает правила и называет группу: наружу уезжают вид, идентификатор и имя
// ГРУППЫ, но не идентификаторы правил. Число правил координатой не является, а
// перечень чужих идентификаторов — становится ею.
func (r *cidrGroupReader) ReferrersFor(ctx context.Context, groupIDs []string) (map[string][]kacho.CidrGroupReferrer, error) {
	if len(groupIDs) == 0 {
		return map[string][]kacho.CidrGroupReferrer{}, nil
	}
	const q = `
		SELECT ref.cidr_group_id, ref.security_group_id, COALESCE(sg.name, ''), count(*)
		  FROM security_group_rule_cidr_group_refs ref
		  LEFT JOIN security_groups sg ON sg.id = ref.security_group_id
		 WHERE ref.cidr_group_id = ANY($1)
		 GROUP BY ref.cidr_group_id, ref.security_group_id, sg.name
		 ORDER BY ref.cidr_group_id, ref.security_group_id`
	rows, err := r.tx.Query(ctx, q, groupIDs)
	if err != nil {
		return nil, helpers.WrapPgErr(err, "CidrGroup", "")
	}
	defer rows.Close()

	out := make(map[string][]kacho.CidrGroupReferrer, len(groupIDs))
	for rows.Next() {
		var (
			groupID string
			ref     kacho.CidrGroupReferrer
			rules   int64
		)
		if err := rows.Scan(&groupID, &ref.SecurityGroupID, &ref.SecurityGroupName, &rules); err != nil {
			return nil, helpers.WrapPgErr(err, "CidrGroup", "")
		}
		ref.Rules = int(rules)
		out[groupID] = append(out[groupID], ref)
	}
	if err := rows.Err(); err != nil {
		return nil, helpers.WrapPgErr(err, "CidrGroup", "")
	}
	return out, nil
}

// loadBlocks дочитывает состав ОДНОГО набора.
func (r *cidrGroupReader) loadBlocks(ctx context.Context, rec *kacho.CidrGroupRecord) error {
	return r.loadBlocksForGroups(ctx, []*kacho.CidrGroupRecord{rec})
}

// loadBlocksForGroups дочитывает состав страницы наборов ОДНИМ запросом.
//
// Порядок членов детерминирован (`ORDER BY family(block), block`): состав целиком
// уезжает в ответ, и ответ, меняющий порядок между двумя одинаковыми чтениями,
// читался бы как изменение ресурса.
func (r *cidrGroupReader) loadBlocksForGroups(ctx context.Context, recs []*kacho.CidrGroupRecord) error {
	if len(recs) == 0 {
		return nil
	}
	byID := make(map[string]*kacho.CidrGroupRecord, len(recs))
	ids := make([]string, 0, len(recs))
	for _, rec := range recs {
		byID[rec.ID] = rec
		ids = append(ids, rec.ID)
	}
	const q = `
		SELECT group_id, family(block), host(block) || '/' || masklen(block)
		  FROM cidr_group_blocks
		 WHERE group_id = ANY($1)
		 ORDER BY group_id, family(block), block`
	rows, err := r.tx.Query(ctx, q, ids)
	if err != nil {
		return helpers.WrapPgErr(err, "CidrGroup", "")
	}
	defer rows.Close()
	for rows.Next() {
		var (
			groupID string
			fam     int
			block   string
		)
		if err := rows.Scan(&groupID, &fam, &block); err != nil {
			return helpers.WrapPgErr(err, "CidrGroup", "")
		}
		rec, ok := byID[groupID]
		if !ok {
			continue
		}
		if fam == 4 {
			rec.V4CidrBlocks = append(rec.V4CidrBlocks, block)
			continue
		}
		rec.V6CidrBlocks = append(rec.V6CidrBlocks, block)
	}
	return helpers.WrapPgErr(rows.Err(), "CidrGroup", "")
}

// cidrGroupWriter — DML над cidr_groups через writer-TX. Embeds cidrGroupReader,
// поэтому writer видит свои writes.
//
// Writer НЕ emit'ит outbox самостоятельно: после успешного DML caller (use-case)
// вызывает `RepositoryWriter.Outbox().Emit(...)`.
type cidrGroupWriter struct {
	cidrGroupReader
}

// Insert — INSERT родительской строки + вставка начального состава + приведение
// счётчиков к фактическому составу, всё в writer-TX вызывающего.
func (w *cidrGroupWriter) Insert(ctx context.Context, g *domain.CidrGroup) (*kacho.CidrGroupRecord, error) {
	labelsJSON, err := helpers.MarshalJSONB(domain.LabelsToMap(g.Labels), "CidrGroup.labels")
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`
		INSERT INTO cidr_groups (id, project_id, name, description, labels)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING %s`, cidrGroupCols)
	rec, err := scanCidrGroup(w.tx.QueryRow(ctx, q,
		g.ID, g.ProjectID, string(g.Name), string(g.Description), labelsJSON))
	if err != nil {
		return nil, helpers.WrapPgErr(err, "CidrGroup", string(g.Name))
	}
	if len(g.V4CidrBlocks) == 0 && len(g.V6CidrBlocks) == 0 {
		return rec, nil
	}
	return w.AddBlocks(ctx, g.ID, g.V4CidrBlocks, g.V6CidrBlocks)
}

// Update — UPDATE косметических полей. Состава не касается: у набора он
// правится только глаголами.
func (w *cidrGroupWriter) Update(ctx context.Context, g *domain.CidrGroup) (*kacho.CidrGroupRecord, error) {
	labelsJSON, err := helpers.MarshalJSONB(domain.LabelsToMap(g.Labels), "CidrGroup.labels")
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`
		UPDATE cidr_groups SET name = $2, description = $3, labels = $4
		WHERE id = $1
		RETURNING %s`, cidrGroupCols)
	rec, err := scanCidrGroup(w.tx.QueryRow(ctx, q,
		g.ID, string(g.Name), string(g.Description), labelsJSON))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: CidrGroup %s not found", helpers.ErrNotFound, g.ID)
		}
		return nil, helpers.WrapPgErr(err, "CidrGroup", g.ID)
	}
	if err := w.loadBlocks(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// GetForUpdate — Get со строгой блокировкой строки набора.
//
// `FOR UPDATE` выбран НЕ по привычке: только он конфликтует с `FOR KEY SHARE`,
// которую берёт проверка внешнего ключа при вставке ссылки правила на набор.
// Обычный UPDATE взял бы `FOR NO KEY UPDATE`, а она с `FOR KEY SHARE` НЕ
// конфликтует — и правило, создаваемое одновременно с опустошением набора,
// проходило бы мимо обеих проверок.
func (w *cidrGroupWriter) GetForUpdate(ctx context.Context, id string) (*kacho.CidrGroupRecord, error) {
	q := fmt.Sprintf(`SELECT %s FROM cidr_groups WHERE id = $1 FOR UPDATE`, cidrGroupCols)
	rec, err := scanCidrGroup(w.tx.QueryRow(ctx, q, id))
	if err != nil {
		return nil, helpers.WrapPgErr(err, "CidrGroup", id)
	}
	if err := w.loadBlocks(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// AddBlocks — потолок и отсутствие затирания, конструкцией базы.
//
// Порядок несущий:
//  1. УСЛОВНЫЙ инкремент счётчиков: предикат самого UPDATE держит потолок, а
//     блокировка строки сериализует конкурентных писателей ПО ПОСТРОЕНИЮ —
//     второй ждёт коммита первого, читает новое значение и упирается в предикат.
//     Проверка «посчитал → 62 → вставил» на его месте пропускала бы N писателей
//     с одним и тем же прочитанным числом.
//  2. Вставка членов без падения на уже присутствующих — глагол идемпотентен.
//  3. Приведение счётчиков к ФАКТИЧЕСКОМУ составу: без него повтор «съедал» бы
//     потолок, хотя ничего не добавил.
func (w *cidrGroupWriter) AddBlocks(ctx context.Context, id string, v4, v6 []string) (*kacho.CidrGroupRecord, error) {
	addV4 := safeconv.IntToInt32(len(v4))
	addV6 := safeconv.IntToInt32(len(v6))

	var locked string
	err := w.tx.QueryRow(ctx, `
		UPDATE cidr_groups
		   SET v4_count = v4_count + $2, v6_count = v6_count + $3
		 WHERE id = $1
		   AND v4_count + $2 <= $4
		   AND v6_count + $3 <= $4
		RETURNING id`,
		id, addV4, addV6, domain.MaxCidrGroupBlocks).Scan(&locked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Ноль строк — либо набора нет, либо потолок. Различаем повторным
			// чтением, чтобы отказ назвал предмет: «нет набора» и «набор полон» —
			// разные исходы, и вызывающий делает по ним разное.
			cur, gerr := w.Get(ctx, id)
			if gerr != nil {
				return nil, gerr
			}
			return nil, capExceeded(id, cur, len(v4), len(v6))
		}
		return nil, helpers.WrapPgErr(err, "CidrGroup", id)
	}

	if _, err := w.tx.Exec(ctx, `
		INSERT INTO cidr_group_blocks (group_id, block)
		SELECT $1, b FROM unnest($2::cidr[]) AS b
		ON CONFLICT DO NOTHING`, id, append(append([]string{}, v4...), v6...)); err != nil {
		return nil, helpers.WrapPgErr(err, "CidrGroup", id)
	}
	return w.recount(ctx, id)
}

// RemoveBlocks — снятие членов и приведение счётчиков к фактическому составу.
//
// Строка набора берётся `FOR UPDATE` ПЕРВЫМ действием, до удаления: см.
// GetForUpdate — именно эта блокировка не даёт правилу, создаваемому
// одновременно, сослаться на набор, который мы опустошаем.
func (w *cidrGroupWriter) RemoveBlocks(ctx context.Context, id string, v4, v6 []string) (*kacho.CidrGroupRecord, error) {
	if _, err := w.GetForUpdate(ctx, id); err != nil {
		return nil, err
	}
	if _, err := w.tx.Exec(ctx, `
		DELETE FROM cidr_group_blocks
		 WHERE group_id = $1 AND block = ANY($2::cidr[])`,
		id, append(append([]string{}, v4...), v6...)); err != nil {
		return nil, helpers.WrapPgErr(err, "CidrGroup", id)
	}
	return w.recount(ctx, id)
}

// recount приводит счётчики к фактическому составу и возвращает актуальную
// запись. Единственное место, где счётчики становятся производными от состава, —
// поэтому «сколько членов» нельзя ответить двумя разными числами.
func (w *cidrGroupWriter) recount(ctx context.Context, id string) (*kacho.CidrGroupRecord, error) {
	q := fmt.Sprintf(`
		UPDATE cidr_groups
		   SET v4_count = (SELECT count(*) FROM cidr_group_blocks
		                    WHERE group_id = $1 AND family(block) = 4),
		       v6_count = (SELECT count(*) FROM cidr_group_blocks
		                    WHERE group_id = $1 AND family(block) = 6)
		 WHERE id = $1
		RETURNING %s`, cidrGroupCols)
	rec, err := scanCidrGroup(w.tx.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: CidrGroup %s not found", helpers.ErrNotFound, id)
		}
		return nil, helpers.WrapPgErr(err, "CidrGroup", id)
	}
	if err := w.loadBlocks(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// Delete — DELETE cidr_groups WHERE id = $1.
//
// Нарушение внешнего ключа с проекции ссылок правил (RESTRICT) → отказ по
// состоянию. Текст здесь родовой: перечень мешающего по видам и числам собирает
// use-case, у которого есть чем его посчитать. Синхронная проверка отвергает
// раньше, а этот путь — атомарный backstop, отвечающий В МОМЕНТ удаления.
func (w *cidrGroupWriter) Delete(ctx context.Context, id string) error {
	tag, err := w.tx.Exec(ctx, `DELETE FROM cidr_groups WHERE id = $1`, id)
	if err != nil {
		if helpers.IsFKViolation(err) {
			return fmt.Errorf("%w: CidrGroup %s is in use", helpers.ErrFailedPrecondition, id)
		}
		return helpers.WrapPgErr(err, "CidrGroup", id)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: CidrGroup %s not found", helpers.ErrNotFound, id)
	}
	return nil
}

// capExceeded — отказ по потолку, называющий ТЕКУЩИЙ размер, ЗАПРОШЕННОЕ и сам
// предел по каждому семейству, которое его перебирает. Разница двух чисел
// («ещё 2 можно») здесь не годится: вызывающий правит СВОЙ запрос, и ему нужно
// знать, что он прислал и во что упёрся.
func capExceeded(id string, cur *kacho.CidrGroupRecord, addV4, addV6 int) error {
	var over []string
	if len(cur.V4CidrBlocks)+addV4 > domain.MaxCidrGroupBlocks {
		over = append(over, fmt.Sprintf("v4: %d present, %d requested", len(cur.V4CidrBlocks), addV4))
	}
	if len(cur.V6CidrBlocks)+addV6 > domain.MaxCidrGroupBlocks {
		over = append(over, fmt.Sprintf("v6: %d present, %d requested", len(cur.V6CidrBlocks), addV6))
	}
	return fmt.Errorf("%w: CidrGroup %s block limit exceeded (%s, limit %d per family)",
		helpers.ErrFailedPrecondition, id, strings.Join(over, ", "), domain.MaxCidrGroupBlocks)
}
