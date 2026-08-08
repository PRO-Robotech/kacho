// Метки ресурса — управляемый редактор пар. Ценность здесь в ПЕРЕВОДЕ между
// формой хранения (объект `map<string,string>` на wire) и формой правки (список
// пар): пока пользователь печатает, ключей-дубликатов и пустых ключей полно, и
// перевод обязан не терять строки и не отправлять мусор.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import {
  LabelsEditor,
  labelsFromEntries,
  labelsFromMap,
  labelsToEntries,
  labelsToMap,
  type LabelEntry,
} from "./LabelsEditor";

describe("LabelsEditor", () => {
  it("показывает пары и отдаёт вызывающему следующий набор", () => {
    const onChange = jest.fn<(next: LabelEntry[]) => void>();
    render(
      <LabelsEditor
        value={[
          { key: "env", value: "prod" },
          { key: "team", value: "net" },
        ]}
        onChange={onChange}
      />,
    );

    expect(screen.getByDisplayValue("env")).toBeInTheDocument();
    expect(screen.getByDisplayValue("prod")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Добавить метку" }));

    expect(onChange).toHaveBeenCalledWith([
      { key: "env", value: "prod" },
      { key: "team", value: "net" },
      { key: "", value: "" },
    ]);
  });

  it("удаление отдаёт набор без удалённой пары", () => {
    const onChange = jest.fn<(next: LabelEntry[]) => void>();
    render(
      <LabelsEditor
        value={[
          { key: "env", value: "prod" },
          { key: "team", value: "net" },
        ]}
        onChange={onChange}
      />,
    );

    fireEvent.click(screen.getAllByLabelText("Удалить строку")[0]);

    expect(onChange).toHaveBeenCalledWith([{ key: "team", value: "net" }]);
  });

  it("выключенный редактор ничего не отдаёт", () => {
    const onChange = jest.fn();
    render(<LabelsEditor value={[{ key: "env", value: "prod" }]} onChange={onChange} disabled />);

    fireEvent.click(screen.getByRole("button", { name: "Добавить метку" }));

    expect(onChange).not.toHaveBeenCalled();
  });
});

describe("перевод между картой и списком пар", () => {
  it("карта разворачивается в пары и сворачивается обратно", () => {
    const entries = labelsToEntries({ env: "prod", team: "net" });
    expect(entries).toEqual([
      { key: "env", value: "prod" },
      { key: "team", value: "net" },
    ]);
    expect(labelsFromEntries(entries)).toEqual({ env: "prod", team: "net" });
  });

  it("отсутствующая карта — пустой список, а не бросок", () => {
    expect(labelsToEntries(undefined)).toEqual([]);
  });

  it("строка с пустым ключом на wire не уезжает", () => {
    // Пустая строка существует, пока пользователь набирает; отправить её
    // значило бы попросить сервер завести метку без имени.
    expect(labelsFromEntries([{ key: "", value: "x" }, { key: "  ", value: "y" }])).toEqual({});
  });

  it("ключ обрезается по краям, значение — нет", () => {
    // Ключ — идентификатор, пробелы по краям в нём случайны. Значение
    // принадлежит пользователю целиком.
    expect(labelsFromEntries([{ key: "  env  ", value: "  prod  " }])).toEqual({ env: "  prod  " });
  });

  it("последняя пара с тем же ключом побеждает — как в карте", () => {
    // Дубликат ключа в списке возможен, в карте — нет; правило разрешения
    // обязано быть определённым, а не «как получится».
    expect(
      labelsFromEntries([
        { key: "env", value: "prod" },
        { key: "env", value: "stage" },
      ]),
    ).toEqual({ env: "stage" });
  });

  it("исторические имена указывают на те же функции", () => {
    // Оба имени живы у вызывающих; разъехавшись, они дали бы два разных
    // перевода одного предмета.
    expect(labelsFromMap).toBe(labelsToEntries);
    expect(labelsToMap).toBe(labelsFromEntries);
  });
});
