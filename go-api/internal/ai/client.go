package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type AnalyzeClauseRequest struct {
	JobID              string   `json:"job_id"`
	RequestID          string   `json:"request_id,omitempty"`
	CheckID            string   `json:"check_id"`
	DocumentIDs        []string `json:"document_ids,omitempty"`
	RequiredClauseText string   `json:"required_clause_text"`
	ContextHint        string   `json:"context_hint,omitempty"`
}

type AnalyzeDocument struct {
	DocumentID string `json:"document_id"`
	Filename   string `json:"filename,omitempty"`
	Text       string `json:"text,omitempty"`
}

type ContractChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ContractChatDocument struct {
	DocumentID string `json:"document_id"`
	Filename   string `json:"filename,omitempty"`
	Text       string `json:"text,omitempty"`
}

type ContractChatCitation struct {
	DocumentID  string `json:"document_id"`
	SnippetText string `json:"snippet_text"`
	Reason      string `json:"reason,omitempty"`
}

type ContractChatRequest struct {
	JobID      string                 `json:"job_id"`
	RequestID  string                 `json:"request_id,omitempty"`
	ContractID string                 `json:"contract_id"`
	Messages   []ContractChatMessage  `json:"messages"`
	Documents  []ContractChatDocument `json:"documents,omitempty"`
}

type ContractChatResult struct {
	Answer    string                 `json:"answer"`
	Citations []ContractChatCitation `json:"citations,omitempty"`
}

type AnalysisEvidenceSnippet struct {
	SnippetText string  `json:"snippet_text"`
	PageNumber  int     `json:"page_number"`
	ChunkID     string  `json:"chunk_id,omitempty"`
	Score       float64 `json:"score,omitempty"`
}

type AnalysisResultItem struct {
	DocumentID string                    `json:"document_id"`
	Outcome    string                    `json:"outcome"`
	Confidence float64                   `json:"confidence"`
	Summary    string                    `json:"summary,omitempty"`
	Evidence   []AnalysisEvidenceSnippet `json:"evidence,omitempty"`
}

type AnalysisResult struct {
	Items       []AnalysisResultItem `json:"items"`
	Diagnostics map[string]any       `json:"diagnostics"`
}

type AnalyzeCompanyNameRequest struct {
	JobID          string   `json:"job_id"`
	RequestID      string   `json:"request_id,omitempty"`
	CheckID        string   `json:"check_id"`
	DocumentIDs    []string `json:"document_ids,omitempty"`
	OldCompanyName string   `json:"old_company_name"`
	NewCompanyName string   `json:"new_company_name,omitempty"`
}

type AnalyzeLLMReviewRequest struct {
	JobID        string            `json:"job_id"`
	RequestID    string            `json:"request_id,omitempty"`
	CheckID      string            `json:"check_id"`
	DocumentIDs  []string          `json:"document_ids,omitempty"`
	Instructions string            `json:"instructions"`
	Documents    []AnalyzeDocument `json:"documents,omitempty"`
}

type ExtractRequest struct {
	JobID      string `json:"job_id"`
	RequestID  string `json:"request_id,omitempty"`
	DocumentID string `json:"document_id"`
	StorageURI string `json:"storage_uri"`
	MIMEType   string `json:"mime_type,omitempty"`
}

type ExtractPage struct {
	PageNumber int     `json:"page_number"`
	Text       string  `json:"text"`
	CharCount  int     `json:"char_count"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"`
}

type ExtractResult struct {
	MIMEType    string         `json:"mime_type"`
	Text        string         `json:"text"`
	Pages       []ExtractPage  `json:"pages"`
	Confidence  float64        `json:"confidence"`
	Diagnostics map[string]any `json:"diagnostics"`
}

type IndexPageInput struct {
	PageNumber int    `json:"page_number"`
	Text       string `json:"text"`
}

type IndexRequest struct {
	JobID           string           `json:"job_id"`
	RequestID       string           `json:"request_id,omitempty"`
	DocumentID      string           `json:"document_id"`
	VersionChecksum string           `json:"version_checksum"`
	ExtractedText   string           `json:"extracted_text,omitempty"`
	Pages           []IndexPageInput `json:"pages,omitempty"`
	SourceURI       string           `json:"source_uri,omitempty"`
	Reindex         bool             `json:"reindex"`
}

type IndexResult struct {
	DocumentID    string         `json:"document_id"`
	Checksum      string         `json:"checksum"`
	ChunkCount    int            `json:"chunk_count"`
	Indexed       bool           `json:"indexed"`
	SkippedReason string         `json:"skipped_reason,omitempty"`
	Diagnostics   map[string]any `json:"diagnostics"`
}

type SearchSectionsRequest struct {
	JobID       string   `json:"job_id"`
	RequestID   string   `json:"request_id,omitempty"`
	QueryText   string   `json:"query_text"`
	DocumentIDs []string `json:"document_ids,omitempty"`
	Limit       int      `json:"limit,omitempty"`
	Strategy    string   `json:"strategy,omitempty"`
	ResultMode  string   `json:"result_mode,omitempty"`
}

type SearchSectionsResultItem struct {
	DocumentID  string  `json:"document_id"`
	PageNumber  int     `json:"page_number"`
	ChunkID     string  `json:"chunk_id,omitempty"`
	Score       float64 `json:"score"`
	SnippetText string  `json:"snippet_text"`
}

type SearchSectionsResult struct {
	Items       []SearchSectionsResultItem `json:"items"`
	Diagnostics map[string]any             `json:"diagnostics"`
}

type acceptedJobResponse[T any] struct {
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	JobType string `json:"job_type"`
	Result  T      `json:"result"`
}

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(baseURL, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) AnalyzeClause(ctx context.Context, req AnalyzeClauseRequest) (AnalysisResult, error) {
	var out acceptedJobResponse[AnalysisResult]
	if err := c.postJSONWithResponse(ctx, "/internal/v1/analyze/clause", req, &out); err != nil {
		return AnalysisResult{}, err
	}
	return out.Result, nil
}

func (c *Client) AnalyzeCompanyName(ctx context.Context, req AnalyzeCompanyNameRequest) (AnalysisResult, error) {
	var out acceptedJobResponse[AnalysisResult]
	if err := c.postJSONWithResponse(ctx, "/internal/v1/analyze/company-name", req, &out); err != nil {
		return AnalysisResult{}, err
	}
	return out.Result, nil
}

func (c *Client) AnalyzeLLMReview(ctx context.Context, req AnalyzeLLMReviewRequest) (AnalysisResult, error) {
	var out acceptedJobResponse[AnalysisResult]
	if err := c.postJSONWithResponse(ctx, "/internal/v1/analyze/llm-review", req, &out); err != nil {
		return AnalysisResult{}, err
	}
	return out.Result, nil
}

func (c *Client) ContractChat(ctx context.Context, req ContractChatRequest) (ContractChatResult, error) {
	var out acceptedJobResponse[ContractChatResult]
	if err := c.postJSONWithResponse(ctx, "/internal/v1/chat/contract", req, &out); err != nil {
		return ContractChatResult{}, err
	}
	return out.Result, nil
}

func (c *Client) Extract(ctx context.Context, req ExtractRequest) (ExtractResult, error) {
	var out acceptedJobResponse[ExtractResult]
	if err := c.postJSONWithResponse(ctx, "/internal/v1/extract", req, &out); err != nil {
		return ExtractResult{}, err
	}
	return out.Result, nil
}

func (c *Client) Index(ctx context.Context, req IndexRequest) (IndexResult, error) {
	var out acceptedJobResponse[IndexResult]
	if err := c.postJSONWithResponse(ctx, "/internal/v1/index", req, &out); err != nil {
		return IndexResult{}, err
	}
	return out.Result, nil
}

func (c *Client) SearchSections(ctx context.Context, req SearchSectionsRequest) (SearchSectionsResult, error) {
	var out acceptedJobResponse[SearchSectionsResult]
	if err := c.postJSONWithResponse(ctx, "/internal/v1/search/sections", req, &out); err != nil {
		return SearchSectionsResult{}, err
	}
	return out.Result, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload any) error {
	return c.postJSONWithResponse(ctx, path, payload, nil)
}

func (c *Client) postJSONWithResponse(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
		httpReq.Header.Set("X-Internal-Service-Token", c.token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}
