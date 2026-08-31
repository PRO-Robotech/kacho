// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package ports

import (
	"errors"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
)

// Sentinel-ошибки слоя use-case/repo. Живут здесь (в leaf-пакете ports), а не в
// use-case-пакете, чтобы общий test-helper `internal/ports/portmock` мог их
// возвращать, не завися от use-case (иначе — import-cycle с white-box тестами
// `apps/kacho/api/<resource>`). `apps/kacho/shared/serviceerr` ре-экспортирует их
// через `var`-alias'ы (`var ErrNotFound = ports.ErrNotFound` — тот же
// error-value, так что `errors.Is` работает прозрачно).
var (
	// ErrNotFound возвращается, когда ресурс не найден.
	ErrNotFound = errors.New("not found")

	// ErrPeerValidationSkipped — маркер Noop-peer'а при
	// KACHO_COMPUTE_SKIP_PEER_VALIDATION=true (dev/test): cross-service проверка
	// НЕ выполнялась. Use-case пропускает такую проверку явно — тем же
	// задокументированным dev-послаблением, что NoopGeoClient («любая зона
	// существует»). В любом развёрнутом стенде флаг выключен, и это никогда не
	// возвращается.
	ErrPeerValidationSkipped = errors.New("peer validation skipped")
	// ErrAlreadyExists возвращается при нарушении UNIQUE constraint.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInvalidArg возвращается при некорректных входных данных.
	ErrInvalidArg = errors.New("invalid argument")
	// ErrFailedPrecondition возвращается, когда операция отклонена из-за
	// состояния ресурса (например, удаление Disk пока он attached — нарушение
	// FK 23503). Маппится в gRPC FailedPrecondition.
	ErrFailedPrecondition = errors.New("failed precondition")
	// ErrInternal — generic-ошибка для неклассифицированных DB-проблем.
	// Маппится на gRPC Internal с фиксированным сообщением (no leak).
	ErrInternal = errors.New("internal database error")

	// ErrQuotaExceeded — место кончилось: потолок назван и выбран.
	// Маппится в gRPC ResourceExhausted (край даёт 429), признак
	// `QUOTA_EXCEEDED`. Администратору требуется ПОДНЯТЬ предел.
	//
	// Текст ОДИН на шесть владельцев учёта и потому произносится дословно так же,
	// как у остальных пяти. Он не украшение: им и снимается префикс на пути
	// наружу, поэтому расхождение словаря делает шесть мапперов несравнимыми, а
	// короткая форма вдобавок совпадает с чужими сообщениями о переполнении в
	// смежных подсистемах. Клиенту текст не виден — префикс снимается, — и
	// именно поэтому расхождение было тихим.
	ErrQuotaExceeded = errors.New("resource count quota exceeded")

	// ErrQuotaNotProvisioned — потолок не назван НИ НА ОДНОЙ области.
	// Маппится в gRPC FailedPrecondition (край даёт 400), признак
	// `QUOTA_NOT_PROVISIONED`. Администратору требуется ЗАВЕСТИ предел.
	//
	// Почему не ErrInvalidArg: ввод арендатора корректен, не выполнено
	// предусловие ПЛАТФОРМЫ. InvalidArgument обвинил бы вызывающего в том, чего
	// он не присылал.
	//
	// Почему это отдельный sentinel, а не оттенок ErrQuotaExceeded: причины
	// разные, и различать их обязан клиент — машинно, по признаку в
	// `google.rpc.ErrorInfo`, а не разбором прозы.
	//
	// Текст — общий словарь шести владельцев, см. довод у ErrQuotaExceeded.
	ErrQuotaNotProvisioned = errors.New("resource count quota not provisioned")
)

// QuotaCarrierProject — носитель учёта «проект».
//
// Все три вида compute считаются в проекте (каталог S1 объявляет носителя рядом
// с видом). Носитель назван константой, а не литералом на месте: ключ учёта —
// тройка, и вторая ось (предел на родителя) добавляет носителей, а не таблицу.
const QuotaCarrierProject = "project"

// QuotaRow — строка учёта в форме, пригодной для материализации.
//
// Живёт здесь, в leaf-пакете, по той же причине, что и sentinel'ы выше: её
// называют и use-case (совещательная полоса), и adapter (запись в хранилище), и
// клиент соседа. Положи её в любой из них — и два других станут зависеть от
// чужого слоя ради одной структуры.
type QuotaRow struct {
	CarrierType   string
	CarrierID     string
	Kind          string
	Limit         int64
	SourceScope   string
	SourceScopeID string
	LimitRevision int64
	AccountID     string
}

// ResolvedLimit — разрешённая величина по одному виду, как её отдаёт владелец.
//
// ПСЕВДОНИМ, а не своя структура. Ровно та же тройка полей была объявлена в пяти
// доменах порознь: пять деклараций компилировались одинаково и разошлись бы при
// первом же новом поле — добавленном одному владельцу, а нужном всем. Общий
// экземпляр вдобавок делает клиента этого домена пригодным полосе чтения
// (`quotaread.Band`) БЕЗ переходника, а переходник — это ровно то место, где
// снисходительность к чужому ответу заводится незамеченной.
//
// Область-победитель едет вместе со значением: без неё арендатор не отличит
// поднятое лично ему от общего правила, а дельта не поймёт, какие строки
// трогать при изменении широкой величины.
type ResolvedLimit = quotaread.ResolvedLimit
