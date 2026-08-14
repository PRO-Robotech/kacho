// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build tools

// Package tools пинит версии code-generation плагинов через go.mod.
//
// Зачем. Плагины protoc-gen-* ОПРЕДЕЛЯЮТ содержимое pkg/api. Если ставить их
// `go install …@latest`, версия у разработчика и в CI разъезжается по времени, а
// гейт `generate-diff` начинает мигать: локально стабы одни, в CI — другие, и никто
// не менял ни строчки .proto. Отладка такого «фантомного диффа» стоит дороже, чем
// сама фича.
//
// Здесь плагины импортируются как обычные зависимости → `go mod tidy` фиксирует их
// версии в go.mod/go.sum. Объявление генерации (proto/buf.gen.yaml) зовёт каждый
// плагин через `go run <пакет>`:
//
//	local: [go, run, google.golang.org/protobuf/cmd/protoc-gen-go]
//
// — то есть версию выбирает go.mod В МОМЕНТ ВЫЗОВА, а не то, что оказалось в PATH.
// Local == CI по построению, а не по договорённости.
//
// ПОЧЕМУ НЕ «go install без @latest». Установка кладёт свою копию в PATH, но не
// отменяет чужую, стоящую раньше: вердикт снова становится свойством машины, и
// разойтись две копии могут молча — обе отвечают «сгенерил». Форма `go run` тем и
// хороша, что обойти её по невнимательности нельзя. Класс держит гейт
// internal/repohygiene TestGeneratorPluginsArePinned.
//
// Версии плагинов при этом совпадают с рантайм-библиотеками, против которых
// собирается код (protobuf v1.36.11, grpc-gateway v2.30.0) — рассинхрон «стабы
// сгенерены новее рантайма» тоже исключён.
//
// Build-tag `tools` не даёт пакету попасть в обычную сборку.
package tools

import (
	_ "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway"
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
