# AgentAPI Office Runtime

This directory documents the public runtime used by the imported Codex office
skills in this repository.

The original Codex export expects private workspace dependency loading,
including `@oai/artifact-tool`. That package is not bundled here and is not
available from the public npm registry. The AgentAPI container therefore
provides public equivalents:

- DOCX: `python-docx`, `lxml`, `Pillow`, LibreOffice, Poppler
- PDF: `reportlab`, `pdfplumber`, `pypdf`, `pdf2image`, Poppler
- Presentations: `python-pptx` and global Node package `pptxgenjs`
- Spreadsheets: `openpyxl` and global Node package `exceljs`

Run the smoke check from any office skill directory:

```bash
python3 .claude/skill-runtimes/office/check_office_runtime.py
```

For Node scripts, global packages are exposed through:

```bash
export NODE_PATH="${OFFICE_RUNTIME_NODE_MODULES:-/usr/local/lib/node_modules}"
```

CommonJS `require(...)` resolves through `NODE_PATH`. For `.mjs` files that need
global packages, prefer `createRequire`:

```js
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const ExcelJS = require("exceljs");
```
