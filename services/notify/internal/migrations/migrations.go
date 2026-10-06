// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrations встраивает goose-миграции базы шлюза notify (база
// kacho_notify): сетка на адресата, суточный потолок потока и ограда ключа
// сетки (замысел NTF-1 З24, §6). Применённую миграцию не правят — только новая
// (ban #5).
//
// Цепочку встраивает импортом точка наката каталога
// services/notify/cmd/migrator; таблица «имя базы → каталог» —
// services/notify/cmd/migrator/chains.yaml (строка kacho_notify).
package migrations

import "embed"

// FS — встроенные миграции шлюза notify (формат goose).
//
//go:embed *.sql
var FS embed.FS
