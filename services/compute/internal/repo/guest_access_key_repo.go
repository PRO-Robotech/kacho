// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// GuestAccessKeyRepo — хранение публичных ключей входа в машину.
type GuestAccessKeyRepo struct {
	pool *pgxpool.Pool
}

// NewGuestAccessKeyRepo создаёт репозиторий ключей.
func NewGuestAccessKeyRepo(pool *pgxpool.Pool) *GuestAccessKeyRepo {
	return &GuestAccessKeyRepo{pool: pool}
}

const guestKeyCols = `id, project_id, name, public_key, fingerprint, labels, created_at`

func scanGuestKey(row pgx.Row) (*domain.GuestAccessKey, error) {
	var k domain.GuestAccessKey
	var labels []byte
	if err := row.Scan(&k.ID, &k.ProjectID, &k.Name, &k.PublicKey, &k.Fingerprint, &labels, &k.CreatedAt); err != nil {
		return nil, err
	}
	if err := unmarshalJSONB(labels, &k.Labels, "GuestAccessKey.labels"); err != nil {
		return nil, err
	}
	return &k, nil
}

// Get возвращает ключ по идентификатору.
func (r *GuestAccessKeyRepo) Get(ctx context.Context, id string) (*domain.GuestAccessKey, error) {
	q := fmt.Sprintf(`SELECT %s FROM guest_access_keys WHERE id = $1`, guestKeyCols)
	k, err := scanGuestKey(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, wrapPgErr(err, "GuestAccessKey", id)
	}
	return k, nil
}

// List возвращает страницу ключей проекта тем же курсором, что у прочих
// ресурсов продукта: пара «время создания, идентификатор».
func (r *GuestAccessKeyRepo) List(ctx context.Context, projectID string, p ports.Pagination) ([]*domain.GuestAccessKey, string, error) {
	pageSize, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}

	args := []any{projectID}
	where := "WHERE project_id = $1"
	if p.PageToken != "" {
		tsv, id, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", invalidPageTokenErr(derr)
		}
		where += " AND (created_at, id) > ($2, $3)"
		args = append(args, tsv, id)
	}
	q := fmt.Sprintf(`SELECT %s FROM guest_access_keys %s ORDER BY created_at ASC, id ASC LIMIT $%d`,
		guestKeyCols, where, len(args)+1)
	args = append(args, pageSize+1)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", wrapPgErr(err, "GuestAccessKey", "")
	}
	defer rows.Close()

	var out []*domain.GuestAccessKey
	for rows.Next() {
		k, serr := scanGuestKey(rows)
		if serr != nil {
			return nil, "", wrapPgErr(serr, "GuestAccessKey", "")
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, "", wrapPgErr(err, "GuestAccessKey", "")
	}

	var next string
	if int64(len(out)) > pageSize {
		last := out[pageSize-1]
		next = encodePageToken(last.CreatedAt, last.ID)
		out = out[:pageSize]
	}
	return out, next, nil
}

// Insert записывает ключ и намерение регистрации прав в одной транзакции.
//
// Журнал действий пишется здесь же: ключ — вход в машину МИМО плоскости
// управления, и запись о том, кто его завёл, единственная связывает вход с
// человеком. Без неё «кто-то вошёл» останется без ответа навсегда.
func (r *GuestAccessKeyRepo) Insert(ctx context.Context, k *domain.GuestAccessKey) (*domain.GuestAccessKey, []ownerregister.Registration, error) {
	// orEmptyMap обязателен: nil-карта сериализуется в `null`, а схема требует
	// объект. Без него ключ БЕЗ меток — самый обычный ключ — не создавался бы
	// вовсе, и отказ приходил бы про разбор ввода, которого вызывающий не слал.
	labelsJSON, err := marshalJSONB(orEmptyMap(k.Labels), "GuestAccessKey.labels")
	if err != nil {
		return nil, nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := fmt.Sprintf(`INSERT INTO guest_access_keys (%s)
		VALUES ($1,$2,$3,$4,$5,$6,now()) RETURNING %s`,
		`id, project_id, name, public_key, fingerprint, labels, created_at`, guestKeyCols)
	created, err := scanGuestKey(tx.QueryRow(ctx, q, k.ID, k.ProjectID, k.Name, k.PublicKey, k.Fingerprint, labelsJSON))
	if err != nil {
		return nil, nil, wrapPgErr(err, "GuestAccessKey", k.Name)
	}

	actor, onBehalf := auditPrincipals(ctx)
	if err := emitAudit(ctx, tx, AuditEvent{
		EventType:    "guest_access_key.create",
		ResourceType: "GuestAccessKey",
		ResourceID:   created.ID,
		ProjectID:    created.ProjectID,
		Actor:        actor,
		OnBehalfOf:   onBehalf,
		Payload:      map[string]any{"name": created.Name, "fingerprint": created.Fingerprint},
	}); err != nil {
		return nil, nil, ports.ErrInternal
	}

	reg, err := emitFGARegisterIntent(ctx, tx, fgaintent.EventRegister, "GuestAccessKey", created.ID, created.ProjectID, created.Labels)
	if err != nil {
		return nil, nil, ports.ErrInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, wrapPgErr(err, "GuestAccessKey", k.Name)
	}
	return created, registrationsOf(reg), nil
}

// Update пишет ТОЛЬКО названные маской колонки одним стейтментом.
//
// Читать строку, сливать маску в памяти и писать снимок обратно нельзя: две
// правки с непересекающимися масками затёрли бы друг друга, обе отчитались бы
// успехом, и потеря была бы молчаливой. Материал ключа и отпечаток здесь не
// упоминаются вовсе — не потому, что вызывающий их не прислал, а потому что
// колонок для них в наборе нет by construction.
func (r *GuestAccessKeyRepo) Update(ctx context.Context, id string, u ports.GuestAccessKeyUpdate) (*domain.GuestAccessKey, error) {
	if !u.Touched() {
		// Ничего не названо — записи не требуется, но исход обязан отличать
		// существующую строку от отсутствующей.
		return r.Get(ctx, id)
	}

	set := make([]string, 0, 2)
	args := []any{id}
	add := func(col string, val any) {
		args = append(args, val)
		set = append(set, fmt.Sprintf("%s=$%d", col, len(args)))
	}

	if u.Name != nil {
		add("name", *u.Name)
	}
	if u.LabelsSet {
		labelsJSON, err := marshalJSONB(orEmptyMap(u.Labels), "GuestAccessKey.labels")
		if err != nil {
			return nil, err
		}
		add("labels", labelsJSON)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := fmt.Sprintf(`UPDATE guest_access_keys SET %s WHERE id=$1 RETURNING %s`,
		strings.Join(set, ", "), guestKeyCols)
	updated, err := scanGuestKey(tx.QueryRow(ctx, q, args...))
	if err != nil {
		return nil, wrapPgErr(err, "GuestAccessKey", id)
	}

	actor, onBehalf := auditPrincipals(ctx)
	if err := emitAudit(ctx, tx, AuditEvent{
		EventType:    "guest_access_key.update",
		ResourceType: "GuestAccessKey",
		ResourceID:   updated.ID,
		ProjectID:    updated.ProjectID,
		Actor:        actor,
		OnBehalfOf:   onBehalf,
		Payload:      map[string]any{"name": updated.Name},
	}); err != nil {
		return nil, ports.ErrInternal
	}

	// Метки участвуют в выдаче прав по меткам, поэтому их правка обязана доехать
	// до владельца прав тем же путём, что и заведение ключа. Иначе выдача,
	// опирающаяся на метку, перестала бы совпадать с тем, что видит арендатор.
	if _, err := emitFGARegisterIntent(ctx, tx, fgaintent.EventRegister, "GuestAccessKey",
		updated.ID, updated.ProjectID, updated.Labels); err != nil {
		return nil, ports.ErrInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, wrapPgErr(err, "GuestAccessKey", id)
	}
	return updated, nil
}

// Delete снимает ключ.
//
// Ключ, привязанный к живой машине, снять НЕЛЬЗЯ — это держит ссылочная
// целостность, а не проверка здесь: проверка защищала бы только этот путь, а
// снятие мимо неё оставило бы машину со ссылкой в никуда. Отказ хранилища
// отображается в понятный ответ вызывающему.
func (r *GuestAccessKeyRepo) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.ErrInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Перечень держателей читается ДО удаления: арендатор обязан получить радиус
	// вместе с отказом, а не выяснять его перебором. Ссылочная целостность
	// отвергла бы удаление и сама, но её отказ называет ограничение, а не машины
	// — по нему нельзя сделать ни одного следующего шага.
	holders, truncated, err := instancesHoldingGuestKey(ctx, tx, id, maxNamedHolders)
	if err != nil {
		return err
	}
	if len(holders) > 0 {
		return &ErrGuestKeyInUse{KeyID: id, InstanceIDs: holders, Truncated: truncated}
	}

	var projectID string
	err = tx.QueryRow(ctx, `DELETE FROM guest_access_keys WHERE id = $1 RETURNING project_id`, id).Scan(&projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: GuestAccessKey %s not found", ports.ErrNotFound, id)
		}
		// Ссылочная целостность остаётся ЗАДНИМ рубежом, а не запасным путём:
		// перечень выше читается в этой же транзакции, поэтому привязка,
		// возникшая между чтением и удалением, всё равно не пропустит удаление.
		return wrapPgErr(err, "GuestAccessKey", id)
	}

	actor, onBehalf := auditPrincipals(ctx)
	if err := emitAudit(ctx, tx, AuditEvent{
		EventType:    "guest_access_key.delete",
		ResourceType: "GuestAccessKey",
		ResourceID:   id,
		ProjectID:    projectID,
		Actor:        actor,
		OnBehalfOf:   onBehalf,
	}); err != nil {
		return ports.ErrInternal
	}

	// Снятие регистрации — та же транзакция, что и удаление строки. Без него
	// право на снятый ключ пережило бы сам ключ: у владельца прав остался бы
	// объект, которого больше нет, и следующий ключ с тем же идентификатором
	// (его не будет — идентификаторы не переиспользуются) не понадобился бы,
	// чтобы это стало неверным — неверно оно уже сейчас.
	if _, err := emitFGARegisterIntent(ctx, tx, fgaintent.EventUnregister, "GuestAccessKey",
		id, projectID, nil); err != nil {
		return ports.ErrInternal
	}
	return tx.Commit(ctx)
}
