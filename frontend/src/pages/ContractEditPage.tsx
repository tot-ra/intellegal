import { type CSSProperties, type DragEvent, type FormEvent, useEffect, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import {
  apiClient,
  type ContractResponse,
  type DocumentResponse,
  type DocumentStatus,
  type DocumentTextResponse
} from "../api/client";
import { formatEuropeanDateTime } from "../app/datetime";
import { readLocalJson, writeLocalJson } from "../app/localState";

const CONTRACT_TEXT_SETTINGS_KEY = "ldi.contractTextSettings";
const DEFAULT_TEXT_FONT_SIZE = 1.12;
const DEFAULT_TEXT_LINE_HEIGHT = 1.82;

type ContractTextSettings = {
  fontSize: number;
  lineHeight: number;
};

type ContractDisplayMode = "text" | "original";

function readContractTextSettings(): ContractTextSettings {
  return readLocalJson<ContractTextSettings>(CONTRACT_TEXT_SETTINGS_KEY, {
    fontSize: DEFAULT_TEXT_FONT_SIZE,
    lineHeight: DEFAULT_TEXT_LINE_HEIGHT
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
  const [documentTexts, setDocumentTexts] = useState<Record<string, DocumentTextResponse>>({});
  const [textError, setTextError] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [displayMode, setDisplayMode] = useState<ContractDisplayMode>("text");
  const [textFontSize, setTextFontSize] = useState(() => readContractTextSettings().fontSize);
  const [textLineHeight, setTextLineHeight] = useState(() => readContractTextSettings().lineHeight);
  const creationNotice = searchParams.get("notice")?.trim() ?? "";

  const contractTextStyle = useMemo(
    () =>
      ({
        "--contract-text-font-size": `${textFontSize}rem`,
        "--contract-text-line-height": String(textLineHeight)
      }) as CSSProperties,
    [textFontSize, textLineHeight]
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
      lineHeight: textLineHeight
    });
  }, [textFontSize, textLineHeight]);

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
          })
        );
        if (cancelled) return;
        setDocumentTexts(Object.fromEntries(loaded));
      } catch (err) {
        if (cancelled) return;
        setTextError(err instanceof Error ? err.message : "Failed to load contract text.");
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
      { ingested: 0, processing: 0, indexed: 0, failed: 0 }
    );
  }, [files]);

  useEffect(() => {
    const shouldPoll = statusSummary.processing > 0 || statusSummary.ingested > 0;
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
          .filter((tag) => tag.length > 0)
      });
      setContract(updated);
      setFiles(updated.files ?? []);
      setMessage("Contract details saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save contract details.");
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
        files.map((item) => item.id)
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
      if (file.type !== "application/pdf" && file.type !== "image/jpeg" && file.type !== "image/png") {
        setError("Only PDF, JPEG, and PNG files are supported.");
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
          mime_type: file.type as "application/pdf" | "image/jpeg" | "image/png",
          content_base64: contentBase64
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
              <input value={contractNameInput} onChange={(event) => setContractNameInput(event.target.value)} />
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
              <button type="submit" disabled={savingDetails || !hasUnsavedDetails || contractNameInput.trim().length === 0}>
                {savingDetails ? "Saving..." : "Save Details"}
              </button>
            </div>
          </form>
          <p className="muted">
            Contract ID: <code>{contract.id}</code> | Files: {files.length} | Updated: {formatEuropeanDateTime(contract.updated_at)}
          </p>
        </section>
      ) : null}

      <section className="contract-detail-grid">
        <div className="contract-detail-column">
          <section className="panel">
            <h3>Files</h3>
            <p className="muted">Drag and drop files to reorder pages/attachments inside this contract.</p>
            {files.length === 0 ? <p className="muted">No files uploaded yet.</p> : null}
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
                    <span className="file-mime">{formatMimeTypeLabel(file.mime_type)}</span>
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
              <p className="muted">Drag and drop files here to add them to the bottom, or choose files manually.</p>
              <label>
                Files
                <input
                  type="file"
                  accept="application/pdf,image/jpeg,image/png"
                  multiple
                  onChange={(event) => appendFiles(Array.from(event.target.files ?? []))}
                />
              </label>
              {newFiles.length > 0 ? <p className="muted">Queued: {newFiles.length} file(s)</p> : null}
              <button type="submit" disabled={uploading || newFiles.length === 0}>
                {uploading ? "Uploading..." : "Upload Files"}
              </button>
            </form>

            <div className="page-actions">
              <button type="button" className="secondary" onClick={saveOrder} disabled={!hasUnsavedOrder || savingOrder}>
                {savingOrder ? "Saving..." : "Save Order"}
              </button>
            </div>
            <p className="muted">
              Indexing summary: {statusSummary.indexed} indexed, {statusSummary.processing + statusSummary.ingested} in
              progress, {statusSummary.failed} failed.
            </p>
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

        <section className="panel contract-text-panel" style={contractTextStyle}>
          <div className="contract-text-panel-header">
            <div>
              <h3>{displayMode === "text" ? "Contract Text" : "Original Files"}</h3>
              <p className="muted">
                {displayMode === "text"
                  ? "Combined extracted text from all files, shown in reading order."
                  : "Original uploaded files shown inline in contract order."}
              </p>
            </div>
            <div className="contract-text-controls" aria-label="Contract text display controls">
              <div className="segmented-control" role="tablist" aria-label="Contract display mode">
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
                      onChange={(event) => setTextFontSize(Number(event.target.value))}
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
                      onChange={(event) => setTextLineHeight(Number(event.target.value))}
                    />
                  </label>
                </>
              ) : null}
            </div>
          </div>
          {displayMode === "text" && textLoading ? <p className="muted">Loading contract text...</p> : null}
          {displayMode === "text" && textError ? <p className="error-text">{textError}</p> : null}
          {displayMode === "text" && !textLoading && !textError ? (
            <>
              {files.length === 0 ? <p className="muted">No files yet, so there is no text to show.</p> : null}
              {files.map((file, index) => {
                const entry = documentTexts[file.id];
                return (
                  <section key={file.id} className="word-document-section">
                    <div className="word-document-label">
                      {index + 1}. {file.filename}
                    </div>
                    <article className="word-document-page">
                      {entry?.has_text ? (
                        <p className="word-document-text">{entry.text}</p>
                      ) : (
                        <p className="muted">No extracted text available for this file yet.</p>
                      )}
                    </article>
                  </section>
                );
              })}
            </>
          ) : null}
          {displayMode === "original" ? (
            <>
              {files.length === 0 ? <p className="muted">No files yet, so there is nothing to preview.</p> : null}
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
                      ) : file.mime_type === "image/jpeg" || file.mime_type === "image/png" ? (
                        <img className="original-document-image" src={contentUrl} alt={file.filename} />
                      ) : (
                        <p className="muted">Inline preview is not available for this file type.</p>
                      )}
                    </article>
                  </section>
                );
              })}
            </>
          ) : null}
        </section>
      </section>
    </section>
  );
}
