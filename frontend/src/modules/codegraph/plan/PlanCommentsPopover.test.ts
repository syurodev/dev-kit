import { describe, expect, it } from "bun:test";
import { isMarkupParagraph } from "./PlanCommentsPopover";

describe("isMarkupParagraph", () => {
  it("recognises an unfenced multi-line XML snippet", () => {
    const changeset = [
      '<changeSet id="000000000005_alter_program_config_auth_add_platform_client_id" author="...">',
      '    <addColumn tableName="program_config_auth">',
      '        <column name="platform_client_id" type="VARCHAR(255)"/>',
      "    </addColumn>",
      "</changeSet>",
    ].join("\n");
    expect(isMarkupParagraph(changeset)).toBe(true);
  });

  it("leaves a sentence that mentions a tag in passing as prose", () => {
    const sentence =
      'Phải thêm <include file="/changelog/000000000005_....xml"/> vào changelog-main.xml — đặt sau dòng 000000000004.';
    expect(isMarkupParagraph(sentence)).toBe(false);
  });

  it("leaves ordinary prose alone", () => {
    expect(isMarkupParagraph("1. Security: app/application.yml có `filter.authorization-urls`.")).toBe(false);
  });

  it("needs more than one line: a single tag alone is not a snippet worth its own block", () => {
    expect(isMarkupParagraph('<addColumn tableName="x">')).toBe(false);
  });
});
