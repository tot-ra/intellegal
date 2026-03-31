package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"legal-doc-intel/go-api/internal/ai"
	"legal-doc-intel/go-api/internal/checksum"
	"legal-doc-intel/go-api/internal/db"
	"legal-doc-intel/go-api/internal/externalcopy"
	"legal-doc-intel/go-api/internal/models"

	"github.com/go-chi/chi/v5"
)

const (
	documentStatusIngested   = "ingested"
	documentStatusProcessing = "processing"
	documentStatusIndexed    = "indexed"
	documentStatusFailed     = "failed"
	checkStatusQueued        = "queued"
	checkStatusRunning       = "running"
	checkStatusCompleted     = "completed"
	checkStatusFailed        = "failed"
	checkTypeClause          = "clause_presence"
	checkTypeCompany         = "company_name"
	checkTypeLLMReview       = "llm_review"
)

var (
	validDocumentMimes = map[string]struct{}{
		"application/pdf": {},
		"image/jpeg":      {},
		"image/png":       {},
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {},
	}
	validSourceTypes       = map[string]struct{}{"repository": {}, "upload": {}, "api": {}}
	validDocStatuses       = map[string]struct{}{documentStatusIngested: {}, documentStatusProcessing: {}, documentStatusIndexed: {}, documentStatusFailed: {}}
	errIdempotencyConflict = errors.New("idempotency conflict")
)

type aiClient interface {
	AnalyzeClause(ctx context.Context, req ai.AnalyzeClauseRequest) (ai.AnalysisResult, error)
	AnalyzeCompanyName(ctx context.Context, req ai.AnalyzeCompanyNameRequest) (ai.AnalysisResult, error)
	AnalyzeLLMReview(ctx context.Context, req ai.AnalyzeLLMReviewRequest) (ai.AnalysisResult, error)
	ContractChat(ctx context.Context, req ai.ContractChatRequest) (ai.ContractChatResult, error)
	Extract(ctx context.Context, req ai.ExtractRequest) (ai.ExtractResult, error)
	Index(ctx context.Context, req ai.IndexRequest) (ai.IndexResult, error)
	SearchSections(ctx context.Context, req ai.SearchSectionsRequest) (ai.SearchSectionsResult, error)
}

type documentStore interface {
	Put(ctx context.Context, key string, body io.Reader) (string, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

type externalCopyClient interface {
	Enabled() bool
	CopyDocument(ctx context.Context, req externalcopy.CopyRequest) (externalcopy.CopyResult, error)
}

type contractReader interface {
	List(ctx context.Context, limit, offset int) ([]models.ContractListRow, int, error)
	Get(ctx context.Context, contractID string) (models.ContractRow, bool, error)
}

type documentReader interface {
	List(ctx context.Context, filter models.DocumentsListFilter) ([]models.DocumentRow, int, error)
	Get(ctx context.Context, documentID string) (models.DocumentRow, bool, error)
	ListIDs(ctx context.Context) ([]string, error)
	ResolveIDs(ctx context.Context, explicit []string) ([]string, error)
	Exists(ctx context.Context, documentID string) (bool, error)
	GetByIDs(ctx context.Context, documentIDs []string) (map[string]models.DocumentRow, error)
}

type noopAIClient struct{}

func (noopAIClient) AnalyzeClause(context.Context, ai.AnalyzeClauseRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}

func (noopAIClient) AnalyzeCompanyName(context.Context, ai.AnalyzeCompanyNameRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}

func (noopAIClient) AnalyzeLLMReview(context.Context, ai.AnalyzeLLMReviewRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}

func (noopAIClient) ContractChat(context.Context, ai.ContractChatRequest) (ai.ContractChatResult, error) {
	return ai.ContractChatResult{}, nil
}

func (noopAIClient) Extract(_ context.Context, req ai.ExtractRequest) (ai.ExtractResult, error) {
	return ai.ExtractResult{
		MIMEType: req.MIMEType,
		Text:     "",
	}, nil
}

func (noopAIClient) Index(_ context.Context, req ai.IndexRequest) (ai.IndexResult, error) {
	return ai.IndexResult{
		DocumentID: req.DocumentID,
		Checksum:   req.VersionChecksum,
		Indexed:    true,
	}, nil
}

func (noopAIClient) SearchSections(_ context.Context, _ ai.SearchSectionsRequest) (ai.SearchSectionsResult, error) {
	return ai.SearchSectionsResult{Items: []ai.SearchSectionsResultItem{}}, nil
}

type noopDocumentStore struct{}

func (noopDocumentStore) Put(_ context.Context, key string, _ io.Reader) (string, error) {
	return "file:///" + key, nil
}

func (noopDocumentStore) Get(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (noopDocumentStore) Delete(_ context.Context, _ string) error {
	return nil
}

type noopExternalCopyClient struct{}

func (noopExternalCopyClient) Enabled() bool { return false }

func (noopExternalCopyClient) CopyDocument(context.Context, externalcopy.CopyRequest) (externalcopy.CopyResult, error) {
	return externalcopy.CopyResult{}, &externalcopy.CallError{Retriable: false, Cause: errors.New("external copy is disabled")}
}

type API struct {
	logger         slogLogger
	ai             aiClient
	store          documentStore
	copier         externalCopyClient
	pg             *db.Postgres
	contractsModel contractReader
	documentsModel documentReader

	mu          sync.RWMutex
	contracts   map[string]contract
	documents   map[string]document
	checks      map[string]checkRun
	idempotency map[string]idempotencyRecord
	copyEvents  map[string]externalCopyEvent
}

type slogLogger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type idempotencyRecord struct {
	PayloadHash string
	CheckID     string
}

type document struct {
	ID            string
	ContractID    string
	SourceType    string
	SourceRef     string
	Tags          []string
	Filename      string
	MIMEType      string
	Status        string
	Checksum      string
	ExtractedText string
	StorageKey    string
	StorageURI    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type contract struct {
	ID         string
	Name       string
	SourceType string
	SourceRef  string
	Tags       []string
	FileIDs    []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type checkRun struct {
	CheckID       string
	Status        string
	CheckType     string
	RequestedAt   time.Time
	FinishedAt    *time.Time
	FailureReason string
	DocumentIDs   []string
	Items         []checkResultItem
}

type checkResultItem struct {
	DocumentID string            `json:"document_id"`
	Outcome    string            `json:"outcome"`
	Confidence float64           `json:"confidence"`
	Summary    string            `json:"summary,omitempty"`
	Evidence   []evidenceSnippet `json:"evidence,omitempty"`
}

type evidenceSnippet struct {
	SnippetText string  `json:"snippet_text"`
	PageNumber  int     `json:"page_number"`
	ChunkID     string  `json:"chunk_id,omitempty"`
	Score       float64 `json:"score,omitempty"`
}

type externalCopyEvent struct {
	ID             string
	DocumentID     string
	TargetSystem   string
	Status         string
	RequestPayload map[string]any
	ResponseBody   map[string]any
	Attempts       int
	ErrorMessage   string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type createDocumentRequest struct {
	ContractID    string   `json:"contract_id,omitempty"`
	SourceType    string   `json:"source_type,omitempty"`
	SourceRef     string   `json:"source_ref,omitempty"`
	Filename      string   `json:"filename"`
	MIMEType      string   `json:"mime_type"`
	ContentBase64 string   `json:"content_base64"`
	Tags          []string `json:"tags,omitempty"`
}

type documentResponse struct {
	ID         string   `json:"id"`
	ContractID string   `json:"contract_id,omitempty"`
	SourceType string   `json:"source_type,omitempty"`
	SourceRef  string   `json:"source_ref,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Filename   string   `json:"filename"`
	MIMEType   string   `json:"mime_type"`
	Status     string   `json:"status"`
	Checksum   string   `json:"checksum,omitempty"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}

type documentListResponse struct {
	Items  []documentResponse `json:"items"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
	Total  int                `json:"total"`
}

type documentTextResponse struct {
	DocumentID string `json:"document_id"`
	Filename   string `json:"filename"`
	Text       string `json:"text"`
	HasText    bool   `json:"has_text"`
}

type createContractRequest struct {
	Name       string   `json:"name"`
	SourceType string   `json:"source_type,omitempty"`
	SourceRef  string   `json:"source_ref,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

type updateContractRequest struct {
	Name *string   `json:"name,omitempty"`
	Tags *[]string `json:"tags,omitempty"`
}

type reorderContractFilesRequest struct {
	FileIDs []string `json:"file_ids"`
}

type contractResponse struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	SourceType string             `json:"source_type,omitempty"`
	SourceRef  string             `json:"source_ref,omitempty"`
	Tags       []string           `json:"tags,omitempty"`
	FileCount  int                `json:"file_count"`
	Files      []documentResponse `json:"files,omitempty"`
	CreatedAt  string             `json:"created_at"`
	UpdatedAt  string             `json:"updated_at"`
}

type contractListResponse struct {
	Items  []contractResponse `json:"items"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
	Total  int                `json:"total"`
}

type clauseCheckRequest struct {
	DocumentIDs        []string `json:"document_ids,omitempty"`
	RequiredClauseText string   `json:"required_clause_text"`
	ContextHint        string   `json:"context_hint,omitempty"`
}

type companyNameCheckRequest struct {
	DocumentIDs    []string `json:"document_ids,omitempty"`
	OldCompanyName string   `json:"old_company_name"`
	NewCompanyName string   `json:"new_company_name,omitempty"`
}

type llmReviewCheckRequest struct {
	DocumentIDs  []string `json:"document_ids,omitempty"`
	Instructions string   `json:"instructions"`
}

type checkAcceptedResponse struct {
	CheckID   string `json:"check_id"`
	Status    string `json:"status"`
	CheckType string `json:"check_type"`
}

type checkRunResponse struct {
	CheckID       string  `json:"check_id"`
	Status        string  `json:"status"`
	CheckType     string  `json:"check_type"`
	RequestedAt   string  `json:"requested_at"`
	FinishedAt    *string `json:"finished_at,omitempty"`
	FailureReason string  `json:"failure_reason,omitempty"`
}

type checkResultsResponse struct {
	CheckID string            `json:"check_id"`
	Status  string            `json:"status"`
	Items   []checkResultItem `json:"items"`
}

type deleteChecksRequest struct {
	CheckIDs []string `json:"check_ids"`
}

type contractSearchRequest struct {
	QueryText   string   `json:"query_text"`
	DocumentIDs []string `json:"document_ids,omitempty"`
	Limit       int      `json:"limit,omitempty"`
	Strategy    string   `json:"strategy,omitempty"`
	ResultMode  string   `json:"result_mode,omitempty"`
}

type contractSearchResultItem struct {
	DocumentID  string  `json:"document_id"`
	ContractID  string  `json:"contract_id,omitempty"`
	Filename    string  `json:"filename"`
	PageNumber  int     `json:"page_number"`
	ChunkID     string  `json:"chunk_id,omitempty"`
	Score       float64 `json:"score"`
	SnippetText string  `json:"snippet_text"`
}

type contractSearchResponse struct {
	Items []contractSearchResultItem `json:"items"`
}

type contractChatMessageRequest struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type contractChatRequest struct {
	Messages []contractChatMessageRequest `json:"messages"`
}

type contractChatCitationResponse struct {
	DocumentID  string `json:"document_id"`
	Filename    string `json:"filename,omitempty"`
	SnippetText string `json:"snippet_text"`
	Reason      string `json:"reason,omitempty"`
}

type contractChatResponse struct {
	Answer    string                         `json:"answer"`
	Citations []contractChatCitationResponse `json:"citations,omitempty"`
}

type errorEnvelope struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retriable bool           `json:"retriable"`
	Details   map[string]any `json:"details,omitempty"`
}

func NewAPI(logger slogLogger, aiClient aiClient, store documentStore, copier externalCopyClient) *API {
	if aiClient == nil {
		aiClient = noopAIClient{}
	}
	if store == nil {
		store = noopDocumentStore{}
	}
	if copier == nil {
		copier = noopExternalCopyClient{}
	}

	return &API{
		logger:         logger,
		ai:             aiClient,
		store:          store,
		copier:         copier,
		contractsModel: nil,
		documentsModel: nil,
		contracts:      map[string]contract{},
		documents:      map[string]document{},
		checks:         map[string]checkRun{},
		idempotency:    map[string]idempotencyRecord{},
		copyEvents:     map[string]externalCopyEvent{},
	}
}

// EnableInMemoryReadersForTesting wires read handlers to the in-memory state maps.
// It is intended only for tests that construct an API without a database.
func (a *API) EnableInMemoryReadersForTesting() {
	if a == nil {
		return
	}
	a.contractsModel = &inMemoryContractReader{api: a}
	a.documentsModel = &inMemoryDocumentReader{api: a}
}

func mapDocument(doc document) documentResponse {
	return documentResponse{
		ID:         doc.ID,
		ContractID: doc.ContractID,
		SourceType: doc.SourceType,
		SourceRef:  doc.SourceRef,
		Tags:       doc.Tags,
		Filename:   doc.Filename,
		MIMEType:   doc.MIMEType,
		Status:     doc.Status,
		Checksum:   doc.Checksum,
		CreatedAt:  doc.CreatedAt.Format(time.RFC3339),
		UpdatedAt:  doc.UpdatedAt.Format(time.RFC3339),
	}
}

func documentFromModel(doc models.DocumentRow) document {
	return document{
		ID:            doc.ID,
		ContractID:    doc.ContractID,
		SourceType:    doc.SourceType,
		SourceRef:     doc.SourceRef,
		Tags:          doc.Tags,
		Filename:      doc.Filename,
		MIMEType:      doc.MIMEType,
		Status:        doc.Status,
		Checksum:      doc.Checksum,
		ExtractedText: doc.ExtractedText,
		StorageKey:    doc.StorageKey,
		StorageURI:    doc.StorageURI,
		CreatedAt:     doc.CreatedAt,
		UpdatedAt:     doc.UpdatedAt,
	}
}

func mapContract(item contract, files []documentResponse) contractResponse {
	return contractResponse{
		ID:         item.ID,
		Name:       item.Name,
		SourceType: item.SourceType,
		SourceRef:  item.SourceRef,
		Tags:       item.Tags,
		FileCount:  len(item.FileIDs),
		Files:      files,
		CreatedAt:  item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:  item.UpdatedAt.Format(time.RFC3339),
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON payload", false, map[string]any{"error": err.Error()})
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, code, message string, retriable bool, details map[string]any) {
	writeJSON(w, status, errorEnvelope{Error: errorPayload{Code: code, Message: message, Retriable: retriable, Details: details}})
}

func hashPayload(payload any, documentIDs []string) (string, error) {
	blob := struct {
		Payload     any      `json:"payload"`
		DocumentIDs []string `json:"document_ids"`
	}{Payload: payload, DocumentIDs: documentIDs}

	data, err := json.Marshal(blob)
	if err != nil {
		return "", err
	}
	return checksum.SHA256Hex(data), nil
}

func extensionForFilename(filename, mimeType string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	if ext == ".pdf" || ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".docx" {
		return ext
	}

	if mimeType == "application/pdf" {
		return ".pdf"
	}
	if mimeType == "image/png" {
		return ".png"
	}
	if mimeType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		return ".docx"
	}

	return ".jpg"
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func pathParam(r *http.Request, key string) string {
	if value := strings.TrimSpace(chi.URLParam(r, key)); value != "" {
		return value
	}
	return strings.TrimSpace(r.PathValue(key))
}
