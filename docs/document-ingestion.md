# Document Ingestion

## User flow

```mermaid
flowchart LR
    A["Upload files"] --> B{"Flow"}
    B -->|"One contract, many files"| C["New Contract"]
    B -->|"One file = one contract"| D["Batch Import"]
    C --> E["Create contract"]
    D --> E
    E --> F["Upload file"]
    F --> G["Extract text"]
    G --> H["Index chunks"]
    H --> I["File becomes indexed or failed"]
```

### Current entry points
- `New Contract`: many files under one contract
- `Batch Import Contracts`: one contract per file

### Supported files
- PDF
- JPEG
- PNG
- DOCX

## Technical flow

```mermaid
sequenceDiagram
    participant U as User
    participant FE as Frontend
    participant GO as Go API
    participant ST as Object Storage
    participant AI as Python AI API
    participant Q as Qdrant/Postgres

    U->>FE: Select files
    FE->>GO: POST /contracts
    FE->>GO: POST /contracts/:id/files
    GO->>ST: Store original file
    GO->>GO: Create document(status=processing)
    GO->>AI: Extract
    AI-->>GO: Full text + pages
    GO->>AI: Index
    AI->>Q: Store chunks + vectors
    AI-->>GO: Index result
    GO->>GO: Save extracted text
    GO->>GO: Mark document indexed
    GO-->>FE: Created document
```

### Main files
- `frontend/src/pages/NewContractPage.tsx`
- `frontend/src/pages/BatchImportContractsPage.tsx`
- `frontend/src/pages/contractUpload.ts`
- `go-api/internal/http/handlers/documents.go`
- `go-api/internal/http/handlers/contracts.go`
- `py-ai-api/py_ai_api/services/extraction.py`
- `py-ai-api/py_ai_api/services/indexing.py`

## Upload variants

```mermaid
flowchart TD
    A["New Contract"] --> B["Create one contract"]
    B --> C["Upload N files into it"]

    D["Batch Import"] --> E["For each file"]
    E --> F["Create contract from filename"]
    F --> G["Upload that file"]
```

## Extraction

```mermaid
flowchart LR
    A{"Mime type"} -->|"PDF"| B["pypdf page text"]
    A -->|"DOCX"| C["python-docx text"]
    A -->|"JPEG / PNG"| D["Tesseract OCR"]
    B --> E{"Text found?"}
    E -->|"Yes"| F["Normalize text"]
    E -->|"No"| G["Render PDF pages + OCR"]
    C --> F
    D --> F
    G --> F
    F --> H["Return full text + page text + diagnostics"]
```

### OCR nuance
- PDFs try native text extraction first
- If a PDF is effectively image-only after normalization, each page is rendered and OCR'd before indexing
- See [OCR + Text Extraction](./ocr-text-extraction.md) for the full extraction/runtime details

## Indexing

```mermaid
flowchart LR
    A["Extracted pages"] --> B["Prepare pages"]
    B --> C["Chunk text"]
    C --> D["Generate embeddings"]
    D --> E["Upsert into Qdrant"]
    C --> F["Store searchable chunks in Postgres"]
```

### Current defaults
- Chunk size: `800`
- Chunk overlap: `120`

## Status model

```mermaid
stateDiagram-v2
    [*] --> processing
    processing --> indexed: extract + index succeed
    processing --> failed: extract or index fails
```

### Notes
- `ingested` exists in the model
- Current upload flow mainly uses `processing -> indexed|failed`

## Outputs used by other features

```mermaid
flowchart LR
    A["Ingestion"] --> B["Full extracted text"]
    A --> C["Indexed chunks"]
    B --> D["Contract comparison"]
    C --> E["Search"]
    C --> F["Checks"]
    C --> G["Evidence retrieval"]
```

## Failure handling
- Validation errors stop the upload
- Extraction/indexing errors mark the file `failed`
- In batch import, one failed file does not stop the rest
- In single-contract upload, the contract may still be created even if some file processing fails
