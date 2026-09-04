// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"fmt"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/option"
	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg/dto"
)

const targetGroupCols = `
    id, project_id, region_id, created_at, updated_at,
    name, description, labels, health_check,
    deregistration_delay_seconds, slow_start_seconds, port, status, xmin::text`

const targetCols = `
    id, target_group_id, created_at, updated_at,
    instance_id, nic_id, ip_ref_subnet_id, ip_ref_address,
    external_ip_address, external_ip_zone_id,
    weight, status, drain_started_at`

type targetGroupReader struct {
	tx pgx.Tx
}

func scanTG(row pgx.Row) (*kacho.TargetGroupRecord, error) {
	var (
		rec       kacho.TargetGroupRecord
		idStr     string
		projIDs   string
		regionIDs string
		nameStr   string
		descStr   string
		statusStr string
		labelsRaw []byte
		hcRaw     []byte
		portVal   int32
		deregSec  int32
		slowSec   int32
	)
	if err := row.Scan(
		&idStr, &projIDs, &regionIDs, &rec.CreatedAt, &rec.UpdatedAt,
		&nameStr, &descStr, &labelsRaw, &hcRaw,
		&deregSec, &slowSec, &portVal, &statusStr, &rec.Xmin,
	); err != nil {
		return nil, err
	}
	// DB stores integer seconds; domain carries Duration (NLB-1c B8).
	rec.DeregistrationDelay = dto.SecondsToDuration(deregSec)
	rec.SlowStart = dto.SecondsToDuration(slowSec)
	rec.Port = domain.LbPort(portVal)
	rec.ID = domain.ResourceID(idStr)
	rec.ProjectID = domain.ProjectID(projIDs)
	rec.RegionID = domain.RegionID(regionIDs)
	rec.Name = domain.LbName(nameStr)
	rec.Description = domain.LbDescription(descStr)
	rec.Status = domain.TargetGroupStatus(statusStr)
	labels, err := dto.LabelsFromJSONB(labelsRaw)
	if err != nil {
		return nil, fmt.Errorf("scan tg labels: %w", err)
	}
	rec.Labels = labels
	hc, err := dto.HealthCheckFromJSONB(hcRaw)
	if err != nil {
		return nil, fmt.Errorf("scan tg health_check: %w", err)
	}
	rec.HealthCheck = hc
	return &rec, nil
}

func scanTarget(row pgx.Row) (*kacho.TargetRecord, error) {
	var (
		rec       kacho.TargetRecord
		idStr     string
		tgIDStr   string
		instID    *string
		nicID     *string
		ipSubnet  *string
		ipAddr    *string
		extAddr   *string
		extZoneID *string
		weight    int32
		statusStr string
	)
	if err := row.Scan(
		&idStr, &tgIDStr, &rec.CreatedAt, &rec.UpdatedAt,
		&instID, &nicID, &ipSubnet, &ipAddr,
		&extAddr, &extZoneID,
		&weight, &statusStr, &rec.DrainStartedAt,
	); err != nil {
		return nil, err
	}
	rec.ID = idStr
	rec.TargetGroupID = tgIDStr
	rec.Status = statusStr
	rec.Weight = domain.LbWeight(weight)
	switch {
	case instID != nil:
		rec.InstanceID = option.MustNewOption(domain.InstanceID(*instID))
	case nicID != nil:
		rec.NicID = option.MustNewOption(domain.NicID(*nicID))
	case ipSubnet != nil && ipAddr != nil:
		rec.IPRef = &domain.TargetIPRef{
			SubnetID: domain.SubnetID(*ipSubnet),
			Address:  domain.IPAddress(*ipAddr),
		}
	case extAddr != nil:
		rec.ExternalIP = &domain.TargetExternalIP{Address: domain.IPAddress(*extAddr)}
		if extZoneID != nil && *extZoneID != "" {
			rec.ExternalIP.ZoneID = option.MustNewOption(domain.ZoneID(*extZoneID))
		}
	}
	return &rec, nil
}

func (r *targetGroupReader) Get(ctx context.Context, id string) (*kacho.TargetGroupRecord, error) {
	q := fmt.Sprintf(`SELECT %s FROM kacho_nlb.target_groups WHERE id = $1`, targetGroupCols)
	row := r.tx.QueryRow(ctx, q, id)
	rec, err := scanTG(row)
	if err != nil {
		return nil, mapPgErr(err, "TargetGroup", id)
	}
	// Подгружаем targets inline (≤100 на TG, влезает в один SELECT).
	targets, err := r.ListTargets(ctx, id)
	if err != nil {
		return nil, err
	}
	fillTargets(rec, targets)
	return rec, nil
}

// fillTargets — единственное место, где путь чтения раскладывает строки целей
// по двум наборам записи. Оба заполняются вместе (контракт
// `TargetGroupRecord.TargetStates`): `Targets` — доменный взгляд, `TargetStates`
// — он же плюс lifecycle-состояние, из которого строится публичная проекция.
// Разложение живёт функцией, а не повторяется по вызывающим: повтор — это ровно
// то место, где один из двух наборов однажды забудут.
func fillTargets(rec *kacho.TargetGroupRecord, targets []*kacho.TargetRecord) {
	if len(targets) == 0 {
		return
	}
	rec.Targets = make([]domain.Target, 0, len(targets))
	rec.TargetStates = make([]kacho.TargetRecord, 0, len(targets))
	for _, t := range targets {
		rec.Targets = append(rec.Targets, t.Target)
		rec.TargetStates = append(rec.TargetStates, *t)
	}
}

func (r *targetGroupReader) List(ctx context.Context, f kacho.TargetGroupFilter, p kacho.Pagination) ([]*kacho.TargetGroupRecord, string, error) {
	pageSize, err := pageSizeOrDefault(p.PageSize)
	if err != nil {
		return nil, "", err
	}
	conditions := []string{}
	args := []any{}
	argIdx := 1
	if f.ProjectID != "" {
		conditions = append(conditions, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, f.ProjectID)
		argIdx++
	}
	if f.Name != nil {
		// Оператор берётся из РАЗОБРАННОГО узла: подстрочный запрос обязан остаться
		// подстрочным. Зашитое здесь равенство отвечало бы «ничего не найдено» на
		// любой неполный ввод — уверенно и неверно (#460).
		frag, fargs := f.Name.ToSQLOn("name", argIdx)
		conditions = append(conditions, frag)
		args = append(args, fargs...)
		argIdx++
	}
	if p.PageToken != "" {
		cur, err := decodePageToken(p.PageToken)
		if err != nil {
			return nil, "", err
		}
		conditions = append(conditions, fmt.Sprintf("(created_at, id) > ($%d, $%d)", argIdx, argIdx+1))
		args = append(args, cur.CreatedAt, cur.ID)
		argIdx += 2
	}
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	q := fmt.Sprintf(`SELECT %s FROM kacho_nlb.target_groups %s ORDER BY created_at ASC, id ASC LIMIT $%d`,
		targetGroupCols, where, argIdx)
	args = append(args, pageSize+1)
	rows, err := r.tx.Query(ctx, q, args...)
	if err != nil {
		return nil, "", mapPgErr(err, "TargetGroup", "")
	}
	defer rows.Close()
	var result []*kacho.TargetGroupRecord
	for rows.Next() {
		rec, err := scanTG(rows)
		if err != nil {
			return nil, "", mapPgErr(err, "TargetGroup", "")
		}
		result = append(result, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, "", mapPgErr(err, "TargetGroup", "")
	}
	nextToken := ""
	if int64(len(result)) > pageSize {
		last := result[pageSize-1]
		nextToken = encodePageToken(last.CreatedAt, string(last.ID))
		result = result[:pageSize]
	}
	// Цели подгружаются ПОСЛЕ усечения страницы: за строку, которая уехала в
	// следующую страницу, платить нечем.
	//
	// Проекция списка совпадает с проекцией одиночного чтения by construction:
	// message контракта у обоих один, поэтому пустой массив здесь обязан
	// означать «целей нет», а не «это чтение поле не заполняет» — второе
	// вызывающему на проводе неотличимо от первого, и клиент, ведущий состояние
	// из списка, прочтёт его как «все цели удалены».
	ids := make([]string, 0, len(result))
	for _, rec := range result {
		ids = append(ids, string(rec.ID))
	}
	byGroup, err := r.listTargetsForGroups(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	for _, rec := range result {
		fillTargets(rec, byGroup[string(rec.ID)])
	}
	return result, nextToken, nil
}

func (r *targetGroupReader) ListByProject(ctx context.Context, projectID string, p kacho.Pagination) ([]*kacho.TargetGroupRecord, string, error) {
	return r.List(ctx, kacho.TargetGroupFilter{ProjectID: projectID}, p)
}

func (r *targetGroupReader) ListTargets(ctx context.Context, tgID string) ([]*kacho.TargetRecord, error) {
	// LIMIT MaxTargetsPerGroup — safety-net: cap на группу гарантирует ≤100 строк,
	// но LIMIT защищает Get() от материализации распухшей (legacy/невалидной) группы
	// в память безусловно (CWE-770: unbounded ListTargets).
	q := fmt.Sprintf(`SELECT %s FROM kacho_nlb.targets WHERE target_group_id = $1 ORDER BY created_at ASC, id ASC LIMIT %d`,
		targetCols, domain.MaxTargetsPerGroup)
	rows, err := r.tx.Query(ctx, q, tgID)
	if err != nil {
		return nil, mapPgErr(err, "Target", "")
	}
	defer rows.Close()
	var out []*kacho.TargetRecord
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, mapPgErr(err, "Target", "")
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgErr(err, "Target", "")
	}
	return out, nil
}

// listTargetsForGroups — цели СТРАНИЦЫ групп одной выборкой.
//
// # Почему не цикл по группам
//
// Список отдаёт до 1000 групп на страницу; вызов ListTargets на каждую дал бы
// столько же обращений к БД на один запрос чтения. Цена страницы принадлежит
// запросу, а не числу строк на ней.
//
// # Почему окно, а не общий LIMIT
//
// Одиночное чтение защищено `LIMIT MaxTargetsPerGroup` — безусловный потолок
// материализации распухшей (legacy/невалидной) группы. Тот же LIMIT «как есть»
// на выборке по странице усёк бы страницу ЦЕЛИКОМ: первая большая группа съела
// бы квоту остальных, и соседи молча приехали бы без целей — то есть ровно тот
// дефект, который здесь чинится, вернулся бы с другой стороны. Потолок обязан
// оставаться потолком НА ГРУППУ, поэтому нумерация идёт внутри партиции.
//
// Порядок внутри группы — тот же, что у одиночного чтения (`created_at, id`), и
// это НЕ порядок вставки: у строк одной транзакции метка времени совпадает, спор
// разрешает крокфордов идентификатор. Поле объявлено НАБОРОМ (см. комментарий
// `TargetGroup.targets` в контракте), поэтому одинаковость важна, а порядок —
// нет; сверять состав надо по элементам, не по индексам.
func (r *targetGroupReader) listTargetsForGroups(ctx context.Context, tgIDs []string) (map[string][]*kacho.TargetRecord, error) {
	out := make(map[string][]*kacho.TargetRecord, len(tgIDs))
	if len(tgIDs) == 0 {
		return out, nil
	}
	q := fmt.Sprintf(`SELECT %s FROM (
        SELECT %s, row_number() OVER (
                   PARTITION BY target_group_id ORDER BY created_at ASC, id ASC
               ) AS rn
          FROM kacho_nlb.targets
         WHERE target_group_id = ANY($1)
    ) ranked
     WHERE rn <= %d
     ORDER BY target_group_id ASC, created_at ASC, id ASC`,
		targetCols, targetCols, domain.MaxTargetsPerGroup)
	rows, err := r.tx.Query(ctx, q, tgIDs)
	if err != nil {
		return nil, mapPgErr(err, "Target", "")
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, mapPgErr(err, "Target", "")
		}
		out[t.TargetGroupID] = append(out[t.TargetGroupID], t)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgErr(err, "Target", "")
	}
	return out, nil
}

func (r *targetGroupReader) ListDrainingExpired(ctx context.Context, tgID string, delaySeconds int32) ([]*kacho.TargetRecord, error) {
	q := fmt.Sprintf(`SELECT %s FROM kacho_nlb.targets
        WHERE target_group_id = $1
          AND status = 'DRAINING'
          AND drain_started_at IS NOT NULL
          AND drain_started_at < now() - make_interval(secs => $2)`, targetCols)
	rows, err := r.tx.Query(ctx, q, tgID, delaySeconds)
	if err != nil {
		return nil, mapPgErr(err, "Target", "")
	}
	defer rows.Close()
	var out []*kacho.TargetRecord
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, mapPgErr(err, "Target", "")
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgErr(err, "Target", "")
	}
	return out, nil
}

// ReferencingListenerIDs — id листенеров, ссылающихся на TG через
// listeners.default_target_group_id (FK RESTRICT из 0018). ORDER BY id для
// детерминированного порядка в teardown-precheck error-тексте (NLB-1-41).
func (r *targetGroupReader) ReferencingListenerIDs(ctx context.Context, tgID string) ([]string, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id FROM kacho_nlb.listeners WHERE default_target_group_id = $1 ORDER BY id`,
		tgID,
	)
	if err != nil {
		return nil, mapPgErr(err, "TargetGroup", tgID)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, mapPgErr(err, "TargetGroup", tgID)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgErr(err, "TargetGroup", tgID)
	}
	return ids, nil
}

type targetGroupWriter struct {
	targetGroupReader
}

// withTargets — дочитывает цели группы и раскладывает их по обоим наборам записи.
//
// # Почему пути ЗАПИСИ обязаны это делать, а не только чтение
//
// Публичная проекция группы строится из `TargetStates`, поэтому запись без него
// проецируется в группу с ПУСТЫМ набором целей. Пустой массив неотличим на
// проводе от «целей нет» — и таким его читает и вызывающий (ответ операции
// правки и переезда), и подписчик потока (событие вида `nlb_target_group` несёт
// полное состояние). Прежде `Update` и `MoveProject` целей не подгружали, и оба
// ответа приходили с пустым набором при живых целях.
//
// Чтение идёт в ТОЙ ЖЕ транзакции, сразу за мутацией: это состояние на момент
// события, а не на момент публикации.
func (w *targetGroupWriter) withTargets(ctx context.Context, rec *kacho.TargetGroupRecord) (*kacho.TargetGroupRecord, error) {
	targets, err := w.ListTargets(ctx, string(rec.ID))
	if err != nil {
		return nil, err
	}
	fillTargets(rec, targets)
	return rec, nil
}

func (w *targetGroupWriter) Insert(ctx context.Context, tg *domain.TargetGroup) (*kacho.TargetGroupRecord, error) {
	labelsJSON, err := dto.LabelsToJSONB(tg.Labels)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", kacho.ErrInvalidArg, err)
	}
	hcJSON, err := dto.HealthCheckToJSONB(tg.HealthCheck)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", kacho.ErrInvalidArg, err)
	}
	deregSecs, err := dto.DurationToSeconds(tg.DeregistrationDelay)
	if err != nil {
		return nil, fmt.Errorf("%w: deregistration_delay: %v", kacho.ErrInvalidArg, err)
	}
	slowSecs, err := dto.DurationToSeconds(tg.SlowStart)
	if err != nil {
		return nil, fmt.Errorf("%w: slow_start: %v", kacho.ErrInvalidArg, err)
	}
	q := fmt.Sprintf(`
        INSERT INTO kacho_nlb.target_groups
            (id, project_id, region_id, name, description, labels, health_check,
             deregistration_delay_seconds, slow_start_seconds, port, status)
        VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
        RETURNING %s`, targetGroupCols)
	row := w.tx.QueryRow(ctx, q,
		string(tg.ID), string(tg.ProjectID), string(tg.RegionID),
		string(tg.Name), string(tg.Description), labelsJSON, hcJSON,
		deregSecs, slowSecs,
		int32(tg.Port), string(tg.Status),
	)
	rec, err := scanTG(row)
	if err != nil {
		return nil, mapPgErr(err, "TargetGroup", string(tg.ID))
	}
	// Inline insert targets если они есть.
	if len(tg.Targets) > 0 {
		if _, err := w.AddTargets(ctx, string(tg.ID), tg.Targets); err != nil {
			return nil, err
		}
		// Re-load inline-targets для completeness response'а.
		targets, err := w.ListTargets(ctx, string(tg.ID))
		if err != nil {
			return nil, err
		}
		fillTargets(rec, targets)
	}
	return rec, nil
}

func (w *targetGroupWriter) Update(ctx context.Context, tg *domain.TargetGroup, expectedXmin string) (*kacho.TargetGroupRecord, error) {
	labelsJSON, err := dto.LabelsToJSONB(tg.Labels)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", kacho.ErrInvalidArg, err)
	}
	hcJSON, err := dto.HealthCheckToJSONB(tg.HealthCheck)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", kacho.ErrInvalidArg, err)
	}
	deregSecs, err := dto.DurationToSeconds(tg.DeregistrationDelay)
	if err != nil {
		return nil, fmt.Errorf("%w: deregistration_delay: %v", kacho.ErrInvalidArg, err)
	}
	slowSecs, err := dto.DurationToSeconds(tg.SlowStart)
	if err != nil {
		return nil, fmt.Errorf("%w: slow_start: %v", kacho.ErrInvalidArg, err)
	}
	// OCC на read-modify-write (`WHERE xmin::text=$exp`): concurrent-modify между
	// Get и этим UPDATE → 0 rows → FailedPrecondition (защита от lost update на
	// partial-mask Update). См. data-integrity.md OCC / LoadBalancerRecord.Xmin.
	// port is LIVE-mutable (NLB-1c NLB-1-56) — included in the SET so a
	// TargetGroup.Update repointing the backend port re-echoes into wired
	// listeners' resolved_backend_port (derived from tg.port on read).
	q := fmt.Sprintf(`
        UPDATE kacho_nlb.target_groups
           SET name = $2,
               description = $3,
               labels = $4::jsonb,
               health_check = $5::jsonb,
               deregistration_delay_seconds = $6,
               slow_start_seconds = $7,
               port = $8,
               updated_at = now()
         WHERE id = $1 AND xmin::text = $9
        RETURNING %s`, targetGroupCols)
	row := w.tx.QueryRow(ctx, q,
		string(tg.ID),
		string(tg.Name), string(tg.Description), labelsJSON, hcJSON,
		deregSecs, slowSecs,
		int32(tg.Port),
		expectedXmin,
	)
	rec, err := scanTG(row)
	if err != nil {
		if pgxIsNoRows(err) {
			return nil, fmt.Errorf("%w: TargetGroup %s was modified concurrently", kacho.ErrFailedPrecondition, string(tg.ID))
		}
		return nil, mapPgErr(err, "TargetGroup", string(tg.ID))
	}
	return w.withTargets(ctx, rec)
}

func (w *targetGroupWriter) SetStatusCAS(ctx context.Context, id string, expected, newStatus domain.TargetGroupStatus) (*kacho.TargetGroupRecord, error) {
	q := fmt.Sprintf(`
        UPDATE kacho_nlb.target_groups
           SET status = $3, updated_at = now()
         WHERE id = $1 AND status = $2
        RETURNING %s`, targetGroupCols)
	row := w.tx.QueryRow(ctx, q, id, string(expected), string(newStatus))
	rec, err := scanTG(row)
	if err != nil {
		if pgxIsNoRows(err) {
			return nil, fmt.Errorf("%w: TargetGroup %s status is not %s", kacho.ErrFailedPrecondition, id, expected)
		}
		return nil, mapPgErr(err, "TargetGroup", id)
	}
	return rec, nil
}

// MoveProject — atomic project-rewrite of a TargetGroup.
//
// Инвариант (within-service refs на DB-уровне):
// TG, на который ссылается хоть один listener (listeners.default_target_group_id),
// двигать НЕЛЬЗЯ — иначе listener в проекте A ссылался бы на TG в проекте B
// (cross-project ref, запрещён моделью). Sync-precheck ReferencingListenerIDs в
// use-case'е — UX/fast-fail; `NOT EXISTS`-guard ниже — второй fast-fail с
// контрактным тоном сообщения.
//
// ЧТО РЕАЛЬНО ЗАКРЫВАЕТ Move↔wire TOCTOU — композитный FK миграции 0023
// (`listeners(default_tg_fk, project_id) → target_groups(id, project_id)`), НЕ
// `NOT EXISTS` сам по себе: guard читает listeners на СНАПШОТЕ своей TX, а
// wire-путь после сноса pivot'а — плоский `UPDATE listeners SET
// default_target_group_id=…` (OCC по xmin), который никаких lock'ов на
// target_groups/load_balancers сам не берёт. Работает это так: UNIQUE
// (id, project_id) делает project_id КЛЮЧЕВОЙ колонкой referenced-стороны,
// поэтому
//   - move-first: смена ключа берёт exclusive tuple-lock → RI-проба
//     конкурентного wire (`… FOR KEY SHARE`) конфликтует, ждёт и после commit'а
//     перечитывает через EvalPlanQual уже НЕ тот project → 23503;
//   - wire-first: KEY SHARE удерживает tuple → key-update ждёт, а
//     referenced-side ON UPDATE NO ACTION триггер (пробует свежим снапшотом)
//     видит закоммиченную ссылку → 23503.
//
// Отзыв композитного FK (или UNIQUE (id, project_id), молча понижающего
// tuple-lock до «no key update») снова открывает гонку — это ловят
// tg_move_repoint_race_integration_test.go.
//
// 0 rows при существующем TG → на него сослался listener между sync-check и apply →
// FailedPrecondition; отсутствующий TG → NotFound; проигравшая гонку TX получает
// FailedPrecondition по тому же 23503. Оба исхода отдают ОДИН контрактный текст
// (`tgMoveBlockedByListeners`, restrict_fk.go) — тот же, что предпроверка
// use-case'а: разные тексты об одном отказе — это два места об одном предмете.
func (w *targetGroupWriter) MoveProject(ctx context.Context, id, newProjectID string) (*kacho.TargetGroupRecord, error) {
	q := fmt.Sprintf(`
        UPDATE kacho_nlb.target_groups
           SET project_id = $2, updated_at = now()
         WHERE id = $1
           AND NOT EXISTS (
               SELECT 1 FROM kacho_nlb.listeners
                WHERE default_target_group_id = $1
           )
        RETURNING %s`, targetGroupCols)
	// Точка сохранения: отказ композитного FK при переписывании ключа отменяет
	// транзакцию, а назвать блокирующих слушателей можно только живой (см.
	// restrict_fk.go). Решение по-прежнему за БД — точка сохранения ничего не
	// решает и ничего не повторяет.
	sp, err := w.tx.Begin(ctx)
	if err != nil {
		return nil, mapPgErr(err, "TargetGroup", id)
	}
	rec, err := scanTG(sp.QueryRow(ctx, q, id, newProjectID))
	if err != nil {
		_ = sp.Rollback(ctx)
		// Композитный FK 0023 отверг переписывание ключа: слушатель успел
		// сослаться. Тот же факт, что и у guard'а NOT EXISTS ниже, — значит и
		// тот же контрактный текст.
		if isFKViolation(err, "listeners_target_group_fk") {
			return nil, tgMoveBlockedByListeners(ctx, w.tx, id)
		}
		if pgxIsNoRows(err) {
			// Различаем «TG нет» (NotFound) и «есть ссылающийся listener» (FailedPrecondition).
			var exists bool
			if e := w.tx.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM kacho_nlb.target_groups WHERE id = $1)`, id,
			).Scan(&exists); e != nil {
				return nil, mapPgErr(e, "TargetGroup", id)
			}
			if exists {
				return nil, tgMoveBlockedByListeners(ctx, w.tx, id)
			}
			return nil, fmt.Errorf("%w: TargetGroup %s not found", kacho.ErrNotFound, id)
		}
		return nil, mapPgErr(err, "TargetGroup", id)
	}
	if err := sp.Commit(ctx); err != nil {
		return nil, mapPgErr(err, "TargetGroup", id)
	}
	return w.withTargets(ctx, rec)
}

// AddTargets — INSERT ON CONFLICT DO NOTHING per-identity-type partial UNIQUE.
// Возвращает количество фактически вставленных строк.
//
// Skill workspace CLAUDE.md «within-service refs»: идемпотентный re-add того же
// identity-tuple обрабатывается через ON CONFLICT (на 4 partial UNIQUE индексах).
// Single-target INSERT — мы не делаем bulk INSERT с одним ON CONFLICT, потому что
// 4 partial UNIQUE индекса не покрываются одним ON CONFLICT-target — обработаем
// каждый INSERT отдельно (≤MaxTargetsPerGroup=100 за вызов, приемлемая
// per-target latency для нечастого RPC).
func (w *targetGroupWriter) AddTargets(ctx context.Context, tgID string, targets []domain.Target) (int, error) {
	if len(targets) == 0 {
		return 0, nil
	}
	// Cumulative per-group cap («raid protection»):
	// per-call limit в use-case'е не мешает раздуть группу серией AddTargets. Здесь
	// прибиваем инвариант на DB-уровне. FOR UPDATE на parent target_groups строке
	// сериализует конкурентные AddTargets одной группы (иначе два воркера оба
	// прочитают count<max и оба вставят — race), после чего count стабилен в TX.
	var locked string
	if err := w.tx.QueryRow(ctx,
		`SELECT id FROM kacho_nlb.target_groups WHERE id = $1 FOR UPDATE`, tgID,
	).Scan(&locked); err != nil {
		if pgxIsNoRows(err) {
			return 0, fmt.Errorf("%w: TargetGroup %s not found", kacho.ErrNotFound, tgID)
		}
		return 0, mapPgErr(err, "TargetGroup", tgID)
	}
	var current int
	if err := w.tx.QueryRow(ctx,
		`SELECT count(*) FROM kacho_nlb.targets WHERE target_group_id = $1`, tgID,
	).Scan(&current); err != nil {
		return 0, mapPgErr(err, "Target", "")
	}
	if current+len(targets) > domain.MaxTargetsPerGroup {
		return 0, fmt.Errorf(
			"%w: target group would exceed the maximum of %d targets (current %d, adding %d)",
			kacho.ErrFailedPrecondition, domain.MaxTargetsPerGroup, current, len(targets))
	}

	inserted := 0
	for i := range targets {
		t := targets[i]
		instID, nicID, ipSubnet, ipAddr, extAddr, extZoneID := splitTargetIdentity(t)

		// 1. Reactivate a same-identity DRAINING row atomically (CAS). A removed
		// target lives as status='DRAINING' until the phase-B drain runner
		// DELETEs it; its identity still occupies the partial-UNIQUE slot (the
		// indexes carry no status predicate). A plain INSERT ... ON CONFLICT DO
		// NOTHING would therefore treat re-adding it as a swallowed no-op, and
		// the drain runner would then delete the still-DRAINING row — silently
		// dropping a target the tenant asked to keep in service (finding DATA #2,
		// CWE-362). Match on the full identity tuple (IS NOT DISTINCT FROM handles
		// the NULL identity columns) and flip it back to ACTIVE with the re-added
		// weight. Serializes with the drain runner's DELETE on the target row lock.
		const reactivateQ = `
            UPDATE kacho_nlb.targets
               SET status = 'ACTIVE', drain_started_at = NULL,
                   weight = $8, updated_at = now()
             WHERE target_group_id = $1
               AND status = 'DRAINING'
               AND instance_id         IS NOT DISTINCT FROM $2
               AND nic_id              IS NOT DISTINCT FROM $3
               AND ip_ref_subnet_id    IS NOT DISTINCT FROM $4
               AND ip_ref_address      IS NOT DISTINCT FROM $5
               AND external_ip_address IS NOT DISTINCT FROM $6
               AND external_ip_zone_id IS NOT DISTINCT FROM $7
            RETURNING id`
		var reactivatedID string
		err := w.tx.QueryRow(ctx, reactivateQ,
			tgID,
			nullableStr(instID), nullableStr(nicID),
			nullableStr(ipSubnet), nullableStr(ipAddr),
			nullableStr(extAddr), nullableStr(extZoneID),
			int32(t.Weight),
		).Scan(&reactivatedID)
		if err == nil {
			inserted++
			continue
		}
		if !pgxIsNoRows(err) {
			return inserted, mapPgErr(err, "Target", "")
		}

		// 2. No DRAINING row to reactivate → genuine insert. ON CONFLICT DO
		// NOTHING keeps re-add of an already-ACTIVE identity idempotent (0 rows).
		id := newTargetID()
		const q = `
            INSERT INTO kacho_nlb.targets
                (id, target_group_id,
                 instance_id, nic_id, ip_ref_subnet_id, ip_ref_address,
                 external_ip_address, external_ip_zone_id,
                 weight, status, drain_started_at)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'ACTIVE', NULL)
            ON CONFLICT DO NOTHING
            RETURNING id`
		var returnedID string
		err = w.tx.QueryRow(ctx, q,
			id, tgID,
			nullableStr(instID), nullableStr(nicID),
			nullableStr(ipSubnet), nullableStr(ipAddr),
			nullableStr(extAddr), nullableStr(extZoneID),
			int32(t.Weight),
		).Scan(&returnedID)
		if err != nil {
			if pgxIsNoRows(err) {
				// ON CONFLICT DO NOTHING — uniq violation погашена; не считаем.
				continue
			}
			return inserted, mapPgErr(err, "Target", "")
		}
		inserted++
	}
	return inserted, nil
}

// RemoveTargetsMarkDraining — фаза A двухфазного drain: status='DRAINING' +
// drain_started_at=now. CHECK targets_drain_consistency требует drain_started_at
// IS NOT NULL когда status='DRAINING' (миграция 0001).
func (w *targetGroupWriter) RemoveTargetsMarkDraining(ctx context.Context, tgID string, targetIDs []string) (int, error) {
	if len(targetIDs) == 0 {
		return 0, nil
	}
	tag, err := w.tx.Exec(ctx,
		`UPDATE kacho_nlb.targets
            SET status = 'DRAINING', drain_started_at = now(), updated_at = now()
          WHERE target_group_id = $1
            AND id = ANY($2::text[])
            AND status = 'ACTIVE'`,
		tgID, targetIDs,
	)
	if err != nil {
		return 0, mapPgErr(err, "Target", "")
	}
	return int(tag.RowsAffected()), nil
}

// DeleteTargetsDraining — все дренирующиеся строки группы, без учёта задержки.
// Вызывается только из TargetGroup.Delete, в той же writer-TX, что и DELETE
// самой группы: цель, помеченная DRAINING, уже снята вызывающим, а группа,
// которую сносят, трафика не принимает. `status='ACTIVE'` не трогаем — FK
// RESTRICT обязан поймать конкурентный AddTargets.
func (w *targetGroupWriter) DeleteTargetsDraining(ctx context.Context, tgID string) (int, error) {
	tag, err := w.tx.Exec(ctx,
		`DELETE FROM kacho_nlb.targets
          WHERE target_group_id = $1
            AND status = 'DRAINING'`,
		tgID,
	)
	if err != nil {
		return 0, mapPgErr(err, "Target", "")
	}
	return int(tag.RowsAffected()), nil
}

// DeleteExpiredDrainingTargets — фаза B слива: снимает дренирующиеся строки, у
// которых истёк `deregistration_delay` ВЛАДЕЮЩЕЙ группы, и возвращает различные
// идентификаторы затронутых групп.
//
// Срок берётся у владеющей группы (`tg.deregistration_delay_seconds`), поэтому
// JOIN обязателен: у групп он разный. `ON DELETE RESTRICT` на ссылке цели
// гарантирует, что владелец есть у каждой строки.
//
// Строку журнала собирает ВЫЗЫВАЮЩИЙ — тем же строителем, что и остальные точки
// эмиссии вида, и в ЭТОЙ ЖЕ транзакции (см. объявление порта).
func (w *targetGroupWriter) DeleteExpiredDrainingTargets(ctx context.Context) (int64, []string, error) {
	rows, err := w.tx.Query(ctx, `
        DELETE FROM kacho_nlb.targets t
              USING kacho_nlb.target_groups tg
              WHERE t.target_group_id = tg.id
                AND t.status = 'DRAINING'
                AND t.drain_started_at < now() - make_interval(secs => tg.deregistration_delay_seconds)
          RETURNING t.target_group_id`)
	if err != nil {
		return 0, nil, mapPgErr(err, "Target", "")
	}
	defer rows.Close()

	var deleted int64
	seen := make(map[string]struct{})
	var tgIDs []string
	for rows.Next() {
		var tgID string
		if err := rows.Scan(&tgID); err != nil {
			return 0, nil, mapPgErr(err, "Target", "")
		}
		deleted++
		if _, ok := seen[tgID]; !ok {
			seen[tgID] = struct{}{}
			tgIDs = append(tgIDs, tgID)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, nil, mapPgErr(err, "Target", "")
	}
	return deleted, tgIDs, nil
}

// Delete — снос группы целей. Отказывает не этот код, а схема: ссылки
// слушателя (`listeners_target_group_fk`) и цели (`targets_target_group_id_fkey`)
// держатся `ON DELETE RESTRICT`. Здесь остаётся только отображение 23503 в код
// и КОНТРАКТНЫЙ ТЕКСТ, называющий блокирующие строки, — см. restrict_fk.go.
func (w *targetGroupWriter) Delete(ctx context.Context, id string) error {
	return deleteParentRow(ctx, w.tx, "TargetGroup", id,
		`DELETE FROM kacho_nlb.target_groups WHERE id = $1`)
}

// splitTargetIdentity — раскладывает domain.Target в 6 nullable-полей колонок
// (parity с CHECK targets_identity_exactly_one).
func splitTargetIdentity(t domain.Target) (instID, nicID, ipSubnet, ipAddr, extAddr, extZoneID string) {
	if v, ok := t.InstanceID.Maybe(); ok {
		instID = string(v)
	}
	if v, ok := t.NicID.Maybe(); ok {
		nicID = string(v)
	}
	if t.IPRef != nil {
		ipSubnet = string(t.IPRef.SubnetID)
		ipAddr = string(t.IPRef.Address)
	}
	if t.ExternalIP != nil {
		extAddr = string(t.ExternalIP.Address)
		if z, ok := t.ExternalIP.ZoneID.Maybe(); ok {
			extZoneID = string(z)
		}
	}
	return
}

// nullableStr — пустая строка → nil (для NULL в DB), иначе &s.
func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// targetIDPrefix — 3-char prefix для target row id. Target — embedded child
// TargetGroup (не tenant-facing ресурс верхнего уровня), у него нет PrefixTarget
// в kacho-corelib/ids. Локальный prefix "tgt" парный с TargetGroup prefix "tgr".
const targetIDPrefix = "tgt"

// newTargetID — генерит stable id для target row. Используем
// kacho-corelib/ids.NewID с локальным 3-char prefix — это даёт 17-char
// crockford-base32 suffix с crypto/rand-энтропией, формат идентичен другим
// kacho-ресурсам. Stable id критичен для RemoveTargets/peer-validate.
func newTargetID() string {
	return ids.NewID(targetIDPrefix)
}
