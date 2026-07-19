// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package fgaregister описывает owner-hierarchy tuple storage-ресурса через
// transactional-outbox (SEC-D).
//
// Созданному Volume/Snapshot нужен owner-hierarchy intent, чтобы per-resource Check
// (public Get/Update/Delete, internal Attach/Detach) и gateway scope_extractor
// {storage_volume,volume_id}/{storage_snapshot,snapshot_id} разрешались. Под flat-моделью
// (Contract-A: `<rel> from project`-каскад удалён) intent несёт parent_project ресурса в
// resource_mirror iam, откуда реконсайлер МАТЕРИАЛИЗУЕТ per-object v_* owner-биндинга и
// фиксирует containment target→project для анти-BOLA-gate. Без него gate не резолвит
// target→project → owner видит DENY на свой же только что созданный ресурс.
//
// INTENT на tuple пишется outbox-строкой (kacho_storage.fga_register_outbox) в ТОЙ
// ЖЕ writer-TX, что вставляет/удаляет ресурс (один commit, без dual-write). Отдельный
// register-drainer (corelib outbox/drainer) применяет каждый intent через kacho-iam
// InternalIAMService.RegisterResource/UnregisterResource по mTLS (idempotent,
// at-least-once, retry на Unavailable — tuple durable и не теряется). Writer-сторона —
// emit в internal/repo/pg; drainer-сторона — IAM register-applier в internal/clients.
//
// Пакет — чистый domain (только stdlib): без pgx/grpc/transport-зависимостей —
// задаёт лишь форму tuple/intent, на которую опираются и repo-writer, и
// clients-applier (dependency rule, architecture.md).
package fgaregister

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Типы событий колонки event_type таблицы fga_register_outbox; передаются
// drainer-Applier'у и определяют вызов: RegisterResource (записать tuple) либо
// UnregisterResource (снять). Стабильные строки — часть on-row контракта,
// дополнительно энфорсятся DB CHECK.
const (
	// EventRegister — применить tuple через InternalIAMService.RegisterResource
	// (Create ресурса). Idempotent: повторная регистрация того же tuple → OK.
	EventRegister = "fga.register"
	// EventUnregister — снять tuple через InternalIAMService.UnregisterResource
	// (Delete ресурса). Idempotent: unregister отсутствующего tuple → OK.
	EventUnregister = "fga.unregister"
)

// FGA object-типы (парити с iam permission-catalog scope_extractor'ами: коммитнутый
// каталог адресует Volume-RPC на storage_volume, Snapshot-RPC на storage_snapshot).
// Storage их НЕ переопределяет — только эмитит tuple на эти уже существующие типы.
const (
	ObjectStorageVolume   = "storage_volume"
	ObjectStorageSnapshot = "storage_snapshot"
)

// relationProject — hierarchy-relation owner-tuple: `project:<id> #project @<obj>`.
const relationProject = "project"

// Tuple — один owner-hierarchy relationship tuple, сериализуемый payload'ом одной
// строки fga_register_outbox. Поля 1:1 с запросом InternalIAMService
// RegisterResource/UnregisterResource:
//
//	SubjectId — "project:prj-xxxxxxxxxxxxxxxxx"
//	Relation  — "project"
//	Object    — "storage_volume:vol-xxxxxxxxxxxxxxxxx"
type Tuple struct {
	SubjectID string `json:"subject_id"`
	Relation  string `json:"relation"`
	Object    string `json:"object"`
}

// Valid сообщает, полностью ли заполнен tuple. Неполный tuple — баг вызывающего:
// drainer-декодер трактует его как permanent (poison) и не ретраит.
func (t Tuple) Valid() bool {
	return t.SubjectID != "" && t.Relation != "" && t.Object != ""
}

// Item — один owner-hierarchy tuple плюс mirror-feed (labels + parent_project_id).
// Один Item == одна строка fga_register_outbox == один Payload. Используется
// синхронным Registrar'ом (immediate owner-grant) и конструкторами Intent.
type Item struct {
	Tuple           Tuple
	Labels          map[string]string
	ParentProjectID string
}

// Registrar — порт синхронной регистрации owner-tuple'ов в kacho-iam. Create-flow
// после успешного коммита ресурса синхронно регистрирует те же Item'ы, что эмитятся
// в outbox-intent, чтобы owner-grant (и анти-BOLA-резолв) был доступен сразу — без
// гонки с async register-drainer'ом. Реализация — adapter поверх
// InternalIAMService.RegisterResource (idempotent); drainer остаётся at-least-once
// backstop'ом. nil-registrar (dev/no-iam) → sync-путь пропускается, только async.
type Registrar interface {
	Register(ctx context.Context, items []Item) error
}

// Payload — JSONB-форма одной строки fga_register_outbox: tuple, развёрнутый на
// верхний уровень (back-compat с «голой» tuple-строкой), плюс mirror-feed (labels +
// parent_project_id + монотонный source_version). На эту форму опираются и
// repo-writer (emit), и clients-applier (forward в kacho-iam).
type Payload struct {
	Tuple
	// Labels — копия labels владельца-ресурса (mirror для селектора iam).
	Labels map[string]string `json:"labels,omitempty"`
	// ParentProjectID — id владеющего проекта (parent-scope / containment).
	ParentProjectID string `json:"parent_project_id,omitempty"`
	// SourceVersion — монотонный per-object маркер, штампуется от DB-часов (now())
	// на момент INSERT внутри writer-TX. Ноль (без штампа) → iam трактует как
	// -infinity (никогда не выигрывает монотонное сравнение).
	SourceVersion time.Time `json:"source_version,omitempty"`
}

// Valid сообщает, полностью ли заполнен вложенный tuple.
func (p Payload) Valid() bool { return p.Tuple.Valid() }

// Encode сериализует Payload в JSONB-payload строки fga_register_outbox.
func Encode(p Payload) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode fga register payload: %w", err)
	}
	return b, nil
}

// Decode разбирает payload одной строки fga_register_outbox в Payload.
func Decode(b []byte) (Payload, error) {
	var p Payload
	if err := json.Unmarshal(b, &p); err != nil {
		return Payload{}, fmt.Errorf("decode fga register payload: %w", err)
	}
	return p, nil
}

// ProjectHierarchy строит канонический tuple `project:<projectID> #project
// @<objectType>:<objectID>` — parent-pointer containment объект→проект, который
// iam-реконсайлер читает (через RegisterResource → resource_mirror.parent_project), чтобы
// материализовать per-object v_* на ресурсе. Под flat-моделью (Contract-A) он НЕ
// потребляется каскадом `<rel> from project` (удалён).
func ProjectHierarchy(projectID, objectType, objectID string) Tuple {
	return Tuple{
		SubjectID: "project:" + projectID,
		Relation:  relationProject,
		Object:    objectType + ":" + objectID,
	}
}

// StorageVolume — owner-tuple тома: project:<projectID> #project @storage_volume:<volumeID>.
func StorageVolume(projectID, volumeID string) Tuple {
	return ProjectHierarchy(projectID, ObjectStorageVolume, volumeID)
}

// StorageSnapshot — owner-tuple снапшота: project:<projectID> #project @storage_snapshot:<snapshotID>.
func StorageSnapshot(projectID, snapshotID string) Tuple {
	return ProjectHierarchy(projectID, ObjectStorageSnapshot, snapshotID)
}

// Item helpers -----------------------------------------------------------------

// VolumeItem — register-Item тома с mirror-feed (labels + parent_project_id=projectID).
func VolumeItem(projectID, volumeID string, labels map[string]string) Item {
	return Item{Tuple: StorageVolume(projectID, volumeID), Labels: labels, ParentProjectID: projectID}
}

// SnapshotItem — register-Item снапшота с mirror-feed.
func SnapshotItem(projectID, snapshotID string, labels map[string]string) Item {
	return Item{Tuple: StorageSnapshot(projectID, snapshotID), Labels: labels, ParentProjectID: projectID}
}
