import { type DragEvent, type FormEvent, type KeyboardEvent, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { addAuditEvent, enqueuePendingAutoGuidelineRuns, listStoredGuidelineRules } from "../app/localState";
import {
  createSingleFileContract,
  isSupportedContractFile,
  parseTagsInput,
  SUPPORTED_CONTRACT_MIME_TYPES
} from "./contractUpload";

type ImportItemState = "queued" | "uploading" | "success" | "warning" | "failed";

type ImportItem = {
  key: string;
  file: File;
  state: ImportItemState;
  message?: string;
  contractId?: string;
};

function toItemKey(file: File): string {
  return `${file.name}:${file.size}:${file.lastModified}`;
}

export function BatchImportContractsPage() {
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const [items, setItems] = useState<ImportItem[]>([]);
  const [tagsInput, setTagsInput] = useState("");
  const [isDragOver, setIsDragOver] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);

  const appendFiles = (incomingFiles: File[]) => {
    if (incomingFiles.length === 0) {
      return;
    }

    setItems((prev) => {
      const next = [...prev];
      const existing = new Set(prev.map((item) => item.key));

      for (const file of incomingFiles) {
        const key = toItemKey(file);
        if (!existing.has(key)) {
          existing.add(key);
          next.push({ key, file, state: "queued" });
        }
      }

      return next;
    });
    setUploadError(null);
  };

  const removeFile = (key: string) => {
    setItems((prev) => prev.filter((item) => item.key !== key));
  };

  const openFilePicker = () => {
    fileInputRef.current?.click();
  };

  const onDropzoneKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      openFilePicker();
    }
  };

  const summary = useMemo(
    () =>
      items.reduce<Record<ImportItemState, number>>(
        (counts, item) => {
          counts[item.state] += 1;
          return counts;
        },
        { queued: 0, uploading: 0, success: 0, warning: 0, failed: 0 }
      ),
    [items]
  );

  const uploadContracts = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (items.length === 0) {
      setUploadError("Select one or more files first.");
      return;
    }

    const unsupportedFile = items.find((item) => !isSupportedContractFile(item.file));
    if (unsupportedFile) {
      setUploadError(`"${unsupportedFile.file.name}" is not supported. Only PDF, JPEG, PNG, and DOCX files are supported.`);
      return;
    }

    setUploading(true);
    setUploadError(null);
    setItems((prev) => prev.map((item) => ({ ...item, state: "queued", message: undefined, contractId: undefined })));

    const tags = parseTagsInput(tagsInput);
    const autoRunRuleIds = listStoredGuidelineRules()
      .filter((rule) => rule.auto_run_on_new_contract)
      .map((rule) => rule.id);

    for (const item of items) {
      setItems((prev) =>
        prev.map((entry) => (entry.key === item.key ? { ...entry, state: "uploading", message: "Uploading..." } : entry))
      );

      try {
        const result = await createSingleFileContract(item.file, tags);
        if (result.uploadedDocument && autoRunRuleIds.length > 0) {
          enqueuePendingAutoGuidelineRuns(result.contract.id, autoRunRuleIds);
        }
        setItems((prev) =>
          prev.map((entry) =>
            entry.key === item.key
              ? {
                  ...entry,
                  state: result.processingIssue ? "warning" : "success",
                  message: result.processingIssue
                    ? "Contract created, but text processing needs attention."
                    : autoRunRuleIds.length > 0
                      ? `Contract created. ${autoRunRuleIds.length} automatic guideline check(s) queued.`
                      : "Contract created.",
                  contractId: result.contract.id
                }
              : entry
          )
        );

        addAuditEvent({
          type: "contract.created",
          message: `Created contract ${result.contract.name}`,
          metadata: { contract_id: result.contract.id, file_count: "1" }
        });
      } catch (err) {
        const message = err instanceof Error ? err.message : "Import failed.";
        setItems((prev) =>
          prev.map((entry) =>
            entry.key === item.key ? { ...entry, state: "failed", message } : entry
          )
        );
      }
    }

    setUploading(false);
  };

  return (
    <section className="page">
      <header className="page-header">
        <h2>Batch Import Contracts</h2>
        <div className="page-actions">
          <Link to="/contracts/new" className="button-link secondary">
            New Contract
          </Link>
          <Link to="/contracts" className="button-link secondary">
            Back to Contracts
          </Link>
        </div>
      </header>

      <form className="panel" onSubmit={uploadContracts}>
        <h3>Upload Files</h3>
        <p className="muted">Each file becomes its own contract. Files are uploaded one by one so partial failures do not stop the whole import.</p>
        <div className="form-grid form-grid-single-column">
          <div className="upload-field">
            <span className="upload-field-label">Files</span>
            <div
              className={`file-upload-dropzone contract-upload-dropzone${isDragOver ? " is-drag-over" : ""}`}
              role="button"
              tabIndex={0}
              onClick={openFilePicker}
              onKeyDown={onDropzoneKeyDown}
              onDragOver={(event: DragEvent<HTMLDivElement>) => {
                event.preventDefault();
                setIsDragOver(true);
              }}
              onDragLeave={() => setIsDragOver(false)}
              onDrop={(event: DragEvent<HTMLDivElement>) => {
                event.preventDefault();
                setIsDragOver(false);
                appendFiles(Array.from(event.dataTransfer.files ?? []));
              }}
            >
              <input
                ref={fileInputRef}
                className="file-upload-input-hidden"
                type="file"
                accept={SUPPORTED_CONTRACT_MIME_TYPES.join(",")}
                multiple
                onChange={(event) => {
                  appendFiles(Array.from(event.target.files ?? []));
                  event.target.value = "";
                }}
              />
              <p className="upload-dropzone-title">Drop files here or click to browse</p>
              <p className="muted">Every file will create a separate contract.</p>
              <button type="button" className="secondary upload-dropzone-action">
                Choose Files
              </button>
            </div>
          </div>
          <label>
            Tags
            <input
              value={tagsInput}
              onChange={(event) => setTagsInput(event.target.value)}
              placeholder="MSA, Vendor, 2026"
            />
          </label>
        </div>
        {items.length > 0 ? (
          <>
            <div className="batch-import-summary">
              <span>{items.length} file(s) queued</span>
              <span>{summary.success} completed</span>
              <span>{summary.warning} warnings</span>
              <span>{summary.failed} failed</span>
            </div>
            <div className="selected-upload-list" aria-live="polite">
              {items.map((item) => (
                <article key={item.key} className={`selected-upload-item batch-import-item batch-import-${item.state}`}>
                  <div>
                    <span className="selected-upload-name">{item.file.name}</span>
                    <span className="selected-upload-meta">
                      {(item.file.size / 1024 / 1024).toFixed(2)} MB
                      {item.contractId ? ` · Contract ${item.contractId}` : ""}
                    </span>
                    {item.message ? (
                      <span className={item.state === "failed" ? "error-text" : item.state === "warning" ? "warning-text" : "muted"}>
                        {item.message}
                      </span>
                    ) : null}
                  </div>
                  <div className="batch-import-item-actions">
                    {item.contractId ? (
                      <Link to={`/contracts/${encodeURIComponent(item.contractId)}/edit`} className="button-link secondary">
                        Open
                      </Link>
                    ) : null}
                    <button type="button" className="secondary" onClick={() => removeFile(item.key)} disabled={uploading}>
                      Remove
                    </button>
                  </div>
                </article>
              ))}
            </div>
          </>
        ) : (
          <p className="muted">No files selected yet.</p>
        )}
        {uploadError ? <p className="error-text">{uploadError}</p> : null}
        <div className="form-actions-end">
          <button type="submit" disabled={uploading}>
            {uploading ? "Importing..." : "Import Contracts"}
          </button>
        </div>
      </form>
    </section>
  );
}
