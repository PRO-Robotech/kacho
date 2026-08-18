// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "strings"

// FieldViolation — нарушение валидации одного поля ресурса: имя поля + человеко-
// читаемое описание. Чистый stdlib-тип (без gRPC/corelib) — domain не зависит от
// transport-слоя (domain — только stdlib + proto-типы).
// Транслируется в `errdetails.BadRequest_FieldViolation`
// слоем serviceerr (`FromValidation`), сохраняя прежний wire-контракт.
//
// Утверждение проверяемо, а не декларативно:
// `go list -deps ./services/vpc/internal/domain | grep google.golang.org/grpc` —
// пусто. Оно НЕ было верным те несколько часов, пока форма имени бралась из
// `pkg/validate`: единственное объявление формы и stdlib-чистый домен были
// недостижимы одновременно. Разрешилось выносом формы в `pkg/validate/nameform`
// — пакет без транспорта. Урок общий: «одно объявление» и «чистый слой» спорят
// только тогда, когда объявление живёт не в том пакете.
type FieldViolation struct {
	Field string
	Msg   string
}

// ValidationError — доменная ошибка валидации, несущая один или несколько
// FieldViolation. Реализует обычный `error` (stdlib) — gRPC InvalidArgument с
// BadRequest-details строится отдельно в serviceerr.FromValidation. Так domain
// не тянет grpc/codes+status, а трансляция в gRPC — единая точка в use-case-слое.
type ValidationError struct {
	Violations []FieldViolation
}

// Error реализует интерфейс error. Сообщение — соединение field: msg по всем
// нарушениям (для логов / errors.Is-цепочек); внешний клиент видит не его, а
// gRPC-статус из serviceerr.FromValidation.
func (e *ValidationError) Error() string {
	if e == nil || len(e.Violations) == 0 {
		return "validation error"
	}
	parts := make([]string, 0, len(e.Violations))
	for _, v := range e.Violations {
		parts = append(parts, v.Field+": "+v.Msg)
	}
	return strings.Join(parts, "; ")
}

// newValidationError — единичная ошибка валидации одного поля. Удобный
// конструктор для leaf-validator'ов (RcNameVPC.Validate и т.п.).
func newValidationError(field, msg string) *ValidationError {
	return &ValidationError{Violations: []FieldViolation{{Field: field, Msg: msg}}}
}

// combineValidation сливает несколько (возможно nil) `*ValidationError` в один,
// сохраняя порядок FieldViolation. Возвращает интерфейс `error`, равный
// нетипизированному nil, когда нарушений нет (важно: возврат типизированного
// `(*ValidationError)(nil)` ломал бы `err != nil`-проверки у вызывающих).
//
// Дает единый типизированный результат — одну InvalidArgument с полным набором
// violation'ов после трансляции (не несколько разрозненных grpc-status, которые
// при >1 ошибке деградируют в garbled message).
//
// Ошибка ЧУЖОГО типа возвращается КАК ЕСТЬ и обрывает слияние. Прежняя редакция
// такую ошибку молча отбрасывала — и всякая проверка, вернувшая не наш тип,
// становилась no-op: она исполнялась на каждом запросе и не отвергала ничего.
//
// Класс перестал быть теоретическим на несколько часов (#715): проверка имени
// временно возвращала готовый транспортный статус, и совокупная `Validate()`
// ВОСЬМИ ресурсов принимала негодное имя, оставаясь зелёной по всем прежним
// пробам. Сегодня имя снова возвращает наш тип (форма приехала из `nameform`,
// без транспорта), поэтому ЭТУ ветку путь имени больше НЕ исполняет.
//
// Ветка тем не менее остаётся, и это не «на всякий случай»: дефект был
// настоящим и невидимым — следующая проверка, вернувшая чужой тип, воскресила бы
// его молча. Держит её `combine_validation_test.go`.
//
// Обрыв на первой чужой ошибке означает, что нарушения, накопленные до неё, в
// ответ не попадут. Цена названа и принята: «отвергнуть по одной причине» всегда
// лучше, чем «принять негодное».
func combineValidation(errs ...error) error {
	var all []FieldViolation
	for _, err := range errs {
		if err == nil {
			continue
		}
		ve, ok := err.(*ValidationError)
		if !ok {
			return err
		}
		all = append(all, ve.Violations...)
	}
	if len(all) == 0 {
		return nil
	}
	return &ValidationError{Violations: all}
}
