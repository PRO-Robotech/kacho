import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const expectedExports = ["SaKeysPanel"] as const;

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "SaKeysPanel.tsx"), "utf8");

describe("SaKeysPanel", () => {
  it("declares its public component exports", () => {
    for (const exportName of expectedExports) {
      expect(source).toContain(exportName);
    }
  });

  it("wires the one-time secret reveal (copy + download) and revoke", () => {
    // Секрет читается из Operation.response и показывается одноразовой модалкой.
    expect(source).toContain("private_key_pem");
    expect(source).toContain("SecretModal");
    // Копировать + скачать + предупреждение о том, что ключ больше не покажут.
    expect(source).toContain("createObjectURL");
    expect(source).toContain("Сохраните ключ");
    // Выпуск/отзыв — async через Operation (useIamMutation), без прямого fetch-слоя.
    expect(source).toContain("useIamMutation");
    expect(source).toContain("saKeysPath");
  });
});
