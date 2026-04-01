# OCR And Text Extraction

## User flow

```mermaid
flowchart LR
    A["User uploads file"] --> B{"File type"}
    B -->|"PDF with embedded text"| C["Extract PDF text"]
    B -->|"Scanned / image-only PDF"| D["Render PDF pages"]
    B -->|"JPEG / PNG"| E["OCR image"]
    B -->|"DOCX"| F["Extract DOCX text"]
    C --> G["Normalize page text"]
    D --> H["OCR rendered pages"]
    E --> G
    F --> G
    H --> G
    G --> I["Return full text + per-page text + diagnostics"]
```

### Current scope
- Supports PDF, JPEG, PNG, and DOCX uploads
- Produces normalized full-document text and per-page text
- Returns diagnostics used for indexing and failure analysis

## Technical flow

```mermaid
sequenceDiagram
    participant GO as Go API
    participant ST as Object Storage
    participant AI as Python AI API
    participant OCR as Tesseract
    participant PP as Poppler

    GO->>ST: Load original file bytes
    GO->>AI: POST /internal/v1/extract
    AI->>AI: Resolve mime type
    AI->>AI: Try native extraction path
    alt PDF has text
        AI->>AI: Read page text with pypdf
    else PDF is image-only
        AI->>PP: Render PDF pages to PNG
        AI->>OCR: OCR each rendered page
    else JPEG / PNG
        AI->>OCR: OCR uploaded image
    else DOCX
        AI->>AI: Extract paragraphs and table text
    end
    AI->>AI: Normalize text + compute diagnostics
    AI-->>GO: text + pages + confidence + diagnostics
```

### Main files
- `py-ai-api/py_ai_api/services/extraction.py`
- `py-ai-api/py_ai_api/models/extraction.py`
- `py-ai-api/py_ai_api/api/routes/internal.py`
- `go-api/internal/ai/client.go`
- `go-api/internal/http/handlers/documents.go`
- `py-ai-api/Dockerfile`

## Extraction strategies

```mermaid
flowchart TD
    A{"Mime type"} -->|"application/pdf"| B["pypdf page text"]
    A -->|"image/jpeg or image/png"| C["Tesseract OCR"]
    A -->|"DOCX"| D["python-docx text"]
    B --> E{"Any non-empty PDF text?"}
    E -->|"Yes"| F["Use PDF text"]
    E -->|"No"| G["Render pages with pdftoppm"]
    G --> H["OCR each page image with Tesseract"]
    C --> I["Use OCR text"]
    D --> J["Use DOCX text"]
    F --> K["Normalize pages"]
    H --> K
    I --> K
    J --> K
```

### Strategy details
- PDF text path: uses `pypdf` page extraction first
- Scanned PDF fallback: if every extracted PDF page is empty after normalization, the service renders pages with `pdftoppm` and OCRs those rendered page images
- Image path: OCR runs directly on uploaded JPEG and PNG files
- DOCX path: extracts paragraph text plus table cell text

## Normalization and outputs

```mermaid
flowchart LR
    A["Raw extracted text"] --> B["Unicode NFKC normalization"]
    B --> C["Normalize CRLF and form-feed"]
    C --> D["Collapse repeated spaces"]
    D --> E["Trim leading/trailing blank lines"]
    E --> F["PageExtraction[]"]
    F --> G["Full text joined with form-feed separators"]
    F --> H["Confidence per page"]
    F --> I["Diagnostics"]
```

### Output shape
- `text`: full normalized document text
- `pages`: one normalized page entry per page
- `confidence`: average confidence across pages
- `diagnostics`: includes page count, empty pages, char count, checksum, processing time, and OCR metadata

## Diagnostics and observability

```mermaid
flowchart LR
    A["Extraction run"] --> B["page_count"]
    A --> C["empty_pages"]
    A --> D["char_count"]
    A --> E["ocr_used"]
    A --> F["checksum_sha256"]
    A --> G["processing_ms"]
    E --> H["ocr.engine"]
    E --> I["ocr.pdf_fallback"]
    E --> J["ocr.rendered_page_count"]
```

### Current OCR diagnostics
- `ocr_used`: whether OCR participated in extraction
- `ocr.engine`: currently `tesseract`
- `ocr.pdf_fallback`: present when a PDF had to be treated as image-only
- `ocr.rendered_page_count`: number of rendered PDF pages sent through OCR

## Runtime dependencies

```mermaid
flowchart LR
    A["Python AI API container"] --> B["pypdf"]
    A --> C["python-docx"]
    A --> D["Pillow"]
    A --> E["pytesseract"]
    A --> F["tesseract-ocr"]
    A --> G["poppler-utils / pdftoppm"]
```

### Required native tools
- `tesseract-ocr` for OCR itself
- `poppler-utils` for scanned PDF page rendering via `pdftoppm`

## Failure modes

```mermaid
stateDiagram-v2
    [*] --> extracting
    extracting --> extracted: text returned
    extracting --> failed: file unreadable
    extracting --> failed: OCR dependency missing
    extracting --> failed: PDF render failed
    extracting --> failed: storage fetch failed
```

### Typical failure cases
- Storage URL cannot be fetched
- Uploaded file is malformed
- `tesseract` or `pdftoppm` is unavailable in the running container
- OCR cannot open the rendered image payload

## Relationship to indexing

```mermaid
flowchart LR
    A["Extraction result"] --> B["Go API saves extracted_text"]
    A --> C["Index pages"]
    C --> D["Chunking"]
    D --> E["Qdrant vectors"]
    D --> F["Postgres searchable chunks"]
```

- OCR quality directly affects searchable chunks, guideline checks, contract chat, and document text display
- If extraction returns empty text and empty pages, indexing can complete with zero chunks, which leaves search and checks without evidence
