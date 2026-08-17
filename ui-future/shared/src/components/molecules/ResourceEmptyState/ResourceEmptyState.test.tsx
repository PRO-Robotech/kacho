// Пустой список ресурса. Экран, который просто пуст, неотличим от «данные не
// загрузились» и не говорит, что делать. Поэтому существенны: призыв к
// действию, названный ресурсом вызывающего; собственный текст ресурса, когда он
// есть, и общий — когда нет; и отсутствие блока документации у ресурса, для
// которого ссылок не объявлено (пустая рамка «Документация» обещает то, чего
// нет).

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { REGISTRY, type ResourceSpec } from "@shared/lib/resource-registry";
import { ResourceEmptyState } from "./ResourceEmptyState";

function specWith(over: Partial<ResourceSpec>): ResourceSpec {
  return { ...REGISTRY["subnets"], ...over };
}

describe("ResourceEmptyState", () => {
  it("зовёт создать ресурс его же именем", () => {
    const onCreate = jest.fn();
    render(<ResourceEmptyState spec={REGISTRY["subnets"]} onCreate={onCreate} />);

    const cta = screen.getByRole("button", { name: `Создать ${REGISTRY["subnets"].singular.toLowerCase()}` });
    fireEvent.click(cta);

    expect(onCreate).toHaveBeenCalledTimes(1);
  });

  it("ресурс без создания призыва создать НЕ показывает", () => {
    // Репозиторий материализуется `docker push`, тип диска и машины заводит
    // администратор облака — глагола создания у них нет вовсе. Кнопка,
    // предлагающая действие, которого не существует, обещает возможность:
    // нажавший получает отказ края там, где консоль звала его сама.
    // Замер: `ops.create:false` объявлен у восьми ресурсов общего реестра.
    render(<ResourceEmptyState spec={specWith({ ops: { create: false, update: false, delete: false } })} onCreate={() => {}} />);

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("ресурс С созданием призыв показывает (положительный контроль)", () => {
    // Без этой пары отрицание выше зеленело бы и на компоненте, который не
    // показывает кнопку никогда.
    render(<ResourceEmptyState spec={specWith({ ops: { create: true, update: true, delete: true } })} onCreate={() => {}} />);

    expect(screen.getByRole("button")).toBeInTheDocument();
  });

  it("подпись кнопки можно переопределить", () => {
    render(<ResourceEmptyState spec={REGISTRY["subnets"]} onCreate={() => {}} createLabel="Добавить подсеть" />);
    expect(screen.getByRole("button", { name: "Добавить подсеть" })).toBeInTheDocument();
  });

  it("показывает собственный текст ресурса, когда он объявлен", () => {
    render(
      <ResourceEmptyState
        spec={specWith({ emptyState: { title: "Подсетей ещё нет", body: "Подсеть делит адресное пространство сети." } })}
        onCreate={() => {}}
      />,
    );

    expect(screen.getByText("Подсетей ещё нет")).toBeInTheDocument();
    expect(screen.getByText("Подсеть делит адресное пространство сети.")).toBeInTheDocument();
  });

  it("без собственного текста даёт общий, а не пустой заголовок", () => {
    render(<ResourceEmptyState spec={specWith({ emptyState: undefined })} onCreate={() => {}} />);
    expect(
      screen.getByText(`Создайте первый ресурс «${REGISTRY["subnets"].singular.toLowerCase()}»`),
    ).toBeInTheDocument();
  });

  it("блок документации появляется только там, где ссылки объявлены", () => {
    const bare = render(<ResourceEmptyState spec={specWith({ emptyState: undefined })} onCreate={() => {}} />);
    expect(screen.queryByText("Документация")).not.toBeInTheDocument();
    bare.unmount();

    render(
      <ResourceEmptyState
        spec={specWith({ emptyState: { title: "t", body: "b", docs: ["Что такое подсеть"] } })}
        onCreate={() => {}}
      />,
    );
    expect(screen.getByText("Документация")).toBeInTheDocument();
    expect(screen.getByText("Что такое подсеть")).toBeInTheDocument();
  });

  it("пустой перечень ссылок блок не заводит", () => {
    // Рамка «Документация» без единой ссылки обещает то, чего нет.
    render(<ResourceEmptyState spec={specWith({ emptyState: { title: "t", body: "b", docs: [] } })} onCreate={() => {}} />);
    expect(screen.queryByText("Документация")).not.toBeInTheDocument();
  });
});
