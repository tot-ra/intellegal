package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"legal-doc-intel/go-api/internal/ai"
	"legal-doc-intel/go-api/internal/db"
	"legal-doc-intel/go-api/internal/http/middleware"
	"legal-doc-intel/go-api/internal/ids"
)

func (a *API) ChatContract(w http.ResponseWriter, r *http.Request) {
	contractID := pathParam(r, "contract_id")
	if !ids.IsUUID(contractID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "contract_id must be a valid UUID", false, nil)
		return
	}

	var req contractChatRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	messages := make([]ai.ContractChatMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		content := strings.TrimSpace(message.Content)
		if role != "user" && role != "assistant" {
			writeError(w, http.StatusBadRequest, "invalid_argument", "messages.role must be one of: user, assistant", false, nil)
			return
		}
		if content == "" {
			writeError(w, http.StatusBadRequest, "invalid_argument", "messages.content is required", false, nil)
			return
		}
		messages = append(messages, ai.ContractChatMessage{
			Role:    role,
			Content: content,
		})
	}
	if len(messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "messages must contain at least one chat message", false, nil)
		return
	}

	documents, err := a.contractChatDocuments(contractID)
	if err != nil {
		switch err.Error() {
		case "contract not found":
			writeError(w, http.StatusNotFound, "not_found", err.Error(), false, nil)
		case "no contract files":
			writeError(w, http.StatusConflict, "contract_not_ready", "contract has no files yet", false, nil)
		case "no extracted text":
			writeError(w, http.StatusConflict, "contract_not_ready", "no extracted text is available for this contract yet", false, nil)
		default:
			if errors.Is(err, db.ErrNotConfigured) {
				writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
		}
		return
	}

	result, err := a.ai.ContractChat(context.Background(), ai.ContractChatRequest{
		JobID:      ids.NewUUID(),
		RequestID:  middleware.GetRequestID(r.Context()),
		ContractID: contractID,
		Messages:   messages,
		Documents:  documents,
	})
	if err != nil {
		a.logger.Error("contract chat failed", "contract_id", contractID, "error", err)
		writeError(w, http.StatusBadGateway, "contract_chat_unavailable", "contract chat is temporarily unavailable", true, nil)
		return
	}

	filenamesByDocumentID := make(map[string]string, len(documents))
	for _, doc := range documents {
		filenamesByDocumentID[doc.DocumentID] = doc.Filename
	}

	citations := make([]contractChatCitationResponse, 0, len(result.Citations))
	for _, citation := range result.Citations {
		snippet := strings.TrimSpace(citation.SnippetText)
		documentID := strings.TrimSpace(citation.DocumentID)
		if documentID == "" || snippet == "" {
			continue
		}
		citations = append(citations, contractChatCitationResponse{
			DocumentID:  documentID,
			Filename:    filenamesByDocumentID[documentID],
			SnippetText: snippet,
			Reason:      strings.TrimSpace(citation.Reason),
		})
	}

	writeJSON(w, http.StatusOK, contractChatResponse{
		Answer:    strings.TrimSpace(result.Answer),
		Citations: citations,
	})
}

func (a *API) contractChatDocuments(contractID string) ([]ai.ContractChatDocument, error) {
	if a.contractsModel == nil {
		return nil, db.ErrNotConfigured
	}

	item, ok, err := a.contractsModel.Get(context.Background(), contractID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errString("contract not found")
	}
	if len(item.Files) == 0 {
		return nil, errString("no contract files")
	}

	documents := make([]ai.ContractChatDocument, 0, len(item.Files))
	for _, doc := range item.Files {
		text := strings.TrimSpace(doc.ExtractedText)
		if text == "" {
			continue
		}
		documents = append(documents, ai.ContractChatDocument{
			DocumentID: doc.ID,
			Filename:   doc.Filename,
			Text:       text,
		})
	}
	if len(documents) == 0 {
		return nil, errString("no extracted text")
	}
	return documents, nil
}

type errString string

func (e errString) Error() string {
	return string(e)
}
