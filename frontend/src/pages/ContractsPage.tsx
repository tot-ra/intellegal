import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import {
  apiClient,
  type ContractResponse,
  type DocumentResponse,
  type DocumentStatus
} from "../api/client";
import { formatEuropeanDateTime } from "../app/datetime";

type Filters = {
  status: "all" | DocumentStatus;
  sourceType: "all" | "repository" | "upload" | "api";
  query: string;
  tagsInput: string;
};

export function ContractsPage() {
  const navigate = useNavigate();
  const [filters, setFilters] = useState<Filters>({ status: "all", sourceType: "all", query: "", tagsInput: "" });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [contracts, setContracts] = useState<ContractResponse[]>([]);
  const [documents, setDocuments] = useState<DocumentResponse[]>([]);
  const [selectedContractIds, setSelectedContractIds] = useState<string[]>([]);
  const [deletingContractId, setDeletingContractId] = useState<string | null>(null);
  const selectedTags = useMemo(
    () =>
      Array.from(
        new Set(
          filters.tagsInput
            .split(",")
            .map((tag) => tag.trim())
            .filter((tag) => tag.length > 0)
        )
      ),
    [filters.tagsInput]
  );

  const loadDocuments = async () => {
    setLoading(true);
    setError(null);
    try {
      const contractsResponse = await apiClient.listContracts({ limit: 200, offset: 0 });
      const response = await apiClient.listDocuments({
        status: filters.status === "all" ? undefined : filters.status,
        source_type: filters.sourceType === "all" ? undefined : filters.sourceType,
        limit: 200,
        offset: 0
      });
      setContracts(contractsResponse.items);
      setDocuments(response.items);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to load documents.";
      setError(message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadDocuments();
  }, [filters.status, filters.sourceType, selectedTags]);

  const filteredContracts = useMemo(() => {
    const query = filters.query.trim().toLowerCase();
    const selectedTagSet = new Set(selectedTags.map((tag) => tag.toLowerCase()));
    const matchingContracts = new Set(
      documents.map((document) => document.contract_id).filter((id): id is string => Boolean(id))
    );

    return contracts.filter((contract) => {
      if ((filters.status !== "all" || filters.sourceType !== "all") && !matchingContracts.has(contract.id)) {
        return false;
      }

      if (selectedTagSet.size > 0) {
        const hasMatchingTag = (contract.tags ?? []).some((tag) => selectedTagSet.has(tag.toLowerCase()));
        if (!hasMatchingTag) {
          return false;
        }
      }

      if (query.length === 0) return true;

      return (
        contract.name.toLowerCase().includes(query) ||
        contract.id.toLowerCase().includes(query) ||
        (contract.source_ref ?? "").toLowerCase().includes(query) ||
        (contract.tags ?? []).some((tag) => tag.toLowerCase().includes(query))
      );
    });
  }, [contracts, documents, filters.query, filters.sourceType, filters.status, selectedTags]);

  const representativeDocumentByContract = useMemo(() => {
    const map = new Map<string, DocumentResponse>();
    for (const document of documents) {
      if (!document.contract_id) {
        continue;
      }
      if (!map.has(document.contract_id)) {
        map.set(document.contract_id, document);
      }
    }
    return map;
  }, [documents]);

  const visibleContractIds = useMemo(() => new Set(filteredContracts.map((contract) => contract.id)), [filteredContracts]);
  const selectedVisibleCount = useMemo(
    () => filteredContracts.filter((contract) => selectedContractIds.includes(contract.id)).length,
    [filteredContracts, selectedContractIds]
  );
  const allVisibleSelected = filteredContracts.length > 0 && selectedVisibleCount === filteredContracts.length;
  const selectedDocumentIds = useMemo(
    () =>
      documents
        .filter((document) => document.contract_id && selectedContractIds.includes(document.contract_id))
        .map((document) => document.id),
    [documents, selectedContractIds]
  );
  const selectableContractCount = useMemo(
    () => contracts.filter((contract) => representativeDocumentByContract.has(contract.id)).length,
    [contracts, representativeDocumentByContract]
  );
  const unfilteredView =
    filters.status === "all" && filters.sourceType === "all" && selectedTags.length === 0 && filters.query.trim().length === 0;
  const allContractsSelected =
    unfilteredView && selectableContractCount > 0 && selectedContractIds.length === selectableContractCount;

  useEffect(() => {
    setSelectedContractIds((prev) => prev.filter((id) => visibleContractIds.has(id)));
  }, [visibleContractIds]);

  const compareSelected = () => {
    if (selectedContractIds.length !== 2) {
      setError("Select exactly two contracts to compare.");
      return;
    }

    const [leftContractId, rightContractId] = selectedContractIds;
    const leftDocument = representativeDocumentByContract.get(leftContractId);
    const rightDocument = representativeDocumentByContract.get(rightContractId);
    if (!leftDocument || !rightDocument) {
      setError("Cannot compare selected contracts because one of them has no comparable file.");
      return;
    }

    setError(null);
    const params = new URLSearchParams({ left: leftDocument.id, right: rightDocument.id });
    navigate(`/contracts/compare?${params.toString()}`);
  };

  const toggleCompareSelection = (contractId: string) => {
    setSelectedContractIds((prev) => {
      if (prev.includes(contractId)) {
        return prev.filter((id) => id !== contractId);
      }
      return [...prev, contractId];
    });
  };

  const toggleSelectAllVisible = () => {
    setSelectedContractIds((prev) => {
      if (allVisibleSelected) {
        return prev.filter((id) => !visibleContractIds.has(id));
      }

      const next = new Set(prev);
      for (const contract of filteredContracts) {
        if (representativeDocumentByContract.has(contract.id)) {
          next.add(contract.id);
        }
      }
      return Array.from(next);
    });
  };

  const startGuidelineForSelection = () => {
    const params = new URLSearchParams();

    if (allContractsSelected) {
      params.set("scope", "all");
    } else {
      params.set("scope", "selected");
      for (const documentId of selectedDocumentIds) {
        params.append("documentId", documentId);
      }
    }

    navigate(`/guidelines/run?${params.toString()}`);
  };

  const compareWithSelected = (contractId: string) => {
    const counterpartId = selectedContractIds.find((id) => id !== contractId);
    if (!counterpartId) {
      setError("Select another contract checkbox first, then click Compare.");
      setSelectedContractIds([contractId]);
      return;
    }

    const leftDocument = representativeDocumentByContract.get(counterpartId);
    const rightDocument = representativeDocumentByContract.get(contractId);
    if (!leftDocument || !rightDocument) {
      setError("Cannot compare selected contracts because one of them has no comparable file.");
      return;
    }

    setError(null);
    const params = new URLSearchParams({ left: leftDocument.id, right: rightDocument.id });
    navigate(`/contracts/compare?${params.toString()}`);
  };

  const handleDelete = async (contract: ContractResponse) => {
    const confirmed = window.confirm(
      `Delete "${contract.name}" permanently?\n\nThis will hard-delete all files in the contract and related data.`
    );
    if (!confirmed) {
      return;
    }

    setError(null);
    setDeletingContractId(contract.id);
    try {
      await apiClient.deleteContract(contract.id);
      setContracts((prev) => prev.filter((item) => item.id !== contract.id));
      setDocuments((prev) => prev.filter((item) => item.contract_id !== contract.id));
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to delete document.";
      setError(message);
    } finally {
      setDeletingContractId(null);
    }
  };

  return (
    <section className="page">
      <header className="page-header">
        <h2>Contracts</h2>
        <div className="page-actions">
          {selectedContractIds.length > 0 ? (
            <button type="button" onClick={startGuidelineForSelection} disabled={selectedDocumentIds.length === 0}>
              Check Guidelines
            </button>
          ) : null}
          <button type="button" className="secondary" onClick={compareSelected} disabled={selectedContractIds.length !== 2}>
            Compare Selected
          </button>
          <Link to="/contracts/import" className="button-link secondary">
            Batch Import
          </Link>
          <Link to="/contracts/new" className="button-link">
            New Contract
          </Link>
        </div>
      </header>

      <section className="contracts-list">
        <div className="filter-row">
          <label>
            Status
            <select
              value={filters.status}
              onChange={(event) => setFilters((prev) => ({ ...prev, status: event.target.value as Filters["status"] }))}
            >
              <option value="all">all</option>
              <option value="ingested">ingested</option>
              <option value="processing">processing</option>
              <option value="indexed">indexed</option>
              <option value="failed">failed</option>
            </select>
          </label>
          <label>
            Source
            <select
              value={filters.sourceType}
              onChange={(event) =>
                setFilters((prev) => ({ ...prev, sourceType: event.target.value as Filters["sourceType"] }))
              }
            >
              <option value="all">all</option>
              <option value="upload">upload</option>
              <option value="repository">repository</option>
              <option value="api">api</option>
            </select>
          </label>
          <label>
            Search
            <input
              value={filters.query}
              onChange={(event) => setFilters((prev) => ({ ...prev, query: event.target.value }))}
              placeholder="filename or id"
            />
          </label>
          <label>
            Tags
            <input
              value={filters.tagsInput}
              onChange={(event) => setFilters((prev) => ({ ...prev, tagsInput: event.target.value }))}
              placeholder="filter by tags (comma-separated)"
            />
          </label>
        </div>

        {error ? <p className="error-text">{error}</p> : null}
        {loading ? <p className="muted">Loading documents...</p> : null}
        {!loading && filteredContracts.length === 0 ? <p className="muted">No contracts found.</p> : null}

        {filteredContracts.length > 0 ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th aria-label="Selection">
                    <input
                      type="checkbox"
                      aria-label={allVisibleSelected ? "Deselect visible contracts" : "Select visible contracts"}
                      checked={allVisibleSelected}
                      onChange={toggleSelectAllVisible}
                    />
                  </th>
                  <th>Name</th>
                  <th>Files</th>
                  <th>Tags</th>
                  <th>Created</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {filteredContracts.map((contract) => (
                  <tr key={contract.id}>
                    <td>
                      <input
                        type="checkbox"
                        aria-label={`Select ${contract.name}`}
                        checked={selectedContractIds.includes(contract.id)}
                        onChange={() => toggleCompareSelection(contract.id)}
                        disabled={!representativeDocumentByContract.has(contract.id)}
                      />
                    </td>
                    <td>
                      <strong>
                        <Link to={`/contracts/${encodeURIComponent(contract.id)}/edit`}>{contract.name}</Link>
                      </strong>
                    </td>
                    <td>{contract.file_count}</td>
                    <td>
                      {(contract.tags ?? []).length > 0 ? (
                        <div className="tag-list">
                          {(contract.tags ?? []).map((tag) => (
                            <span key={`${contract.id}-${tag}`} className="chip chip-tag">
                              {tag}
                            </span>
                          ))}
                        </div>
                      ) : (
                        <span className="muted">-</span>
                      )}
                    </td>
                    <td>{formatEuropeanDateTime(contract.created_at)}</td>
                    <td>
                      <button
                        type="button"
                        className="secondary"
                        disabled={!representativeDocumentByContract.has(contract.id)}
                        onClick={() => compareWithSelected(contract.id)}
                      >
                        Compare
                      </button>
                      <button
                        type="button"
                        className="danger"
                        disabled={deletingContractId !== null}
                        onClick={() => void handleDelete(contract)}
                      >
                        {deletingContractId === contract.id ? "Deleting..." : "Delete"}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>
    </section>
  );
}
