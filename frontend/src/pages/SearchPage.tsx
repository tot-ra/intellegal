import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { apiClient, type ContractSearchResultItem } from "../api/client";

type SearchStrategy = "semantic" | "strict";
type SearchResultMode = "sections" | "contracts";

function parseSearchQuery(query: string): { includeTerms: string[]; excludeTerms: string[] } {
  const tokens = query.match(/"[^"]+"|'[^']+'|\S+/g) ?? [];
  const includeTerms: string[] = [];
  const excludeTerms: string[] = [];
  let excludeNext = false;

  for (const token of tokens) {
    const normalized = token.trim().replace(/^['"]|['"]$/g, "");
    if (!normalized) {
      continue;
    }
    if (normalized === "!") {
      excludeNext = true;
      continue;
    }
    if (normalized.startsWith("!") && normalized.length > 1) {
      excludeTerms.push(normalized.slice(1));
      excludeNext = false;
      continue;
    }
    if (excludeNext) {
      excludeTerms.push(normalized);
      excludeNext = false;
      continue;
    }
    includeTerms.push(normalized);
  }

  return { includeTerms, excludeTerms };
}

function buildQuerySummaryText(includeTerms: string[], excludeTerms: string[]): string {
  if (includeTerms.length > 0 && excludeTerms.length > 0) {
    return `matching ${includeTerms.join(", ")} and excluding ${excludeTerms.join(", ")}`;
  }
  if (excludeTerms.length > 0) {
    return `excluding ${excludeTerms.join(", ")}`;
  }
  if (includeTerms.length > 0) {
    return includeTerms.join(", ");
  }
  return "-";
}

function inferSectionHint(snippet: string): string | null {
  const normalized = snippet.replace(/\s+/g, " ").trim();
  if (!normalized) {
    return null;
  }

  const namedMatch = normalized.match(/\b(?:section|clause)\s+([A-Za-z0-9.-]{1,20})\b/i);
  if (namedMatch) {
    return `Section ${namedMatch[1]}`;
  }

  const numericMatch = normalized.match(/\b(\d+(?:\.\d+){1,5})\b/);
  if (numericMatch) {
    return `Section ${numericMatch[1]}`;
  }

  return null;
}

function buildResultLink(item: ContractSearchResultItem): string {
  const params = new URLSearchParams();
  params.set("snippet", item.snippet_text.slice(0, 600));
  params.set("page", String(item.page_number));
  params.set("score", item.score.toFixed(3));
  if (item.contract_id) {
    params.set("contractId", item.contract_id);
  }
  return `/contracts/files/${encodeURIComponent(item.document_id)}?${params.toString()}`;
}

export function SearchPage() {
  const [searchParams] = useSearchParams();
  const [searched, setSearched] = useState(false);
  const [activeStrategy, setActiveStrategy] = useState<SearchStrategy>("semantic");
  const [resultMode, setResultMode] = useState<SearchResultMode>("contracts");
  const [semanticSearching, setSemanticSearching] = useState(false);
  const [strictSearching, setStrictSearching] = useState(false);
  const [semanticError, setSemanticError] = useState<string | null>(null);
  const [strictError, setStrictError] = useState<string | null>(null);
  const [semanticResults, setSemanticResults] = useState<ContractSearchResultItem[]>([]);
  const [strictResults, setStrictResults] = useState<ContractSearchResultItem[]>([]);

  const query = searchParams.get("q")?.trim() ?? "";

  useEffect(() => {
    const run = async () => {
      if (query.length < 2) {
        setSearched(false);
        setSemanticError(null);
        setStrictError(null);
        setSemanticResults([]);
        setStrictResults([]);
        setSemanticSearching(false);
        setStrictSearching(false);
        return;
      }

      setSearched(true);
      if (activeStrategy === "semantic") {
        setSemanticSearching(true);
        setSemanticError(null);
        try {
          const response = await apiClient.searchContractSections({
            query_text: query,
            strategy: "semantic",
            result_mode: resultMode,
            limit: 30
          });
          setSemanticResults(response.items);
        } catch (err) {
          setSemanticResults([]);
          const message = err instanceof Error ? err.message : "Semantic search failed.";
          setSemanticError(message);
        } finally {
          setSemanticSearching(false);
        }
        return;
      }

      setStrictSearching(true);
      setStrictError(null);
      try {
        const response = await apiClient.searchContractSections({
          query_text: query,
          strategy: "strict",
          result_mode: resultMode,
          limit: 30
        });
        setStrictResults(response.items);
      } catch (err) {
        setStrictResults([]);
        const message = err instanceof Error ? err.message : "Strict search failed.";
        setStrictError(message);
      } finally {
        setStrictSearching(false);
      }
    };

    void run();
  }, [activeStrategy, query, resultMode]);

  const selectedResults = activeStrategy === "semantic" ? semanticResults : strictResults;
  const selectedError = activeStrategy === "semantic" ? semanticError : strictError;
  const selectedSearching = activeStrategy === "semantic" ? semanticSearching : strictSearching;
  const { includeTerms, excludeTerms } = useMemo(() => parseSearchQuery(query), [query]);
  const querySummaryText = useMemo(() => buildQuerySummaryText(includeTerms, excludeTerms), [excludeTerms, includeTerms]);

  const resultCountLabel = useMemo(() => {
    if (!searched || query.length < 2) {
      return "Enter at least 2 characters to search contracts.";
    }
    if (selectedSearching) {
      const targetLabel = resultMode === "contracts" ? "contracts" : "sections";
      return activeStrategy === "semantic"
        ? `Searching semantic ${targetLabel}...`
        : `Searching strict text ${targetLabel}...`;
    }
    if (selectedError) {
      return "";
    }
    return `${selectedResults.length} ${resultMode === "contracts" ? "contract" : "section"} matches`;
  }, [activeStrategy, query, resultMode, searched, selectedError, selectedResults.length, selectedSearching]);

  return (
    <section className="page">
      <header className="page-header">
        <h2>Search</h2>
      </header>

      <section className="search-view-panel">
        <p className="muted">
          Query: <strong>{querySummaryText}</strong>
        </p>
        <p className="muted">Prefix a term with <code>!</code> to exclude it, for example <code>payment !late</code>.</p>
        <div className="compare-tabs search-mode-tabs" role="tablist" aria-label="Search result grouping">
          <button
            type="button"
            role="tab"
            aria-selected={resultMode === "contracts"}
            className={resultMode === "contracts" ? "secondary" : undefined}
            onClick={() => setResultMode("contracts")}
          >
            Contracts
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={resultMode === "sections"}
            className={resultMode === "sections" ? "secondary" : undefined}
            onClick={() => setResultMode("sections")}
          >
            Sections
          </button>
        </div>
        {includeTerms.length > 0 ? (
          <div className="search-query-summary" aria-label="Applied search terms">
            <p className="muted">
              Matching:{" "}
              {includeTerms.map((term) => (
                <span key={`include-${term}`} className="chip chip-neutral">
                  {term}
                </span>
              ))}
            </p>
          </div>
        ) : null}
        <div className="compare-tabs search-mode-tabs" role="tablist" aria-label="Search modes">
          <button
            type="button"
            role="tab"
            aria-selected={activeStrategy === "semantic"}
            className={activeStrategy === "semantic" ? "secondary" : undefined}
            onClick={() => setActiveStrategy("semantic")}
          >
            Similarity
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={activeStrategy === "strict"}
            className={activeStrategy === "strict" ? "secondary" : undefined}
            onClick={() => setActiveStrategy("strict")}
          >
            Strict
          </button>
        </div>
        {resultCountLabel ? <p className="muted">{resultCountLabel}</p> : null}
        {resultMode === "contracts" ? (
          <p className="muted">Contract mode keeps the strongest match per contract so the result list stays compact.</p>
        ) : (
          <p className="muted">Section mode shows each matching chunk separately, like the existing search behavior.</p>
        )}
        {selectedError ? <p className="error-text">{selectedError}</p> : null}

        {selectedResults.length > 0 ? (
          <div className="search-results-grid">
            {selectedResults.map((item, index) => {
              const sectionHint = inferSectionHint(item.snippet_text);
              return (
                <article key={`${item.document_id}-${item.chunk_id ?? index}`} className="search-result-card">
                  <div className="search-result-head">
                    <strong>{item.filename}</strong>
                    <span className="chip chip-neutral">Score {item.score.toFixed(3)}</span>
                  </div>
                  <p className="muted">
                    {resultMode === "contracts" ? "Best match" : "Page"} {item.page_number} | Document <code>{item.document_id}</code>
                  </p>
                  {sectionHint ? <p className="search-section-hint">{sectionHint}</p> : null}
                  <p>{item.snippet_text}</p>
                  <div className="search-result-actions">
                    {resultMode === "sections" ? (
                      <Link className="button-link secondary" to={buildResultLink(item)}>
                        Open Match
                      </Link>
                    ) : null}
                    {item.contract_id ? (
                      <Link className="button-link secondary" to={`/contracts/${encodeURIComponent(item.contract_id)}/edit`}>
                        Open Contract
                      </Link>
                    ) : (
                      <Link className="button-link secondary" to={buildResultLink(item)}>
                        Open File
                      </Link>
                    )}
                  </div>
                </article>
              );
            })}
          </div>
        ) : null}
      </section>
    </section>
  );
}
