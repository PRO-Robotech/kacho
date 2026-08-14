// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Проверки конфигурации написаны здесь, а не взяты библиотекой.
//
// Причина названа, чтобы её не приняли за изобретение велосипеда: интерфейсы
// проверок живут в САМОМ каркасе, который модуль уже несёт, а готовый набор —
// в отдельной зависимости. Модуль у продукта общий, поэтому её появление вошло
// бы в граф каждого сервиса и каждой сборки образа ради полусотни строк.
//
// Второе, менее очевидное: тексты отказов у библиотеки свои и английские, а этот
// отказ читает арендатор на плане. Здесь он назван на языке продукта и говорит,
// ЧТО не так, а не какое правило сработало.

// oneOfValidator — значение из закрытого набора.
type oneOfValidator struct {
	allowed []string
}

// oneOf — проверка «значение из перечня». Пустое и неизвестное ПРОПУСКАЮТСЯ:
// обязательность — отдельная ответственность схемы (`Required`), а о неизвестном
// на этапе плана нельзя утверждать ничего. Смешать эти вопросы значило бы
// отвергать вычисляемое значение, которое ещё не получено.
func oneOf(allowed ...string) validator.String {
	return oneOfValidator{allowed: allowed}
}

func (v oneOfValidator) Description(_ context.Context) string {
	return "одно из: " + strings.Join(v.allowed, ", ")
}

func (v oneOfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v oneOfValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	for _, a := range v.allowed {
		if got == a {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Недопустимое значение",
		fmt.Sprintf("%s = %q; допустимо одно из: %s.",
			req.Path, got, strings.Join(v.allowed, ", ")))
}

// exactlyOneOfValidator — ровно один из перечисленных атрибутов задан.
type exactlyOneOfValidator struct {
	paths []path.Path
}

// exactlyOneOf — «ни одного» и «больше одного» суть разные отказы, и оба отказы.
//
// Проверка стоит у ресурса ради МОМЕНТА: несочетаемая пара видна на плане, до
// того как применение начнёт заводить что-либо ещё. Это проверка ФОРМЫ
// конфигурации, а не решение о доступе, — авторитетом остаётся край.
func exactlyOneOf(paths ...path.Path) exactlyOneOfValidator {
	return exactlyOneOfValidator{paths: paths}
}

func (v exactlyOneOfValidator) Description(_ context.Context) string {
	names := make([]string, 0, len(v.paths))
	for _, p := range v.paths {
		names = append(names, p.String())
	}
	return "задан ровно один из: " + strings.Join(names, ", ")
}

func (v exactlyOneOfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v exactlyOneOfValidator) ValidateResource(
	ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse,
) {
	v.check(ctx, req.Config, &resp.Diagnostics)
}

func (v exactlyOneOfValidator) ValidateDataSource(
	ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse,
) {
	v.check(ctx, req.Config, &resp.Diagnostics)
}

// check — общее тело обеих сторон.
//
// # О неизвестном значении проверка ВОЗДЕРЖИВАЕТСЯ
//
// Здесь стояло обратное — «неизвестное считается заданным», — и рассуждение
// звучало убедительно: значение ещё не получено, но будет. Оно неверно, и это
// показала модульная проба, а не размышление.
//
// Каркас зовёт проверку на КОНФИГУРАЦИЮ БЛОКА, один раз, ДО развёртки `for_each`.
// В блоке с `for_each` каждое поле — выражение над `each.value`, то есть
// неизвестно ВСЁ сразу. Считая неизвестное заданным, проверка находила две
// координаты там, где ни одной ещё не вычислено, и роняла план на модуле, где
// групп вообще ноль.
//
// Правило, которое из этого следует: судить можно только о том, что ИЗВЕСТНО.
//
//   - известных больше одной → отказ (мы уверены);
//   - известных ноль И неизвестных ноль → отказ (мы уверены, что не задано ничего);
//   - иначе → воздержаться: исход зависит от значения, которого ещё нет.
//
// Цена воздержания названа честно: пара «литерал + неизвестное» уедет к краю и
// получит отказ там. Это правильный размен — край остаётся авторитетом, а
// проверка на плане обязана быть верной, а не ранней.
func (v exactlyOneOfValidator) check(ctx context.Context, cfg tfsdk.Config, diags *diag.Diagnostics) {
	var known []string
	unknown := 0
	for _, p := range v.paths {
		var s types.String
		d := cfg.GetAttribute(ctx, p, &s)
		if d.HasError() {
			diags.Append(d...)
			return
		}
		switch {
		case s.IsUnknown():
			unknown++
		case !s.IsNull():
			known = append(known, p.String())
		}
	}

	names := make([]string, 0, len(v.paths))
	for _, p := range v.paths {
		names = append(names, p.String())
	}

	switch {
	case len(known) > 1:
		diags.AddError("Задано больше одной координаты",
			"Ожидался ровно один из: "+strings.Join(names, ", ")+
				". Заданы: "+strings.Join(known, ", ")+".")
	case len(known) == 0 && unknown == 0:
		diags.AddError("Не задана ни одна координата",
			"Ожидался ровно один из: "+strings.Join(names, ", ")+". Не задан ни один.")
	}
}
