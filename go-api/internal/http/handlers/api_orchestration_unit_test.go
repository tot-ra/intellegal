//go:build !integration

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"legal-doc-intel/go-api/internal/ai"
)

type capturingAIClient struct {
	clauseReq  *ai.AnalyzeClauseRequest
	companyReq *ai.AnalyzeCompanyNameRequest
	llmReq     *ai.AnalyzeLLMReviewRequest
	chatReq    *ai.ContractChatRequest
	extractReq *ai.ExtractRequest
	indexReq   *ai.IndexRequest
	searchReq  *ai.SearchSectionsRequest
	clauseErr  error
	companyErr error
	llmErr     error
	chatErr    error
	extractErr error
	indexErr   error
	searchErr  error
}

func (c *capturingAIClient) AnalyzeClause(_ context.Context, req ai.AnalyzeClauseRequest) (ai.AnalysisResult, error) {
	copyReq := req
	copyReq.DocumentIDs = append([]string(nil), req.DocumentIDs...)
	c.clauseReq = &copyReq
	if c.clauseErr != nil {
		return ai.AnalysisResult{}, c.clauseErr
	}
	items := make([]ai.AnalysisResultItem, 0, len(req.DocumentIDs))
	for _, documentID := range req.DocumentIDs {
		items = append(items, ai.AnalysisResultItem{
			DocumentID: documentID,
			Outcome:    "match",
			Confidence: 0.86,
			Summary:    "Clause evidence found.",
			Evidence: []ai.AnalysisEvidenceSnippet{
				{SnippetText: "must include payment terms", PageNumber: 1, ChunkID: "1", Score: 0.91},
			},
		})
	}
	return ai.AnalysisResult{Items: items}, nil
}

func (c *capturingAIClient) AnalyzeCompanyName(_ context.Context, req ai.AnalyzeCompanyNameRequest) (ai.AnalysisResult, error) {
	copyReq := req
	copyReq.DocumentIDs = append([]string(nil), req.DocumentIDs...)
	c.companyReq = &copyReq
	if c.companyErr != nil {
		return ai.AnalysisResult{}, c.companyErr
	}
	items := make([]ai.AnalysisResultItem, 0, len(req.DocumentIDs))
	for _, documentID := range req.DocumentIDs {
		items = append(items, ai.AnalysisResultItem{
			DocumentID: documentID,
			Outcome:    "review",
			Confidence: 0.6,
			Summary:    "Both old and new names found.",
		})
	}
	return ai.AnalysisResult{Items: items}, nil
}

func (c *capturingAIClient) AnalyzeLLMReview(_ context.Context, req ai.AnalyzeLLMReviewRequest) (ai.AnalysisResult, error) {
	copyReq := req
	copyReq.DocumentIDs = append([]string(nil), req.DocumentIDs...)
	copyReq.Documents = append([]ai.AnalyzeDocument(nil), req.Documents...)
	c.llmReq = &copyReq
	if c.llmErr != nil {
		return ai.AnalysisResult{}, c.llmErr
	}
	items := make([]ai.AnalysisResultItem, 0, len(req.DocumentIDs))
	for _, documentID := range req.DocumentIDs {
		items = append(items, ai.AnalysisResultItem{
			DocumentID: documentID,
			Outcome:    "review",
			Confidence: 0.78,
			Summary:    "Gemini flagged a legal risk that needs review.",
			Evidence: []ai.AnalysisEvidenceSnippet{
				{SnippetText: "Either party may terminate on thirty days written notice.", PageNumber: 2},
			},
		})
	}
	return ai.AnalysisResult{Items: items}, nil
}

func (c *capturingAIClient) ContractChat(_ context.Context, req ai.ContractChatRequest) (ai.ContractChatResult, error) {
	copyReq := req
	copyReq.Messages = append([]ai.ContractChatMessage(nil), req.Messages...)
	copyReq.Documents = append([]ai.ContractChatDocument(nil), req.Documents...)
	c.chatReq = &copyReq
	if c.chatErr != nil {
		return ai.ContractChatResult{}, c.chatErr
	}
	return ai.ContractChatResult{
		Answer: "Termination is allowed with written notice.",
		Citations: []ai.ContractChatCitation{
			{
				DocumentID:  req.Documents[0].DocumentID,
				SnippetText: "Either party may terminate on thirty days written notice.",
				Reason:      "This clause states the notice requirement.",
			},
		},
	}, nil
}

func (c *capturingAIClient) Extract(_ context.Context, req ai.ExtractRequest) (ai.ExtractResult, error) {
	copyReq := req
	c.extractReq = &copyReq
	if c.extractErr != nil {
		return ai.ExtractResult{}, c.extractErr
	}
	return ai.ExtractResult{
		MIMEType: req.MIMEType,
		Text:     "sample text",
		Pages: []ai.ExtractPage{
			{PageNumber: 1, Text: "sample text"},
		},
	}, nil
}

func (c *capturingAIClient) Index(_ context.Context, req ai.IndexRequest) (ai.IndexResult, error) {
	copyReq := req
	c.indexReq = &copyReq
	if c.indexErr != nil {
		return ai.IndexResult{}, c.indexErr
	}
	return ai.IndexResult{
		DocumentID: req.DocumentID,
		Checksum:   req.VersionChecksum,
		ChunkCount: 1,
		Indexed:    true,
	}, nil
}

func (c *capturingAIClient) SearchSections(_ context.Context, req ai.SearchSectionsRequest) (ai.SearchSectionsResult, error) {
	copyReq := req
	copyReq.DocumentIDs = append([]string(nil), req.DocumentIDs...)
	c.searchReq = &copyReq
	if c.searchErr != nil {
		return ai.SearchSectionsResult{}, c.searchErr
	}
	return ai.SearchSectionsResult{Items: []ai.SearchSectionsResultItem{}}, nil
}

func TestRunClauseCheckMarksCompletedAndPassesRequest(t *testing.T) {
	aiClient := &capturingAIClient{}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)

	checkID := "00000000-0000-4000-8000-000000000011"
	docID := "00000000-0000-4000-8000-000000000012"
	api.checks[checkID] = checkRun{
		CheckID:     checkID,
		Status:      checkStatusQueued,
		CheckType:   checkTypeClause,
		RequestedAt: time.Now().UTC(),
		DocumentIDs: []string{docID},
	}

	api.runClauseCheck(checkID, clauseCheckRequest{
		RequiredClauseText: "must include payment terms",
		ContextHint:        "scope: fees",
	}, "req-123")

	if aiClient.clauseReq == nil {
		t.Fatal("expected AnalyzeClause to be called")
	}
	if aiClient.clauseReq.CheckID != checkID {
		t.Fatalf("expected check id %q, got %q", checkID, aiClient.clauseReq.CheckID)
	}
	if aiClient.clauseReq.RequestID != "req-123" {
		t.Fatalf("expected request id req-123, got %q", aiClient.clauseReq.RequestID)
	}
	if len(aiClient.clauseReq.DocumentIDs) != 1 || aiClient.clauseReq.DocumentIDs[0] != docID {
		t.Fatalf("unexpected document ids: %#v", aiClient.clauseReq.DocumentIDs)
	}

	run := api.checks[checkID]
	if run.Status != checkStatusCompleted {
		t.Fatalf("expected status completed, got %q", run.Status)
	}
	if run.FinishedAt == nil {
		t.Fatal("expected finished_at to be set")
	}
	if len(run.Items) != 1 {
		t.Fatalf("expected 1 result item, got %d", len(run.Items))
	}
	if run.Items[0].Outcome != "match" {
		t.Fatalf("expected mapped outcome match, got %q", run.Items[0].Outcome)
	}
	if len(run.Items[0].Evidence) != 1 {
		t.Fatalf("expected evidence to be mapped, got %d snippets", len(run.Items[0].Evidence))
	}
}

func TestRunCompanyNameCheckMarksFailedWhenAIClientReturnsError(t *testing.T) {
	aiClient := &capturingAIClient{companyErr: errors.New("upstream timeout")}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)

	checkID := "00000000-0000-4000-8000-000000000021"
	docID := "00000000-0000-4000-8000-000000000022"
	api.checks[checkID] = checkRun{
		CheckID:     checkID,
		Status:      checkStatusQueued,
		CheckType:   checkTypeCompany,
		RequestedAt: time.Now().UTC(),
		DocumentIDs: []string{docID},
	}

	api.runCompanyNameCheck(checkID, companyNameCheckRequest{
		OldCompanyName: "Old Corp",
		NewCompanyName: "New Corp",
	}, "req-789")

	if aiClient.companyReq == nil {
		t.Fatal("expected AnalyzeCompanyName to be called")
	}
	if aiClient.companyReq.CheckID != checkID {
		t.Fatalf("expected check id %q, got %q", checkID, aiClient.companyReq.CheckID)
	}
	if aiClient.companyReq.RequestID != "req-789" {
		t.Fatalf("expected request id req-789, got %q", aiClient.companyReq.RequestID)
	}

	run := api.checks[checkID]
	if run.Status != checkStatusFailed {
		t.Fatalf("expected status failed, got %q", run.Status)
	}
	if run.FinishedAt == nil {
		t.Fatal("expected finished_at to be set")
	}
	if run.FailureReason != "upstream timeout" {
		t.Fatalf("expected failure reason to be propagated, got %q", run.FailureReason)
	}
}

func TestChatContractBuildsAIRequestFromContractFiles(t *testing.T) {
	aiClient := &capturingAIClient{}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)

	contractID := "00000000-0000-4000-8000-000000000031"
	documentID := "00000000-0000-4000-8000-000000000032"
	now := time.Now().UTC()
	api.contracts[contractID] = contract{
		ID:        contractID,
		Name:      "Alpha",
		FileIDs:   []string{documentID},
		CreatedAt: now,
		UpdatedAt: now,
	}
	api.documents[documentID] = document{
		ID:            documentID,
		ContractID:    contractID,
		Filename:      "alpha.pdf",
		ExtractedText: "Either party may terminate on thirty days written notice.",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	body, err := json.Marshal(contractChatRequest{
		Messages: []contractChatMessageRequest{
			{Role: "user", Content: "Can either party terminate early?"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+contractID+"/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("contract_id", contractID)
	rec := httptest.NewRecorder()

	api.ChatContract(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if aiClient.chatReq == nil {
		t.Fatal("expected ContractChat to be called")
	}
	if aiClient.chatReq.ContractID != contractID {
		t.Fatalf("expected contract id %q, got %q", contractID, aiClient.chatReq.ContractID)
	}
	if len(aiClient.chatReq.Documents) != 1 {
		t.Fatalf("expected 1 document, got %d", len(aiClient.chatReq.Documents))
	}
	if aiClient.chatReq.Documents[0].Text != "Either party may terminate on thirty days written notice." {
		t.Fatalf("unexpected document text: %q", aiClient.chatReq.Documents[0].Text)
	}

	var resp contractChatResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Answer == "" {
		t.Fatal("expected answer")
	}
	if len(resp.Citations) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(resp.Citations))
	}
	if resp.Citations[0].Filename != "alpha.pdf" {
		t.Fatalf("expected filename alpha.pdf, got %q", resp.Citations[0].Filename)
	}
}

func TestRunLLMReviewCheckPassesExtractedDocumentText(t *testing.T) {
	aiClient := &capturingAIClient{}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)

	checkID := "00000000-0000-4000-8000-000000000031"
	docID := "00000000-0000-4000-8000-000000000032"
	api.checks[checkID] = checkRun{
		CheckID:     checkID,
		Status:      checkStatusQueued,
		CheckType:   checkTypeLLMReview,
		RequestedAt: time.Now().UTC(),
		DocumentIDs: []string{docID},
	}
	api.documents[docID] = document{
		ID:            docID,
		Filename:      "msa.pdf",
		ExtractedText: "Page 1\fEither party may terminate on thirty days written notice.",
	}

	api.runLLMReviewCheck(checkID, llmReviewCheckRequest{
		Instructions: "Review the full contract for termination for convenience.",
	}, "req-llm-123")

	if aiClient.llmReq == nil {
		t.Fatal("expected AnalyzeLLMReview to be called")
	}
	if aiClient.llmReq.CheckID != checkID {
		t.Fatalf("expected check id %q, got %q", checkID, aiClient.llmReq.CheckID)
	}
	if aiClient.llmReq.RequestID != "req-llm-123" {
		t.Fatalf("expected request id req-llm-123, got %q", aiClient.llmReq.RequestID)
	}
	if aiClient.llmReq.Instructions != "Review the full contract for termination for convenience." {
		t.Fatalf("unexpected instructions: %q", aiClient.llmReq.Instructions)
	}
	if len(aiClient.llmReq.Documents) != 1 {
		t.Fatalf("expected one document payload, got %d", len(aiClient.llmReq.Documents))
	}
	if aiClient.llmReq.Documents[0].Text == "" {
		t.Fatal("expected extracted text to be forwarded")
	}

	run := api.checks[checkID]
	if run.Status != checkStatusCompleted {
		t.Fatalf("expected status completed, got %q", run.Status)
	}
	if len(run.Items) != 1 || run.Items[0].Outcome != "review" {
		t.Fatalf("expected a mapped review result, got %#v", run.Items)
	}
}
