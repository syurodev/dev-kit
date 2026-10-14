import { createElement, lazy } from "react";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { parsePdfInfo, parsePdfList, parsePdfPages, parsePdfSearch, parsePdfText } from "./pdfResult";

/**
 * ToolResultViews for `pdf.*` (docs/dev-kit/tool-result-view.mdx). A PDF is a
 * document of the Agent Hub session that attached it, so Agent Hub owns the
 * tools' views. Each result is checked synchronously before the card chunk
 * loads; a shape a parser cannot read falls back to the generic block.
 */
const cards = () => import("./pdf");
const LazyPdfPagesCard = lazy(() => cards().then(({ PdfPagesCard }) => ({ default: PdfPagesCard })));
const LazyPdfSearchCard = lazy(() => cards().then(({ PdfSearchCard }) => ({ default: PdfSearchCard })));
const LazyPdfTextCard = lazy(() => cards().then(({ PdfTextCard }) => ({ default: PdfTextCard })));
const LazyPdfInfoCard = lazy(() => cards().then(({ PdfInfoCard }) => ({ default: PdfInfoCard })));
const LazyPdfListCard = lazy(() => cards().then(({ PdfListCard }) => ({ default: PdfListCard })));

export function registerPdfViews(): void {
  registerToolResultView("pdf.viewPages", ({ rawOutput, content, heading }) => {
    const pages = parsePdfPages(rawOutput, content);
    return pages ? createElement(LazyPdfPagesCard, { pages, heading }) : null;
  });
  registerToolResultView("pdf.search", ({ rawOutput, heading }) => {
    const search = parsePdfSearch(rawOutput);
    return search ? createElement(LazyPdfSearchCard, { search, heading }) : null;
  });
  registerToolResultView("pdf.readText", ({ rawOutput, heading }) => {
    const text = parsePdfText(rawOutput);
    return text ? createElement(LazyPdfTextCard, { text, heading }) : null;
  });
  registerToolResultView("pdf.info", ({ rawOutput, heading }) => {
    const info = parsePdfInfo(rawOutput);
    return info ? createElement(LazyPdfInfoCard, { info, heading }) : null;
  });
  registerToolResultView("pdf.list", ({ rawOutput, heading }) => {
    const documents = parsePdfList(rawOutput);
    return documents ? createElement(LazyPdfListCard, { documents, heading }) : null;
  });
}
