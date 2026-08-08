// Редактор JSON, которым правят тела запросов вручную. Его единственное
// содержательное свойство — разбор ввода: он обязан ОТДАВАТЬ вызывающему любой
// текст (иначе нельзя допечатать до валидного) и при этом показывать, что текст
// сейчас не разбирается. Молчаливый редактор отправил бы битое тело на край.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { JsonEditor } from "./JsonEditor";

function editor(): HTMLTextAreaElement {
  return screen.getByRole("textbox");
}

describe("JsonEditor", () => {
  it("отдаёт введённый текст вызывающему", () => {
    const onChange = jest.fn<(v: string) => void>();
    render(<JsonEditor value="" onChange={onChange} />);

    fireEvent.change(editor(), { target: { value: '{"a":1}' } });

    expect(onChange).toHaveBeenCalledWith('{"a":1}');
  });

  it("на валидном JSON ошибки не показывает", () => {
    render(<JsonEditor value="" onChange={() => {}} />);

    fireEvent.change(editor(), { target: { value: '{"a":1}' } });

    expect(screen.queryByText(/^JSON:/)).not.toBeInTheDocument();
    expect(editor().className).not.toContain("ring-rose-500");
  });

  it("на неразбираемом тексте показывает причину и подсвечивает поле", () => {
    render(<JsonEditor value="" onChange={() => {}} />);

    fireEvent.change(editor(), { target: { value: "{не json" } });

    expect(screen.getByText(/^JSON:/)).toBeInTheDocument();
    expect(editor().className).toContain("ring-rose-500");
  });

  it("не глотает ввод, который пока не разбирается", () => {
    // Пока пользователь допечатывает объект, текст невалиден почти всё время;
    // редактор, отдающий только валидное, набрать объект не даёт вовсе.
    const onChange = jest.fn<(v: string) => void>();
    render(<JsonEditor value="" onChange={onChange} />);

    fireEvent.change(editor(), { target: { value: '{"a":' } });

    expect(onChange).toHaveBeenCalledWith('{"a":');
    expect(screen.getByText(/^JSON:/)).toBeInTheDocument();
  });

  it("снимает ошибку, когда текст стал валидным", () => {
    render(<JsonEditor value="" onChange={() => {}} />);

    fireEvent.change(editor(), { target: { value: "{" } });
    expect(screen.getByText(/^JSON:/)).toBeInTheDocument();

    fireEvent.change(editor(), { target: { value: "{}" } });
    expect(screen.queryByText(/^JSON:/)).not.toBeInTheDocument();
  });

  it("пустой текст ошибкой не считает", () => {
    render(<JsonEditor value="" onChange={() => {}} />);

    fireEvent.change(editor(), { target: { value: "{" } });
    fireEvent.change(editor(), { target: { value: "   " } });

    expect(screen.queryByText(/^JSON:/)).not.toBeInTheDocument();
  });

  it("показывает подсказку и заданную высоту", () => {
    render(<JsonEditor value="" onChange={() => {}} rows={4} placeholder="{ }" />);
    expect(editor()).toHaveAttribute("rows", "4");
    expect(editor()).toHaveAttribute("placeholder", "{ }");
  });
});
