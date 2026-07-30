// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// ---- MachineTypeRepo (COMP-1 F7) ----

// MachineTypeRepo — реализация ports.MachineTypeRepo поверх pgxpool. Каталог
// machine-type: public read (ambient) + admin CRUD (InternalMachineTypeService).
// effective_resources распакованы в скалярные колонки (v_cpu/memory_mib/gpus/
// gpu_type), чтобы minGpus= был индексируемым предикатом; available_zones — TEXT[]
// (native pgx array); labels — JSONB.
type MachineTypeRepo struct {
	pool *pgxpool.Pool
}

// NewMachineTypeRepo создаёт MachineTypeRepo.
func NewMachineTypeRepo(pool *pgxpool.Pool) *MachineTypeRepo { return &MachineTypeRepo{pool: pool} }

const machineTypeCols = `id, name, description, family, v_cpu, memory_mib, gpus, gpu_type, available_zones, status, labels, created_at`

// scanMachineType сканирует одну строку machine_types в domain.MachineType.
func scanMachineType(row scannable) (*domain.MachineType, error) {
	var mt domain.MachineType
	var family, status int32
	var labelsJSON []byte
	if err := row.Scan(
		&mt.ID, &mt.Name, &mt.Description, &family,
		&mt.EffectiveResources.VCPU, &mt.EffectiveResources.MemoryMiB, &mt.EffectiveResources.GPUs, &mt.EffectiveResources.GPUType,
		&mt.AvailableZones, &status, &labelsJSON, &mt.CreatedAt,
	); err != nil {
		return nil, err
	}
	mt.Family = domain.MachineTypeFamily(family)
	mt.Status = domain.MachineTypeStatus(status)
	if err := unmarshalJSONB(labelsJSON, &mt.Labels, "MachineType.labels"); err != nil {
		return nil, err
	}
	return &mt, nil
}

// Get возвращает machine-type по id.
func (r *MachineTypeRepo) Get(ctx context.Context, id string) (*domain.MachineType, error) {
	q := fmt.Sprintf(`SELECT %s FROM machine_types WHERE id = $1`, machineTypeCols)
	mt, err := scanMachineType(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, wrapPgErr(err, "MachineType", id)
	}
	return mt, nil
}

// List возвращает machine-type с cursor-пагинацией (created_at, id) ASC и
// whitelist-фильтрами name=/family=/minGpus= (COMP-1 F7/F19). Ambient каталог —
// per-object list-authz к нему не применяется.
func (r *MachineTypeRepo) List(ctx context.Context, f ports.MachineTypeFilter, p ports.Pagination) ([]*domain.MachineType, string, error) {
	pageSize, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	var args []any
	var conditions []string
	argIdx := 1
	if f.Name != "" {
		conditions = append(conditions, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, f.Name)
		argIdx++
	}
	if f.Family != domain.MachineTypeFamilyUnspecified {
		conditions = append(conditions, fmt.Sprintf("family = $%d", argIdx))
		args = append(args, int32(f.Family))
		argIdx++
	}
	if f.MinGPUs > 0 {
		conditions = append(conditions, fmt.Sprintf("gpus >= $%d", argIdx))
		args = append(args, f.MinGPUs)
		argIdx++
	}
	if p.PageToken != "" {
		tsv, id, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", invalidPageTokenErr(derr)
		}
		conditions = append(conditions, fmt.Sprintf("(created_at, id) > ($%d, $%d)", argIdx, argIdx+1))
		args = append(args, tsv, id)
		argIdx += 2
	}
	var where string
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	q := fmt.Sprintf(`SELECT %s FROM machine_types %s ORDER BY created_at ASC, id ASC LIMIT $%d`, machineTypeCols, where, argIdx)
	args = append(args, pageSize+1)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", wrapPgErr(err, "MachineType", "")
	}
	defer rows.Close()
	var out []*domain.MachineType
	for rows.Next() {
		mt, serr := scanMachineType(rows)
		if serr != nil {
			return nil, "", wrapPgErr(serr, "MachineType", "")
		}
		out = append(out, mt)
	}
	if err := rows.Err(); err != nil {
		return nil, "", wrapPgErr(err, "MachineType", "")
	}
	var next string
	if int64(len(out)) > pageSize {
		last := out[pageSize-1]
		next = encodePageToken(last.CreatedAt, last.ID)
		out = out[:pageSize]
	}
	return out, next, nil
}

// Insert вставляет machine-type (admin-only). UNIQUE(name) на DB-уровне →
// 23505 → wrapPgErr → ports.ErrAlreadyExists.
func (r *MachineTypeRepo) Insert(ctx context.Context, mt *domain.MachineType) (*domain.MachineType, error) {
	labelsJSON, err := marshalJSONB(orEmptyMap(mt.Labels), "MachineType.labels")
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`INSERT INTO machine_types (id, name, description, family, v_cpu, memory_mib, gpus, gpu_type, available_zones, status, labels, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING %s`, machineTypeCols)
	created, err := scanMachineType(r.pool.QueryRow(ctx, q,
		mt.ID, mt.Name, mt.Description, int32(mt.Family),
		mt.EffectiveResources.VCPU, mt.EffectiveResources.MemoryMiB, mt.EffectiveResources.GPUs, mt.EffectiveResources.GPUType,
		orEmptySlice(mt.AvailableZones), int32(mt.Status), labelsJSON, time.Now().UTC()))
	if err != nil {
		return nil, wrapPgErr(err, "MachineType", mt.ID)
	}
	return created, nil
}

// Update применяет ТОЛЬКО названные маской колонки одним стейтментом (admin-only;
// name/id/created_at неизменяемы и в SET не попадают). RETURNING даёт финальную
// строку; 0 строк → ErrNotFound.
//
// Один стейтмент — это и есть инвариант, а не оптимизация. Раньше правка читала
// строку, сливала маску в памяти и писала ВЕСЬ прочитанный снимок обратно, поэтому
// две правки с НЕПЕРЕСЕКАЮЩИМИСЯ масками (вывести размер из эксплуатации / поменять
// метки) затирали друг друга: кто писал вторым, возвращал чужую колонку к значению
// своего снимка. Обе операции при этом завершались успехом — потеря была молчаливой,
// а на колонке состояния ещё и возвращала снятый с эксплуатации размер в оборот.
// Мутация асинхронная, поэтому двух администраторов для этого не требовалось:
// достаточно двух последовательных вызовов одного скрипта.
//
// Версии/xmin здесь не нужны: снимок в БД больше не уезжает вовсе, а конкуренция за
// ОДНУ И ТУ ЖЕ колонку остаётся честным «побеждает последний» — обе правки
// действительно называли это поле.
func (r *MachineTypeRepo) Update(ctx context.Context, id string, u ports.MachineTypeUpdate) (*domain.MachineType, error) {
	if !u.Touched() {
		// Ничего не названо — записи не требуется, но исход обязан отличать
		// существующую строку от отсутствующей.
		return r.Get(ctx, id)
	}

	set := make([]string, 0, 9)
	args := []any{id}
	add := func(col string, val any) {
		args = append(args, val)
		set = append(set, fmt.Sprintf("%s=$%d", col, len(args)))
	}

	if u.Description != nil {
		add("description", *u.Description)
	}
	if u.Family != nil {
		add("family", int32(*u.Family))
	}
	if u.EffectiveResources != nil {
		add("v_cpu", u.EffectiveResources.VCPU)
		add("memory_mib", u.EffectiveResources.MemoryMiB)
		add("gpus", u.EffectiveResources.GPUs)
		add("gpu_type", u.EffectiveResources.GPUType)
	}
	if u.AvailableZones != nil {
		add("available_zones", orEmptySlice(*u.AvailableZones))
	}
	if u.Status != nil {
		add("status", int32(*u.Status))
	}
	if u.LabelsSet {
		labelsJSON, err := marshalJSONB(orEmptyMap(u.Labels), "MachineType.labels")
		if err != nil {
			return nil, err
		}
		add("labels", labelsJSON)
	}

	q := fmt.Sprintf(`UPDATE machine_types SET %s WHERE id=$1 RETURNING %s`,
		strings.Join(set, ", "), machineTypeCols)
	updated, err := scanMachineType(r.pool.QueryRow(ctx, q, args...))
	if err != nil {
		return nil, wrapPgErr(err, "MachineType", id)
	}
	return updated, nil
}

// Delete удаляет machine-type (admin-only). 0 rows → ErrNotFound.
//
// Тип, на который ссылается хотя бы один инстанс, удалить нельзя:
// instances_machine_type_id_fkey ON DELETE RESTRICT (миграция 0017) даёт 23503 →
// ErrFailedPrecondition. Это единственный корректный способ вывести размер из
// эксплуатации — status=RETIRED (Bookable()=false), а не hard-DELETE занятой
// каталожной строки (иначе живые инстансы остались бы с dangling machineTypeId).
func (r *MachineTypeRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM machine_types WHERE id = $1`, id)
	if err != nil {
		if isFKViolation(err) {
			// Конвенционный тон вместо generic "The machinetype … is being used".
			return fmt.Errorf("%w: machine type %s is in use", ports.ErrFailedPrecondition, id)
		}
		return wrapPgErr(err, "MachineType", id)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// orEmptySlice normalises a nil slice to an empty one.
//
// The distinction matters on the wire: a nil slice marshals to `null` while an
// empty one marshals to `[]`, and a client that iterates the field should not have
// to special-case the difference between "no zones" and "no answer".
func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
