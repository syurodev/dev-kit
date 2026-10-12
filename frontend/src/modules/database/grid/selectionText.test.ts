import { expect, test } from "bun:test";
import { selectionAsTSV, serializeSelection } from "./rowText";
test("copies a complete row in display order, preserving null as an empty field", () => {
  expect(selectionAsTSV([["1", "Ada", null]], [1, 2, 0], { top: 0, bottom: 0, left: 0, right: 2 })).toBe("Ada\t\t1");
});
test("copies a rectangular range and quotes embedded TSV separators", () => {
  expect(selectionAsTSV([["0", "skip"], ["a\tb", 'say "hi"'], ["a\nb", null]], [0, 1], { top: 1, bottom: 2, left: 0, right: 1 })).toBe('"a\tb"\t"say ""hi"""\n"a\nb"\t');
});

test("JSON copies selected rows and columns as export objects, preserving SQL NULL", () => {
  const result = { columns: ["id", "name", "note"], rows: [["1", "Ada", null], ["2", "Grace", "x"]], rowCount: 2, truncated: false };
  expect(serializeSelection(result, [2, 1, 0], { top: 0, bottom: 0, left: 0, right: 1 }, "json")).toBe('[\n  {\n    "note": null,\n    "name": "Ada"\n  }\n]');
  expect(JSON.parse(serializeSelection(result, [0, 1, 2], { top: 0, bottom: 1, left: 0, right: 2 }, "json"))).toEqual([{ id: "1", name: "Ada", note: null }, { id: "2", name: "Grace", note: "x" }]);
});

test("CSV selection uses export headers and escaping in displayed order", () => {
  const result = { columns: ["id", "name"], rows: [["1", "skip"], ["2", 'a,"b"']], rowCount: 2, truncated: false };
  expect(serializeSelection(result, [1, 0], { top: 1, bottom: 1, left: 0, right: 1 }, "csv")).toBe('name,id\r\n"a,""b""",2');
});


test("TSV treats formula-leading database strings as text and preserves signed numbers", () => {
  const rows = [["=1+1", "+SUM(A1)", "-cmd", "@SUM(A1)", "  =2+2", "-42", "+3.5", "-1.2e-3"]];
  expect(selectionAsTSV(rows, [0,1,2,3,4,5,6,7], {top:0,bottom:0,left:0,right:7})).toBe("'=1+1\t'+SUM(A1)\t'-cmd\t'@SUM(A1)\t'  =2+2\t-42\t+3.5\t-1.2e-3");
});
