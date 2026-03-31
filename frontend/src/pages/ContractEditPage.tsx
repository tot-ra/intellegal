import {
  type CSSProperties,
  type DragEvent,
  type FormEvent,
  type ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import {
  apiClient,
  type ContractChatCitation,
  type ContractChatMessage,
  type ContractResponse,
  type DocumentResponse,
  type DocumentStatus,
  type DocumentTextResponse,
} from "../api/client";
import { formatEuropeanDateTime } from "../app/datetime";
import {
  deleteStoredResultsMany,
  deleteStoredRuns,
  getStoredGuidelineRule,
  getStoredResults,
  listPendingAutoGuidelineRuns,
  listStoredRuns,
  removePendingAutoGuidelineRun,
  setStoredResults,
  upsertStoredRun,
  writeLocalJson,
  readLocalJson,
  type StoredCheckRun,
} from "../app/localState";
import {
  formatGuidelineRunStatusEmoji,
  runGuidelineRule,
} from "../app/guidelineRunFlow";

const CONTRACT_TEXT_SETTINGS_KEY = "ldi.contractTextSettings";
const DEFAULT_TEXT_FONT_SIZE = 1.12;
const DEFAULT_TEXT_LINE_HEIGHT = 1.82;

type ContractTextSettings = {
  fontSize: number;
  lineHeight: number;
};

type ContractDisplayMode = "text" | "original";

type ChatMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  citations?: ContractChatCitation[];
};

type LocatedCitation = ContractChatCitation & {
  id: string;
  colorIndex: number;
  start: number;
  end: number;
};

function buildChatPayload(messages: ChatMessage[]): ContractChatMessage[] {
  return messages.map((message) => ({
    role: message.role,
    content: message.content,
  }));
}

function findCitationRange(
  text: string,
  snippetText: string,
): { start: number; end: number } | null {
  const normalizedText = text.toLowerCase();
  const normalizedSnippet = snippetText.trim().toLowerCase();
  if (!normalizedSnippet) {
    return null;
  }
  const start = normalizedText.indexOf(normalizedSnippet);
  if (start < 0) {
    return null;
  }
  return { start, end: start + normalizedSnippet.length };
}

function locateCitations(
  text: string,
  citations: LocatedCitation[],
): LocatedCitation[] {
  const matches: LocatedCitation[] = [];
  for (const citation of citations) {
    const range = findCitationRange(text, citation.snippet_text);
    if (!range) {
      continue;
    }
    matches.push({ ...citation, start: range.start, end: range.end });
  }
  return matches.sort((left, right) => left.start - right.start);
}

function renderHighlightedContractText(
  text: string,
  citations: LocatedCitation[],
  activeCitationId: string | null,
): ReactNode[] {
  if (citations.length === 0) {
    return [text];
  }

  const nodes: ReactNode[] = [];
  let cursor = 0;
  let segmentIndex = 0;
  for (const citation of citations) {
    if (citation.start < cursor) {
      continue;
    }
    if (citation.start > cursor) {
      nodes.push(text.slice(cursor, citation.start));
    }
    const content = text.slice(citation.start, citation.end);
    nodes.push(
      <mark
        key={`${citation.id}-${segmentIndex}`}
        id={`contract-chat-highlight-${citation.id}`}
        className={`contract-chat-highlight contract-chat-highlight-${citation.colorIndex}${
          activeCitationId === citation.id ? " is-active" : ""
        }`}
        title={citation.reason || "Referenced by contract assistant"}
      >
        {content}
      </mark>,
    );
    cursor = citation.end;
    segmentIndex += 1;
  }
  if (cursor < text.length) {
    nodes.push(text.slice(cursor));
  }
  return nodes;
}

function RobotIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <rect x="5" y="7" width="14" height="11" rx="3" />
      <path d="M12 3v4" />
      <path d="M8 18v2" />
      <path d="M16 18v2" />
      <path d="M3 11h2" />
      <path d="M19 11h2" />
      <circle cx="9.5" cy="12" r="1.1" />
      <circle cx="14.5" cy="12" r="1.1" />
      <path d="M9 15h6" />
    </svg>
  );
}

function readContractTextSettings(): ContractTextSettings {
  return readLocalJson<ContractTextSettings>(CONTRACT_TEXT_SETTINGS_KEY, {
    fontSize: DEFAULT_TEXT_FONT_SIZE,
    lineHeight: DEFAULT_TEXT_LINE_HEIGHT,
  });
}

function formatMimeTypeLabel(mimeType: string): string {
  switch (mimeType) {
    case "application/pdf":
      return "PDF";
    case "image/jpeg":
      return "JPEG";
    case "image/png":
      return "PNG";
    case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
      return "DOCX";
    default:
      return mimeType;
  }
}

async function toBase64(file: File): Promise<string> {
  const buffer = await file.arrayBuffer();
  let binary = "";
  const bytes = new Uint8Array(buffer);
  for (const value of bytes) {
    binary += String.fromCharCode(value);
  }
  return btoa(binary);
}

export function ContractEditPage() {
  const { contractId } = useParams<{ contractId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const [contract, setContract] = useState<ContractResponse | null>(null);
  const [files, setFiles] = useState<DocumentResponse[]>([]);
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [savingOrder, setSavingOrder] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [newFiles, setNewFiles] = useState<File[]>([]);
  const [isUploadDragOver, setIsUploadDragOver] = useState(false);
  const [contractNameInput, setContractNameInput] = useState("");
  const [contractTagsInput, setContractTagsInput] = useState("");
  const [savingDetails, setSavingDetails] = useState(false);
  const [textLoading, setTextLoading] = useState(false);
  const [documentTexts, setDocumentTexts] = useState<
    Record<string, DocumentTextResponse>
  >({});
  const [textError, setTextError] = useState<string | null>(null);
  const [pendingAutoRuns, setPendingAutoRuns] = useState(() =>
    contractId ? listPendingAutoGuidelineRuns(contractId) : [],
  );
  const [guidelineRuns, setGuidelineRuns] = useState<StoredCheckRun[]>([]);
  const [selectedGuidelineRunIds, setSelectedGuidelineRunIds] = useState<
    string[]
  >([]);
  const [guidelineError, setGuidelineError] = useState<string | null>(null);
  const [startingAutoGuidelines, setStartingAutoGuidelines] = useState(false);
  const [deletingGuidelines, setDeletingGuidelines] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [displayMode, setDisplayMode] = useState<ContractDisplayMode>("text");
  const [textFontSize, setTextFontSize] = useState(
    () => readContractTextSettings().fontSize,
  );
  const [textLineHeight, setTextLineHeight] = useState(
    () => readContractTextSettings().lineHeight,
  );
  const [chatOpen, setChatOpen] = useState(false);
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([]);
  const [chatInput, setChatInput] = useState("");
  const [chatLoading, setChatLoading] = useState(false);
  const [chatError, setChatError] = useState<string | null>(null);
  const [activeCitations, setActiveCitations] = useState<
    ContractChatCitation[]
  >([]);
  const [activeCitationId, setActiveCitationId] = useState<string | null>(null);
  const creationNotice = searchParams.get("notice")?.trim() ?? "";
  const chatBodyRef = useRef<HTMLDivElement | null>(null);

  const contractTextStyle = useMemo(
    () =>
      ({
        "--contract-text-font-size": `${textFontSize}rem`,
        "--contract-text-line-height": String(textLineHeight),
      }) as CSSProperties,
    [textFontSize, textLineHeight],
  );

  const appendFiles = (incomingFiles: File[]) => {
    if (incomingFiles.length === 0) return;
    setNewFiles((prev) => [...prev, ...incomingFiles]);
    setError(null);
  };

  const loadContract = async () => {
    if (!contractId) return;
    setError(null);
    try {
      const response = await apiClient.getContract(contractId);
      setContract(response);
      setFiles(response.files ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load contract.");
    }
  };

  useEffect(() => {
    void loadContract();
  }, [contractId]);

  useEffect(() => {
    if (!contract) return;
    setContractNameInput(contract.name);
    setContractTagsInput((contract.tags ?? []).join(", "));
  }, [contract]);

  useEffect(() => {
    writeLocalJson<ContractTextSettings>(CONTRACT_TEXT_SETTINGS_KEY, {
      fontSize: textFontSize,
      lineHeight: textLineHeight,
    });
  }, [textFontSize, textLineHeight]);

  useEffect(() => {
    if (!chatBodyRef.current) {
      return;
    }
    chatBodyRef.current.scrollTop = chatBodyRef.current.scrollHeight;
  }, [chatMessages, chatLoading]);

  useEffect(() => {
    if (!activeCitationId) {
      return;
    }
    const highlight = document.getElementById(
      `contract-chat-highlight-${activeCitationId}`,
    );
    if (!highlight || typeof highlight.scrollIntoView !== "function") {
      return;
    }
    highlight.scrollIntoView({ behavior: "smooth", block: "center" });
  }, [activeCitationId, activeCitations, documentTexts, displayMode]);

  const contractDocumentIds = useMemo(
    () => files.map((file) => file.id),
    [files],
  );

  useEffect(() => {
    setPendingAutoRuns(
      contractId ? listPendingAutoGuidelineRuns(contractId) : [],
    );
  }, [contractId, files.length, contract?.updated_at]);

  useEffect(() => {
    let cancelled = false;

    const loadDocumentTexts = async () => {
      if (files.length === 0) {
        setDocumentTexts({});
        setTextError(null);
        return;
      }

      setTextLoading(true);
      setTextError(null);
      try {
        const loaded = await Promise.all(
          files.map(async (file) => {
            const text = await apiClient.getDocumentText(file.id);
            return [file.id, text] as const;
          }),
        );
        if (cancelled) return;
        setDocumentTexts(Object.fromEntries(loaded));
      } catch (err) {
        if (cancelled) return;
        setTextError(
          err instanceof Error ? err.message : "Failed to load contract text.",
        );
      } finally {
        if (!cancelled) {
          setTextLoading(false);
        }
      }
    };

    void loadDocumentTexts();
    return () => {
      cancelled = true;
    };
  }, [files]);

  const statusSummary = useMemo(() => {
    return files.reduce<Record<DocumentStatus, number>>(
      (summary, file) => {
        summary[file.status] += 1;
        return summary;
      },
      { ingested: 0, processing: 0, indexed: 0, failed: 0 },
    );
  }, [files]);

  useEffect(() => {
    const shouldPoll =
      statusSummary.processing > 0 || statusSummary.ingested > 0;
    if (!shouldPoll) {
      return;
    }

    const timer = window.setInterval(() => {
      void loadContract();
    }, 3000);
    return () => {
      window.clearInterval(timer);
    };
  }, [statusSummary.processing, statusSummary.ingested, contractId]);

  const locatedCitationsByDocumentId = useMemo(() => {
    const byDocument: Record<string, LocatedCitation[]> = {};
    activeCitations.forEach((citation, index) => {
      const documentText = documentTexts[citation.document_id]?.text ?? "";
      const match = findCitationRange(documentText, citation.snippet_text);
      if (!match) {
        return;
      }
      const located: LocatedCitation = {
        ...citation,
        id: `${citation.document_id}-${index}`,
        colorIndex: index % 4,
        start: match.start,
        end: match.end,
      };
      byDocument[citation.document_id] = [
        ...(byDocument[citation.document_id] ?? []),
        located,
      ];
    });
    return byDocument;
  }, [activeCitations, documentTexts]);

  useEffect(() => {
    const relevantRuns = listStoredRuns().filter((run) =>
      (run.document_ids ?? []).some((documentId) =>
        contractDocumentIds.includes(documentId),
      ),
    );
    setGuidelineRuns(relevantRuns);
  }, [contractDocumentIds, contract?.updated_at, files.length]);

  useEffect(() => {
    setSelectedGuidelineRunIds((current) =>
      current.filter((checkId) =>
        guidelineRuns.some((run) => run.check_id === checkId),
      ),
    );
  }, [guidelineRuns]);

  useEffect(() => {
    let cancelled = false;

    const refreshRuns = async () => {
      if (contractDocumentIds.length === 0) {
        setGuidelineRuns([]);
        return;
      }

      const relevantRuns = listStoredRuns().filter((run) =>
        (run.document_ids ?? []).some((documentId) =>
          contractDocumentIds.includes(documentId),
        ),
      );

      try {
        setGuidelineError(null);
        await Promise.all(
          relevantRuns.map(async (run) => {
            if (run.execution_mode === "local") {
              return;
            }

            const runResponse = await apiClient.getCheckRun(run.check_id);
            upsertStoredRun({ ...run, ...runResponse });

            if (runResponse.status === "completed") {
              const resultsResponse = await apiClient.getCheckResults(
                run.check_id,
              );
              setStoredResults({
                check_id: resultsResponse.check_id,
                status: resultsResponse.status,
                items: resultsResponse.items,
                updated_at: new Date().toISOString(),
              });
            }
          }),
        );
      } catch (err) {
        if (!cancelled) {
          setGuidelineError(
            err instanceof Error
              ? err.message
              : "Failed to refresh guideline checks.",
          );
        }
      }

      if (!cancelled) {
        setGuidelineRuns(
          listStoredRuns().filter((run) =>
            (run.document_ids ?? []).some((documentId) =>
              contractDocumentIds.includes(documentId),
            ),
          ),
        );
      }
    };

    void refreshRuns();

    return () => {
      cancelled = true;
    };
  }, [contractDocumentIds, statusSummary.processing, statusSummary.ingested]);

  useEffect(() => {
    let cancelled = false;

    const startPendingAutoGuidelines = async () => {
      if (
        !contractId ||
        contractDocumentIds.length === 0 ||
        pendingAutoRuns.length === 0
      ) {
        return;
      }

      const filesStillProcessing = files.some(
        (file) => file.status === "ingested" || file.status === "processing",
      );
      if (filesStillProcessing) {
        return;
      }

      setStartingAutoGuidelines(true);
      setGuidelineError(null);

      try {
        for (const pending of pendingAutoRuns) {
          const rule = getStoredGuidelineRule(pending.rule_id);
          removePendingAutoGuidelineRun(pending.contract_id, pending.rule_id);
          setPendingAutoRuns(listPendingAutoGuidelineRuns(contractId));

          if (!rule) {
            continue;
          }

          await runGuidelineRule({
            rule,
            documentIds: contractDocumentIds,
            documents: files,
            scope: "contract",
          });
        }
      } catch (err) {
        if (!cancelled) {
          setGuidelineError(
            err instanceof Error
              ? err.message
              : "Failed to start automatic guideline checks.",
          );
        }
      } finally {
        if (!cancelled) {
          setStartingAutoGuidelines(false);
          setPendingAutoRuns(
            contractId ? listPendingAutoGuidelineRuns(contractId) : [],
          );
          setGuidelineRuns(
            listStoredRuns().filter((run) =>
              (run.document_ids ?? []).some((documentId) =>
                contractDocumentIds.includes(documentId),
              ),
            ),
          );
        }
      }
    };

    void startPendingAutoGuidelines();

    return () => {
      cancelled = true;
    };
  }, [contractId, contractDocumentIds, files, pendingAutoRuns]);

  const onDragStart = (id: string) => {
    setDraggingId(id);
  };

  const moveFileBefore = (targetId: string) => {
    if (!draggingId || draggingId === targetId) return;
    setFiles((prev) => {
      const sourceIndex = prev.findIndex((item) => item.id === draggingId);
      const targetIndex = prev.findIndex((item) => item.id === targetId);
      if (sourceIndex < 0 || targetIndex < 0) return prev;
      const next = [...prev];
      const [moved] = next.splice(sourceIndex, 1);
      next.splice(targetIndex, 0, moved);
      return next;
    });
  };

  const hasUnsavedOrder = useMemo(() => {
    const original = (contract?.files ?? []).map((item) => item.id).join(",");
    const current = files.map((item) => item.id).join(",");
    return original !== current;
  }, [contract?.files, files]);

  const hasUnsavedDetails = useMemo(() => {
    if (!contract) return false;
    const normalizedName = contractNameInput.trim();
    const normalizedTags = contractTagsInput
      .split(",")
      .map((tag) => tag.trim())
      .filter((tag) => tag.length > 0)
      .join("|")
      .toLowerCase();
    const currentTags = (contract.tags ?? []).join("|").toLowerCase();
    return normalizedName !== contract.name || normalizedTags !== currentTags;
  }, [contract, contractNameInput, contractTagsInput]);

  const allGuidelineRunsSelected =
    guidelineRuns.length > 0 &&
    selectedGuidelineRunIds.length === guidelineRuns.length;

  const selectedGuidelineRuns = useMemo(
    () =>
      guidelineRuns.filter((run) =>
        selectedGuidelineRunIds.includes(run.check_id),
      ),
    [guidelineRuns, selectedGuidelineRunIds],
  );

  const refreshGuidelineRunsFromStorage = () => {
    setGuidelineRuns(
      listStoredRuns().filter((run) =>
        (run.document_ids ?? []).some((documentId) =>
          contractDocumentIds.includes(documentId),
        ),
      ),
    );
  };

  const removeGuidelineRunsFromStorage = (checkIds: string[]) => {
    deleteStoredRuns(checkIds);
    deleteStoredResultsMany(checkIds);
    setSelectedGuidelineRunIds((current) =>
      current.filter((checkId) => !checkIds.includes(checkId)),
    );
    refreshGuidelineRunsFromStorage();
  };

  const deleteGuidelineRuns = async (runs: StoredCheckRun[]) => {
    if (runs.length === 0 || deletingGuidelines) {
      return;
    }

    const ids = runs.map((run) => run.check_id);
    const remoteRuns = runs.filter((run) => run.execution_mode !== "local");
    const localRuns = runs.filter((run) => run.execution_mode === "local");
    const confirmMessage =
      runs.length === 1
        ? `Delete "${runs[0].rule_name ?? "this guideline check"}"?`
        : `Delete ${runs.length} selected guideline checks?`;

    if (!window.confirm(`${confirmMessage}\n\nThis cannot be undone.`)) {
      return;
    }

    setDeletingGuidelines(true);
    setGuidelineError(null);
    setMessage(null);

    try {
      if (remoteRuns.length === 1) {
        await apiClient.deleteCheckRun(remoteRuns[0].check_id);
      } else if (remoteRuns.length > 1) {
        await apiClient.deleteCheckRuns({
          check_ids: remoteRuns.map((run) => run.check_id),
        });
      }

      removeGuidelineRunsFromStorage(ids);
      setMessage(
        runs.length === 1
          ? "Guideline check removed."
          : `${runs.length} guideline checks removed.`,
      );
    } catch (err) {
      const fallback =
        remoteRuns.length > 0
          ? "Failed to delete guideline checks."
          : "Failed to remove local guideline checks.";
      setGuidelineError(err instanceof Error ? err.message : fallback);
      if (localRuns.length > 0 && remoteRuns.length === 0) {
        removeGuidelineRunsFromStorage(ids);
      }
    } finally {
      setDeletingGuidelines(false);
    }
  };

  const saveContractDetails = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!contractId || !contract) return;

    setSavingDetails(true);
    setError(null);
    setMessage(null);
    try {
      const updated = await apiClient.updateContract(contractId, {
        name: contractNameInput.trim(),
        tags: contractTagsInput
          .split(",")
          .map((tag) => tag.trim())
          .filter((tag) => tag.length > 0),
      });
      setContract(updated);
      setFiles(updated.files ?? []);
      setMessage("Contract details saved.");
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to save contract details.",
      );
    } finally {
      setSavingDetails(false);
    }
  };

  const saveOrder = async () => {
    if (!contractId) return;
    setSavingOrder(true);
    setError(null);
    setMessage(null);
    try {
      const updated = await apiClient.reorderContractFiles(
        contractId,
        files.map((item) => item.id),
      );
      setContract(updated);
      setFiles(updated.files ?? []);
      setMessage("File order saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save order.");
    } finally {
      setSavingOrder(false);
    }
  };

  const uploadMoreFiles = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!contractId || newFiles.length === 0) return;

    for (const file of newFiles) {
      if (
        file.type !== "application/pdf" &&
        file.type !== "image/jpeg" &&
        file.type !== "image/png" &&
        file.type !==
          "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
      ) {
        setError("Only PDF, JPEG, PNG, and DOCX files are supported.");
        return;
      }
    }

    setUploading(true);
    setError(null);
    setMessage(null);
    try {
      for (const file of newFiles) {
        const contentBase64 = await toBase64(file);
        await apiClient.addContractFile(contractId, {
          filename: file.name,
          mime_type: file.type as
            | "application/pdf"
            | "image/jpeg"
            | "image/png"
            | "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
          content_base64: contentBase64,
        });
      }
      setNewFiles([]);
      await loadContract();
      setMessage("Files uploaded.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Upload failed.");
    } finally {
      setUploading(false);
    }
  };

  const submitContractChat = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!contractId || chatLoading) {
      return;
    }

    const question = chatInput.trim();
    if (!question) {
      return;
    }

    const nextMessages: ChatMessage[] = [
      ...chatMessages,
      {
        id: `user-${Date.now()}`,
        role: "user",
        content: question,
      },
    ];

    setChatMessages(nextMessages);
    setChatInput("");
    setChatLoading(true);
    setChatError(null);

    try {
      const response = await apiClient.chatWithContract(contractId, {
        messages: buildChatPayload(nextMessages),
      });
      const assistantMessage: ChatMessage = {
        id: `assistant-${Date.now()}`,
        role: "assistant",
        content: response.answer,
        citations: response.citations,
      };
      setChatMessages([...nextMessages, assistantMessage]);
      setActiveCitations(response.citations);
      setDisplayMode("text");
      const firstCitation = response.citations[0];
      setActiveCitationId(
        firstCitation ? `${firstCitation.document_id}-0` : null,
      );
    } catch (err) {
      setChatError(
        err instanceof Error
          ? err.message
          : "Failed to ask the contract assistant.",
      );
    } finally {
      setChatLoading(false);
    }
  };

  return (
    <section className="page">
      <header className="page-header">
        <h2>Contract Files</h2>
        <div className="page-actions">
          <Link to="/contracts" className="button-link secondary">
            Back to Contracts
          </Link>
        </div>
      </header>

      {contract ? (
        <section className="panel">
          <h3>Contract Details</h3>
          <form className="form-grid" onSubmit={saveContractDetails}>
            <label>
              Contract Name
              <input
                value={contractNameInput}
                onChange={(event) => setContractNameInput(event.target.value)}
              />
            </label>
            <label>
              Tags
              <input
                value={contractTagsInput}
                onChange={(event) => setContractTagsInput(event.target.value)}
                placeholder="comma-separated"
              />
            </label>
            <div className="page-actions">
              <button
                type="submit"
                disabled={
                  savingDetails ||
                  !hasUnsavedDetails ||
                  contractNameInput.trim().length === 0
                }
              >
                {savingDetails ? "Saving..." : "Save Details"}
              </button>
            </div>
          </form>
          <p className="muted">
            Contract ID: <code>{contract.id}</code> | Files: {files.length} |
            Updated: {formatEuropeanDateTime(contract.updated_at)}
          </p>
        </section>
      ) : null}

      <section className="contract-detail-grid">
        <div className="contract-detail-column">
          <section className="panel">
            <h3>Files</h3>
            <p className="muted">
              Drag and drop files to reorder pages/attachments inside this
              contract.
            </p>
            {files.length === 0 ? (
              <p className="muted">No files uploaded yet.</p>
            ) : null}
            <div className="contract-file-list">
              {files.map((file, index) => (
                <div
                  key={file.id}
                  className={`contract-file-item${draggingId === file.id ? " dragging" : ""}`}
                  draggable
                  onDragStart={() => onDragStart(file.id)}
                  onDragOver={(event: DragEvent<HTMLDivElement>) => {
                    event.preventDefault();
                    moveFileBefore(file.id);
                  }}
                  onDrop={(event) => {
                    event.preventDefault();
                    setDraggingId(null);
                  }}
                  onDragEnd={() => setDraggingId(null)}
                >
                  <span className="drag-handle">::</span>
                  <span className="order-index">{index + 1}.</span>
                  <span className="file-name">{file.filename}</span>
                  <span className="file-meta">
                    <span className="file-mime">
                      {formatMimeTypeLabel(file.mime_type)}
                    </span>
                    <span
                      className={`chip chip-compact ${
                        file.status === "indexed"
                          ? "chip-success"
                          : file.status === "failed"
                            ? "chip-danger"
                            : "chip-warning"
                      }`}
                    >
                      {file.status}
                    </span>
                  </span>
                </div>
              ))}
            </div>

            <form
              className={`file-upload-dropzone${isUploadDragOver ? " is-drag-over" : ""}`}
              onSubmit={uploadMoreFiles}
              onDragOver={(event: DragEvent<HTMLFormElement>) => {
                event.preventDefault();
                setIsUploadDragOver(true);
              }}
              onDragLeave={() => setIsUploadDragOver(false)}
              onDrop={(event: DragEvent<HTMLFormElement>) => {
                event.preventDefault();
                setIsUploadDragOver(false);
                appendFiles(Array.from(event.dataTransfer.files ?? []));
              }}
            >
              <h3>Add More Files</h3>
              <p className="muted">
                Drag and drop files here to add them to the bottom, or choose
                files manually.
              </p>
              <label>
                Files
                <input
                  type="file"
                  accept="application/pdf,image/jpeg,image/png,application/vnd.openxmlformats-officedocument.wordprocessingml.document,.docx"
                  multiple
                  onChange={(event) =>
                    appendFiles(Array.from(event.target.files ?? []))
                  }
                />
              </label>
              {newFiles.length > 0 ? (
                <p className="muted">Queued: {newFiles.length} file(s)</p>
              ) : null}
              <button
                type="submit"
                disabled={uploading || newFiles.length === 0}
              >
                {uploading ? "Uploading..." : "Upload Files"}
              </button>
            </form>

            <div className="page-actions">
              <button
                type="button"
                className="secondary"
                onClick={saveOrder}
                disabled={!hasUnsavedOrder || savingOrder}
              >
                {savingOrder ? "Saving..." : "Save Order"}
              </button>
            </div>
            <p className="muted">
              Indexing summary: {statusSummary.indexed} indexed,{" "}
              {statusSummary.processing + statusSummary.ingested} in progress,{" "}
              {statusSummary.failed} failed.
            </p>
          </section>

          <section className="panel">
            <div className="guideline-section-header">
              <div>
                <h3>Guideline Checks</h3>
                <p className="muted">
                  Automatic and manual guideline checks for the files in this
                  contract.
                </p>
              </div>
              <div className="page-actions">
                {guidelineRuns.length > 0 ? (
                  <button
                    type="button"
                    className="secondary"
                    onClick={() =>
                      setSelectedGuidelineRunIds(
                        allGuidelineRunsSelected
                          ? []
                          : guidelineRuns.map((run) => run.check_id),
                      )
                    }
                    disabled={deletingGuidelines}
                  >
                    {allGuidelineRunsSelected
                      ? "Clear Selection"
                      : "Select All"}
                  </button>
                ) : null}
                <button
                  type="button"
                  className="secondary"
                  onClick={() =>
                    void deleteGuidelineRuns(selectedGuidelineRuns)
                  }
                  disabled={
                    deletingGuidelines || selectedGuidelineRuns.length === 0
                  }
                >
                  {deletingGuidelines
                    ? "Deleting..."
                    : `Delete Selected (${selectedGuidelineRuns.length})`}
                </button>
                <Link to="/guidelines/run" className="button-link secondary">
                  Run Guideline
                </Link>
              </div>
            </div>
            {pendingAutoRuns.length > 0 ? (
              <p className="muted">
                {files.some(
                  (file) =>
                    file.status === "ingested" || file.status === "processing",
                )
                  ? `${pendingAutoRuns.length} automatic guideline check(s) will start once file processing finishes.`
                  : startingAutoGuidelines
                    ? `Starting ${pendingAutoRuns.length} automatic guideline check(s)...`
                    : `${pendingAutoRuns.length} automatic guideline check(s) are queued.`}
              </p>
            ) : null}
            {guidelineRuns.length === 0 && pendingAutoRuns.length === 0 ? (
              <p className="muted">
                No guideline checks for this contract yet.
              </p>
            ) : null}
            {guidelineRuns.length > 0 ? (
              <ul className="run-list">
                {guidelineRuns.map((run) => {
                  const cachedResults = getStoredResults(run.check_id);
                  const flaggedCount =
                    cachedResults?.items.filter(
                      (item) =>
                        item.outcome === "missing" || item.outcome === "review",
                    ).length ?? 0;
                  const isSelected = selectedGuidelineRunIds.includes(
                    run.check_id,
                  );

                  return (
                    <li key={run.check_id}>
                      <div className="run-row">
                        <label className="run-select">
                          <input
                            type="checkbox"
                            checked={isSelected}
                            onChange={(event) => {
                              setSelectedGuidelineRunIds((current) =>
                                event.target.checked
                                  ? [...current, run.check_id]
                                  : current.filter(
                                      (checkId) => checkId !== run.check_id,
                                    ),
                              );
                            }}
                            aria-label={`Select ${run.rule_name ?? "guideline check"}`}
                            disabled={deletingGuidelines}
                          />
                        </label>
                        <Link
                          to={`/guidelines?checkId=${encodeURIComponent(run.check_id)}`}
                          className="run-item"
                        >
                          <span className="guideline-run-item-copy">
                            <span className="guideline-run-item-title">
                              <span
                                className="guideline-run-status-emoji"
                                aria-hidden="true"
                              >
                                {formatGuidelineRunStatusEmoji(run.status)}
                              </span>
                              <span>{run.rule_name ?? "Guideline run"}</span>
                            </span>
                            <small>
                              {run.status === "completed" && cachedResults
                                ? `Flagged items: ${flaggedCount}`
                                : `Created ${formatEuropeanDateTime(run.requested_at)}`}
                            </small>
                          </span>
                        </Link>
                        <button
                          type="button"
                          className="secondary"
                          onClick={() => void deleteGuidelineRuns([run])}
                          disabled={deletingGuidelines}
                        >
                          Delete
                        </button>
                      </div>
                    </li>
                  );
                })}
              </ul>
            ) : null}
            {guidelineError ? (
              <p className="error-text">{guidelineError}</p>
            ) : null}
          </section>

          {creationNotice ? (
            <p className="muted">
              {creationNotice}{" "}
              <button
                type="button"
                className="secondary"
                onClick={() => {
                  const next = new URLSearchParams(searchParams);
                  next.delete("notice");
                  setSearchParams(next);
                }}
              >
                Dismiss
              </button>
            </p>
          ) : null}
          {message ? <p className="success-text">{message}</p> : null}
          {error ? <p className="error-text">{error}</p> : null}
        </div>

        <section
          className="panel contract-text-panel"
          style={contractTextStyle}
        >
          <div className="contract-text-panel-header">
            <div>
              <h3>
                {displayMode === "text" ? "Contract Text" : "Original Files"}
              </h3>
              <p className="muted">
                {displayMode === "text"
                  ? "Combined extracted text from all files, shown in reading order."
                  : "Original uploaded files shown inline in contract order."}
              </p>
            </div>
            <div
              className="contract-text-controls"
              aria-label="Contract text display controls"
            >
              <div
                className="segmented-control"
                role="tablist"
                aria-label="Contract display mode"
              >
                <button
                  type="button"
                  className={displayMode === "text" ? "is-active" : ""}
                  aria-pressed={displayMode === "text"}
                  onClick={() => setDisplayMode("text")}
                >
                  Text
                </button>
                <button
                  type="button"
                  className={displayMode === "original" ? "is-active" : ""}
                  aria-pressed={displayMode === "original"}
                  onClick={() => setDisplayMode("original")}
                >
                  Original
                </button>
              </div>
              {displayMode === "text" ? (
                <>
                  <label className="contract-text-control">
                    <span>Size {textFontSize.toFixed(2)}rem</span>
                    <input
                      type="range"
                      min="0.9"
                      max="1.5"
                      step="0.05"
                      value={textFontSize}
                      onChange={(event) =>
                        setTextFontSize(Number(event.target.value))
                      }
                    />
                  </label>
                  <label className="contract-text-control">
                    <span>Line {textLineHeight.toFixed(2)}</span>
                    <input
                      type="range"
                      min="0.9"
                      max="2.4"
                      step="0.1"
                      value={textLineHeight}
                      onChange={(event) =>
                        setTextLineHeight(Number(event.target.value))
                      }
                    />
                  </label>
                </>
              ) : null}
            </div>
          </div>
          {displayMode === "text" && textLoading ? (
            <p className="muted">Loading contract text...</p>
          ) : null}
          {displayMode === "text" && textError ? (
            <p className="error-text">{textError}</p>
          ) : null}
          {displayMode === "text" && !textLoading && !textError ? (
            <>
              {files.length === 0 ? (
                <p className="muted">
                  No files yet, so there is no text to show.
                </p>
              ) : null}
              {files.map((file, index) => {
                const entry = documentTexts[file.id];
                const locatedCitations = entry?.has_text
                  ? locateCitations(
                      entry.text,
                      locatedCitationsByDocumentId[file.id] ?? [],
                    )
                  : [];
                return (
                  <section key={file.id} className="word-document-section">
                    <div className="word-document-label">
                      {index + 1}. {file.filename}
                    </div>
                    <article className="word-document-page">
                      {entry?.has_text ? (
                        <>
                          {locatedCitations.length > 0 ? (
                            <div
                              className="contract-chat-citation-strip"
                              aria-label={`Highlights for ${file.filename}`}
                            >
                              {locatedCitations.map(
                                (citation, citationIndex) => (
                                  <button
                                    key={citation.id}
                                    type="button"
                                    className={`contract-chat-citation-pill contract-chat-citation-pill-${citation.colorIndex}${
                                      activeCitationId === citation.id
                                        ? " is-active"
                                        : ""
                                    }`}
                                    onClick={() =>
                                      setActiveCitationId(citation.id)
                                    }
                                  >
                                    {citationIndex + 1}.{" "}
                                    {citation.reason || "Referenced clause"}
                                  </button>
                                ),
                              )}
                            </div>
                          ) : null}
                          <p className="word-document-text">
                            {renderHighlightedContractText(
                              entry.text,
                              locatedCitations,
                              activeCitationId,
                            )}
                          </p>
                        </>
                      ) : (
                        <p className="muted">
                          No extracted text available for this file yet.
                        </p>
                      )}
                    </article>
                  </section>
                );
              })}
            </>
          ) : null}
          {displayMode === "original" ? (
            <>
              {files.length === 0 ? (
                <p className="muted">
                  No files yet, so there is nothing to preview.
                </p>
              ) : null}
              {files.map((file, index) => {
                const contentUrl = apiClient.getDocumentContentUrl(file.id);
                return (
                  <section key={file.id} className="word-document-section">
                    <div className="word-document-label">
                      {index + 1}. {file.filename}
                    </div>
                    <article className="word-document-page original-document-page">
                      {file.mime_type === "application/pdf" ? (
                        <iframe
                          className="original-document-frame"
                          src={contentUrl}
                          title={`Original preview for ${file.filename}`}
                        />
                      ) : file.mime_type === "image/jpeg" ||
                        file.mime_type === "image/png" ? (
                        <img
                          className="original-document-image"
                          src={contentUrl}
                          alt={file.filename}
                        />
                      ) : (
                        <p className="muted">
                          Inline preview is not available for this file type.
                        </p>
                      )}
                    </article>
                  </section>
                );
              })}
            </>
          ) : null}
        </section>
      </section>

      <div className="contract-chat-dock">
        {chatOpen ? (
          <section
            className="contract-chat-panel"
            aria-label="Contract assistant"
          >
            <header className="contract-chat-header">
              <div className="contract-chat-title">
                <span className="contract-chat-title-icon" aria-hidden="true">
                  <RobotIcon />
                </span>
                <h3>Contract Assistant</h3>
              </div>
              <div className="contract-chat-header-actions">
                <button
                  type="button"
                  className="secondary contract-chat-close-button"
                  onClick={() => setChatOpen(false)}
                  aria-label="Close contract assistant"
                >
                  Close
                </button>
              </div>
            </header>
            <div className="contract-chat-body" ref={chatBodyRef}>
              {chatMessages.length === 0 ? (
                <div className="contract-chat-message contract-chat-message-assistant">
                  Ask about clauses, obligations, dates, termination rights, or
                  missing language.
                </div>
              ) : null}
              {chatMessages.map((message) => (
                <div
                  key={message.id}
                  className={`contract-chat-message ${
                    message.role === "user"
                      ? "contract-chat-message-user"
                      : "contract-chat-message-assistant"
                  }`}
                >
                  <p>{message.content}</p>
                  {message.role === "assistant" &&
                  message.citations &&
                  message.citations.length > 0 ? (
                    <div className="contract-chat-citations">
                      {message.citations.map((citation, index) => {
                        const citationId = `${citation.document_id}-${index}`;
                        return (
                          <button
                            key={`${message.id}-${citationId}`}
                            type="button"
                            className={`contract-chat-citation-pill contract-chat-citation-pill-${index % 4}${
                              activeCitationId === citationId
                                ? " is-active"
                                : ""
                            }`}
                            onClick={() => {
                              setDisplayMode("text");
                              setActiveCitations(message.citations ?? []);
                              setActiveCitationId(citationId);
                            }}
                          >
                            {citation.filename || "Contract text"}:{" "}
                            {citation.reason || "Show support"}
                          </button>
                        );
                      })}
                    </div>
                  ) : null}
                </div>
              ))}
              {chatLoading ? (
                <div className="contract-chat-message contract-chat-message-assistant">
                  Thinking through the contract text...
                </div>
              ) : null}
            </div>
            <form className="contract-chat-form" onSubmit={submitContractChat}>
              <label className="sr-only" htmlFor="contract-chat-input">
                Ask a question about this contract
              </label>
              <div className="contract-chat-compose">
                <textarea
                  id="contract-chat-input"
                  value={chatInput}
                  onChange={(event) => setChatInput(event.target.value)}
                  placeholder="What does this contract say about termination, payment, liability..."
                  rows={3}
                />
                <button
                  type="submit"
                  disabled={chatLoading || chatInput.trim().length === 0}
                >
                  {chatLoading ? "Asking..." : "Ask"}
                </button>
              </div>
              <div className="contract-chat-actions">
                {chatError ? <p className="error-text">{chatError}</p> : null}
              </div>
            </form>
          </section>
        ) : null}
        {!chatOpen ? (
          <button
            type="button"
            className="contract-chat-toggle"
            aria-label="Open contract assistant"
            onClick={() => setChatOpen(true)}
          >
            <span className="contract-chat-toggle-icon">
              <RobotIcon />
            </span>
            <span>Ask AI</span>
          </button>
        ) : null}
      </div>
    </section>
  );
}
