// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package errors — чистые sentinel-ошибки, разделяемые между use-case-слоем
// (internal/apps/kacho/api/*) и adapter'ами (internal/repo, internal/clients).
// Leaf-пакет: НЕ импортирует pgx / grpc (только stdlib errors), поэтому его
// безопасно тянет use-case, не втягивая Postgres-драйвер в dependency-closure.
// Трансляция SQLSTATE→sentinel (зависит от pgx) живёт в repo-adjacent adapter,
// gRPC-статус → в internal/apps/kacho/shared/serviceerr.
//
// Порты (интерфейсы) здесь НЕ живут и никогда не жили: каждый порт объявляет
// сам use-case, который им пользуется (`volume.Reader`/`Writer`/`GeoClient`/
// `IAMClient`, `image.*`, `snapshot.Repo`, `disktype.Repo`) — так же, как у vpc и
// nlb. Прежнее имя пакета (`ports`) обещало обратное.
package errors

import "errors"

// Sentinel-ошибки adapter/use-case-слоя. Сырой pgx/gRPC-текст наружу не утекает —
// handler маппит их в фиксированный gRPC-код через serviceerr.ToStatus.
var (
	// ErrNotFound — строки не существует (pgx.ErrNoRows).
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists — нарушение UNIQUE / PK (SQLSTATE 23505).
	ErrAlreadyExists = errors.New("already exists")
	// ErrFailedPrecondition — нарушение FK / конфликт состояния (SQLSTATE 23503/CAS).
	ErrFailedPrecondition = errors.New("failed precondition")
	// ErrInvalidArg — нарушение CHECK / доменной валидации (SQLSTATE 23514).
	ErrInvalidArg = errors.New("invalid argument")
	// ErrInternal — некатегоризированная ошибка БД (без утечки pgx-текста).
	ErrInternal = errors.New("internal database error")
	// ErrUnimplemented — путь ещё не реализован. Единственный носитель —
	// `Volume.GetInternal` (infra-проекция, ждёт data-plane): осознанный
	// out-of-scope, а не заглушка скелета.
	ErrUnimplemented = errors.New("not implemented")

	// ErrResourceExhausted — предел исчерпан: провизионированный ОБЪЁМ проекта
	// либо ёмкость бэкенда.
	//
	// Полоса отдельная от «предусловие не выполнено» намеренно. Исчерпание — не
	// свойство запроса и не состояние ресурса: тот же запрос пройдёт, как только
	// освободится место, и вызывающему нечего в нём чинить. Схлопнув их в один код,
	// мы отправили бы его править ввод, который верен.
	ErrResourceExhausted = errors.New("resource exhausted")

	// ErrQuotaExceeded — потолок на ЧИСЛО ресурсов этого вида у проекта достигнут.
	//
	// Отдельный sentinel, а не переиспользование `ErrResourceExhausted`, хотя
	// gRPC-код у них один. Оси две и они не смешиваются (V2-11): объём считается
	// по самому ресурсу и меняется вместе с его размером, число — вставками
	// строк. Слив их в один sentinel, мы потеряли бы возможность приклеить
	// РАЗНЫЕ машинные признаки: клиент, читающий `QUOTA_EXCEEDED`, знает, что
	// удалить лишний ресурс поможет, а уменьшить размер — нет.
	//
	// Текст приходит из единственного производителя в базе и является частью
	// контракта: он называет носителя, предел и вид, поэтому здесь не
	// переписывается.
	ErrQuotaExceeded = errors.New("resource count quota exceeded")

	// ErrQuotaNotProvisioned — потолок на число ресурсов не назван НИ НА ОДНОЙ
	// области видимости.
	//
	// Это отказ, а не разрешение, и он НАМЕРЕННО отличим от ErrQuotaExceeded.
	// Свести их к одному коду значило бы послать администратора искать, что
	// понизить, там, где ничего не назначено. Обратная трактовка («нет строки ⇒
	// без предела») уже живёт в дереве у прецедента compute и измерена как
	// механизм, не отказавший ни разу за всю свою жизнь: строку предела там не
	// пишет ни один путь прод-кода.
	//
	// Маппится в `FAILED_PRECONDITION` (400) — ввод арендатора корректен, не
	// выполнено предусловие ПЛАТФОРМЫ, поэтому `INVALID_ARGUMENT` обвинил бы
	// вызывающего в том, чего он не присылал.
	ErrQuotaNotProvisioned = errors.New("resource count quota not provisioned")
)
