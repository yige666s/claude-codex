import { pathToFileURL } from "node:url";
import { Presentation, PresentationFile } from "@oai/artifact-tool";
import { builders } from "../../artifact-tool-compose/index.mjs";

export function buildPresentation() {
  const presentation = Presentation.create({ slideSize: { width: 1280, height: 720 } });
  for (const buildSlide of builders) buildSlide(presentation);
  return presentation;
}

export async function exportPresentation(outputPath) {
  const file = await PresentationFile.exportPptx(buildPresentation());
  await file.save(outputPath);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const outputPath = process.argv[2] ?? "codex-grid-layout-library.pptx";
  await exportPresentation(outputPath);
  console.log(outputPath);
}
