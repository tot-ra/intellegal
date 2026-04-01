import {
  type DragEvent,
  type FormEvent,
  type KeyboardEvent,
  useRef,
  useState,
} from "react";
import { Link, useNavigate } from "react-router-dom";
import { apiClient, type ContractLanguage } from "../api/client";
import {
  addAuditEvent,
  enqueuePendingAutoGuidelineRuns,
  listStoredGuidelineRules,
} from "../app/localState";
import {
  deriveContractNameFromFile,
  isRecoverableProcessingError,
  isSupportedContractFile,
  parseTagsInput,
  toBase64,
} from "./contractUpload";

export function NewContractPage() {
  const navigate = useNavigate();
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const [contractName, setContractName] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [isDragOver, setIsDragOver] = useState(false);
  const [tagsInput, setTagsInput] = useState("");
  const [language, setLanguage] = useState<ContractLanguage>("eng");
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const sourceType = "upload" as const;

  const appendFiles = (incomingFiles: File[]) => {
    if (incomingFiles.length === 0) return;

    setFiles((prev) => {
      const next = [...prev];
      const existing = new Set(
        prev.map((file) => `${file.name}:${file.size}:${file.lastModified}`),
      );

      for (const file of incomingFiles) {
        const key = `${file.name}:${file.size}:${file.lastModified}`;
        if (!existing.has(key)) {
          existing.add(key);
          next.push(file);
        }
      }

      return next;
    });
    setUploadError(null);
  };

  const removeFile = (fileToRemove: File) => {
    setFiles((prev) =>
      prev.filter(
        (file) =>
          !(
            file.name === fileToRemove.name &&
            file.size === fileToRemove.size &&
            file.lastModified === fileToRemove.lastModified
          ),
      ),
    );
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

  const uploadDocument = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const resolvedContractName =
      contractName.trim() || deriveContractNameFromFile(files[0]);

    if (files.length === 0) {
      setUploadError("Select one or more files first.");
      return;
    }
    if (!resolvedContractName) {
      setUploadError(
        "Contract name could not be derived from the selected file.",
      );
      return;
    }
    for (const file of files) {
      if (!isSupportedContractFile(file)) {
        setUploadError("Only PDF, JPEG, PNG, and DOCX files are supported.");
        return;
      }
    }

    setUploading(true);
    setUploadError(null);

    let createdContractId: string | null = null;
    const uploadedDocumentIds: string[] = [];

    try {
      const tags = parseTagsInput(tagsInput);
      const contract = await apiClient.createContract(
        {
          name: resolvedContractName,
          language,
          source_type: sourceType,
          tags: tags.length > 0 ? tags : undefined,
        },
        {
          idempotencyKey:
            globalThis.crypto?.randomUUID?.() ?? `contract-${Date.now()}`,
        },
      );
      createdContractId = contract.id;
      let processingFailureCount = 0;

      for (const file of files) {
        const contentBase64 = await toBase64(file);
        try {
          const uploadedDocument = await apiClient.addContractFile(
            contract.id,
            {
              filename: file.name,
              mime_type: file.type as
                | "application/pdf"
                | "image/jpeg"
                | "image/png"
                | "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
              source_type: sourceType,
              tags: tags.length > 0 ? tags : undefined,
              content_base64: contentBase64,
            },
            {
              idempotencyKey:
                globalThis.crypto?.randomUUID?.() ??
                `upload-${Date.now()}-${file.name}`,
            },
          );
          uploadedDocumentIds.push(uploadedDocument.id);
        } catch (err) {
          if (isRecoverableProcessingError(err)) {
            processingFailureCount += 1;
            continue;
          }
          throw err;
        }
      }

      addAuditEvent({
        type: "contract.created",
        message: `Created contract ${contract.name}`,
        metadata: {
          contract_id: contract.id,
          file_count: String(files.length),
        },
      });

      const autoRunRuleIds = listStoredGuidelineRules()
        .filter((rule) => rule.auto_run_on_new_contract)
        .map((rule) => rule.id);
      if (uploadedDocumentIds.length > 0 && autoRunRuleIds.length > 0) {
        enqueuePendingAutoGuidelineRuns(contract.id, autoRunRuleIds);
      }

      const notice =
        autoRunRuleIds.length > 0
          ? `Contract created. ${autoRunRuleIds.length} automatic guideline check(s) will run in the Files section when processing is ready.`
          : processingFailureCount > 0
            ? `Contract created. ${processingFailureCount} file(s) uploaded with text-processing issues; track status here.`
            : "Contract created. Indexing status will update here.";
      navigate(
        `/contracts/${encodeURIComponent(contract.id)}/edit?notice=${encodeURIComponent(notice)}`,
      );
    } catch (err) {
      if (createdContractId) {
        const message =
          err instanceof Error
            ? err.message
            : "File upload failed after contract creation.";
        navigate(
          `/contracts/${encodeURIComponent(createdContractId)}/edit?notice=${encodeURIComponent(
            `Contract was created, but some file steps failed: ${message}`,
          )}`,
        );
        return;
      }
      const message = err instanceof Error ? err.message : "Upload failed.";
      setUploadError(message);
    } finally {
      setUploading(false);
    }
  };

  return (
    <section className="page">
      <header className="page-header">
        <h2>New Contract</h2>
        <Link to="/contracts" className="button-link secondary">
          Back to Contracts
        </Link>
      </header>

      <form className="panel" onSubmit={uploadDocument}>
        <h3>Upload Contract</h3>
        <p className="muted">
          Use this when several files belong to one contract. For
          one-file-per-contract imports, use Batch Import.
        </p>
        <div className="form-grid form-grid-single-column">
          <label>
            Contract Name (optional)
            <input
              value={contractName}
              onChange={(event) => setContractName(event.target.value)}
              placeholder="Leave blank to use the first file name"
            />
          </label>
          <label>
            Contract Language
            <select
              aria-label="Contract Language"
              value={language}
              onChange={(event) =>
                setLanguage(event.target.value as ContractLanguage)
              }
            >
              <option value="eng">English</option>
              <option value="est">Estonian</option>
              <option value="rus">Russian</option>
            </select>
            <span className="muted">
              Used as the OCR hint for scanned PDFs and image files.
            </span>
          </label>
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
                accept="application/pdf,image/jpeg,image/png,application/vnd.openxmlformats-officedocument.wordprocessingml.document,.docx"
                multiple
                onChange={(event) => {
                  appendFiles(Array.from(event.target.files ?? []));
                  event.target.value = "";
                }}
              />
              <p className="upload-dropzone-title">
                Drop files here or click to browse
              </p>
              <p className="muted">
                Supports multiple PDF, JPEG, PNG, and DOCX files.
              </p>
              <button
                type="button"
                className="secondary upload-dropzone-action"
              >
                Choose Files
              </button>
            </div>
            {files.length > 0 ? (
              <div className="selected-upload-list" aria-live="polite">
                {files.map((file) => (
                  <div
                    key={`${file.name}-${file.size}-${file.lastModified}`}
                    className="selected-upload-item"
                  >
                    <div>
                      <span className="selected-upload-name">{file.name}</span>
                      <span className="selected-upload-meta">
                        {(file.size / 1024 / 1024).toFixed(2)} MB
                      </span>
                    </div>
                    <button
                      type="button"
                      className="secondary"
                      onClick={(event) => {
                        event.stopPropagation();
                        removeFile(file);
                      }}
                    >
                      Remove
                    </button>
                  </div>
                ))}
              </div>
            ) : (
              <p className="muted">No files selected yet.</p>
            )}
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
        {files.length > 0 ? (
          <p className="muted">
            Upload order: {files.map((file) => file.name).join(" -> ")}
          </p>
        ) : null}
        {uploadError ? <p className="error-text">{uploadError}</p> : null}
        <div className="form-actions-end">
          <button type="submit" disabled={uploading}>
            {uploading ? "Uploading..." : "Create Contract"}
          </button>
        </div>
      </form>
    </section>
  );
}
