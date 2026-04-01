# Contracts RAG Assistant

## User flow

```mermaid
flowchart LR
    A["Contracts page"] --> B["Open Ask AI"]
    B --> C["Ask question about the contract set"]
    C --> D["Assistant runs strict + semantic retrieval"]
    D --> E["Rank strongest matching contracts"]
    E --> F["Expand matching contracts with extracted text"]
    F --> G["Answer with citations + result block"]
    G --> H["User expands results in chat"]
    G --> I["User applies result IDs to main contract list"]
```

### Current scope
- Ask questions from the contracts list without granting the assistant blanket access to every full contract by default
- Run autonomous retrieval behind the scenes using both strict and semantic search
- Expand the strongest matching contracts before answering so the assistant can dig deeper when needed
- Return a compact result block in chat that can also drive the main contract list filter

## Technical flow

```mermaid
sequenceDiagram
    participant U as User
    participant FE as Frontend
    participant GO as Go API
    participant AI as Python AI API
    participant IDX as Search Index
    participant LLM as Gemini

    U->>FE: Ask question in contracts list assistant
    FE->>GO: POST /api/v1/contracts/search/chat
    GO->>AI: Search sections (strict)
    GO->>AI: Search sections (semantic)
    AI->>IDX: Retrieve matching chunks
    IDX-->>AI: Candidate matches
    AI-->>GO: Ranked search hits
    GO->>GO: Group by contract and pick strongest leads
    GO->>GO: Load matching contract text for deeper context
    GO->>AI: Contract chat with expanded documents
    AI->>LLM: Answer from expanded contract text
    LLM-->>AI: Answer + citations
    AI-->>GO: Chat result
    GO-->>FE: Answer + citations + result block
    FE-->>U: Show answer, expandable results, and apply-to-list action
```

### Main files
- `frontend/src/pages/ContractsPage.tsx`
- `frontend/src/api/client.ts`
- `go-api/internal/http/handlers/contract_chat.go`
- `go-api/internal/http/router/router.go`

## Retrieval strategy

### Why we use a staged RAG flow
- The contracts list can span many contracts, so sending all extracted text into one prompt is too expensive and noisy
- Some questions respond better to exact wording, while others respond better to semantic similarity
- The UI needs actionable results, not only a prose answer

### Current retrieval plan
```mermaid
flowchart TD
    A["Latest user question"] --> B["Strict search"]
    A --> C["Semantic search"]
    B --> D["Merge candidates"]
    C --> D
    D --> E["Rank by best contract-level hit"]
    E --> F["Take top matching contracts"]
    F --> G["Load full extracted text for those contracts"]
    G --> H["Ask LLM with expanded contract set"]
```

### Result block behavior
- The assistant answer can include a result block with matched contract/document IDs, names, scores, and snippets
- Users can expand that block inside chat to inspect matches
- Users can apply those IDs back into the contracts page to filter the main list view

## Current limitations
- The assistant still decides retrieval server-side rather than exposing tool-by-tool execution in the UI
- Contract ranking is based on best hit score rather than a richer multi-signal relevance model
- List filtering uses an assistant-owned filter state rather than rewriting the text search box
- Deep follow-up inspection is limited to contracts with extracted text already available

## Next likely step

```mermaid
flowchart LR
    A["Current: autonomous retrieval + apply results"] --> B["Pinned result sets in list view"]
    B --> C["Follow-up questions against selected result set"]
    C --> D["Per-result include/exclude actions"]
    D --> E["Deeper multi-hop legal reasoning"]
```
