import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { formatEuropeanDateTime } from "../app/datetime";
import {
  addAuditEvent,
  deleteStoredGuidelineRule,
  getStoredResults,
  listStoredGuidelineRules,
  listStoredRuns,
  setStoredResults,
  type StoredGuidelineRule,
  type StoredCheckRun,
  upsertStoredRun
} from "../app/localState";
import { apiClient, type CheckResultItem, type CheckRunResponse, type CheckType } from "../api/client";
import { describeGuidelineRule } from "../app/guidelineRules";

type SelectedRun = {
  check_id: string;
  check_type: CheckType;
  execution_mode?: "remote" | "local";
  requested_at: string;
  rule_name?: string;
  rule_type?: StoredCheckRun["rule_type"];
  rule_text?: string;
};

export function GuidelinesPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [rules, setRules] = useState<StoredGuidelineRule[]>(listStoredGuidelineRules());
  const [trackedRuns, setTrackedRuns] = useState(listStoredRuns());
  const [selected, setSelected] = useState<SelectedRun | null>(null);
  const [run, setRun] = useState<CheckRunResponse | null>(null);
  const [results, setResults] = useState<CheckResultItem[] | null>(null);
  const [contractNamesById, setContractNamesById] = useState<Record<string, string>>({});
  const [contractIdsByDocumentId, setContractIdsByDocumentId] = useState<Record<string, string>>({});
  const [loadingRun, setLoadingRun] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const lastLoggedStatusByRunRef = useRef<Record<string, CheckRunResponse["status"]>>({});
  const loggedResultsRunsRef = useRef(new Set<string>());

  useEffect(() => {
    const checkId = searchParams.get("checkId") ?? "";

    if (!checkId) {
      return;
    }

    const found = trackedRuns.find((item) => item.check_id === checkId);
    if (found) {
      setSelected((current) => {
        if (current?.check_id === found.check_id) {
          return current;
        }

        return {
          check_id: found.check_id,
          check_type: found.check_type,
          execution_mode: found.execution_mode,
          requested_at: found.requested_at,
          rule_name: found.rule_name,
          rule_type: found.rule_type,
          rule_text: found.rule_text
        };
      });
      return;
    }

    setSelected({
      check_id: checkId,
      check_type: "clause_presence",
      execution_mode: "remote",
      requested_at: new Date().toISOString()
    });
  }, [searchParams, trackedRuns]);

  useEffect(() => {
    let cancelled = false;

    const loadLookups = async () => {
      try {
        const [contractsResponse, documentsResponse] = await Promise.all([
          apiClient.listContracts({ limit: 200, offset: 0 }),
          apiClient.listDocuments({ limit: 200, offset: 0 })
        ]);

        if (cancelled) {
          return;
        }

        setContractNamesById(
          Object.fromEntries(contractsResponse.items.map((contract) => [contract.id, contract.name]))
        );
        setContractIdsByDocumentId(
          Object.fromEntries(
            documentsResponse.items
              .filter((document) => Boolean(document.contract_id))
              .map((document) => [document.id, document.contract_id as string])
          )
        );
      } catch {
        if (!cancelled) {
          setContractNamesById({});
          setContractIdsByDocumentId({});
        }
      }
    };

    void loadLookups();

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!selected) {
      setRun(null);
      setResults(null);
      return;
    }

    let cancelled = false;

    const load = async () => {
      setLoadingRun(true);
      setError(null);
      try {
        if (selected.execution_mode === "local") {
          const cached = getStoredResults(selected.check_id);
          setRun({
            check_id: selected.check_id,
            status: cached?.status ?? "completed",
            check_type: selected.check_type,
            requested_at: selected.requested_at,
            finished_at: selected.requested_at
          });
          setResults(cached?.items ?? []);
          setLoadingRun(false);
          return;
        }

        const runResponse = await apiClient.getCheckRun(selected.check_id);
        if (cancelled) {
          return;
        }

        setRun(runResponse);
        upsertStoredRun(runResponse);

        if (lastLoggedStatusByRunRef.current[runResponse.check_id] !== runResponse.status) {
          addAuditEvent({
            type: "check.updated",
            message: `Fetched guideline status (${runResponse.status})`,
            metadata: { check_id: runResponse.check_id }
          });
          lastLoggedStatusByRunRef.current[runResponse.check_id] = runResponse.status;
        }

        if (runResponse.status === "completed") {
          const response = await apiClient.getCheckResults(selected.check_id);
          if (cancelled) {
            return;
          }

          setResults(response.items);
          setStoredResults({
            check_id: response.check_id,
            status: response.status,
            items: response.items,
            updated_at: new Date().toISOString()
          });

          if (!loggedResultsRunsRef.current.has(response.check_id)) {
            addAuditEvent({
              type: "results.loaded",
              message: `Loaded results for ${response.check_id}`,
              metadata: { item_count: String(response.items.length) }
            });
            loggedResultsRunsRef.current.add(response.check_id);
          }
        } else {
          loggedResultsRunsRef.current.delete(runResponse.check_id);
          const cached = getStoredResults(selected.check_id);
          setResults(cached?.items ?? null);
        }

        setTrackedRuns(listStoredRuns());
      } catch (err) {
        if (cancelled) {
          return;
        }

        const cached = getStoredResults(selected.check_id);
        if (cached) {
          setResults(cached.items);
        }

        setError(err instanceof Error ? err.message : "Failed to load run details.");
      } finally {
        if (!cancelled) {
          setLoadingRun(false);
        }
      }
    };

    void load();

    return () => {
      cancelled = true;
    };
  }, [selected]);

  const flaggedCount = useMemo(() => {
    if (!results) {
      return 0;
    }

    return results.filter((item) => item.outcome === "missing" || item.outcome === "review").length;
  }, [results]);

  const resultRows = useMemo(
    () =>
      (results ?? []).map((item) => {
        const contractId = contractIdsByDocumentId[item.document_id];
        const contractName = contractId ? contractNamesById[contractId] : undefined;

        return {
          ...item,
          contractId,
          contractName
        };
      }),
    [contractIdsByDocumentId, contractNamesById, results]
  );

  const handleDeleteRule = (rule: StoredGuidelineRule) => {
    if (!window.confirm(`Delete guideline rule "${rule.name}"?`)) {
      return;
    }

    deleteStoredGuidelineRule(rule.id);
    setRules(listStoredGuidelineRules());
  };

  return (
    <section className="page">
      <header className="page-header">
        <div>
          <h2>Guidelines</h2>
          <p className="muted">Manage reusable rules separately from running and reviewing guideline checks.</p>
        </div>
      </header>

      <div className="guideline-sections">
        <section className="panel">
          <header className="guideline-section-header">
            <div>
              <h3>Rules</h3>
              <p className="muted">Create and maintain your reusable guideline rules here.</p>
            </div>
            <Link to="/guidelines/new" className="button-link">
              New Rule
            </Link>
          </header>
          {rules.length === 0 ? <p className="muted">No rules created yet.</p> : null}
          <ul className="run-list guideline-rule-list">
            {rules.map((rule) => (
              <li key={rule.id}>
                <div className="guideline-rule-item">
                  <div className="guideline-rule-copy">
                    <strong>{rule.name}</strong>
                    <p className="muted">{describeGuidelineRule(rule)}</p>
                  </div>
                  <div className="guideline-rule-actions">
                    <Link to={`/guidelines/run?ruleId=${encodeURIComponent(rule.id)}`} className="button-link secondary">
                      Run
                    </Link>
                    <button type="button" className="danger" onClick={() => handleDeleteRule(rule)}>
                      Delete
                    </button>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        </section>

        <section className="panel">
          <header className="guideline-section-header">
            <div>
              <h3>Guideline Checks</h3>
              <p className="muted">Run rules against contracts and inspect the outcome details separately from rule setup.</p>
            </div>
            <Link to="/guidelines/run" className="button-link secondary">
              Run Guideline
            </Link>
          </header>
          <div className="split-grid guideline-execution-grid">
            <section className="guideline-run-list-panel">
              {trackedRuns.length === 0 ? <p className="muted">No executions yet.</p> : null}
              <ul className="run-list">
                {trackedRuns.map((item) => (
                  <li key={item.check_id}>
                    <button
                      type="button"
                      className={selected?.check_id === item.check_id ? "run-item active" : "run-item"}
                      onClick={() => {
                        setSearchParams({ checkId: item.check_id });
                        setSelected({
                          check_id: item.check_id,
                          check_type: item.check_type,
                          execution_mode: item.execution_mode,
                          requested_at: item.requested_at,
                          rule_name: item.rule_name,
                          rule_type: item.rule_type,
                          rule_text: item.rule_text
                        });
                      }}
                    >
                      <span className="guideline-run-item-copy">
                        <span className="guideline-run-item-title">
                          <span className="guideline-run-status-emoji" aria-hidden="true">
                            {formatRunStatusEmoji(item.status)}
                          </span>
                          <span>{formatRunLabel(item)}</span>
                        </span>
                        <small>Created {formatEuropeanDateTime(item.requested_at)}</small>
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </section>

            <section className="guideline-run-detail-panel">
              {selected === null ? <p className="muted">Select an execution to inspect details.</p> : null}
              {selected !== null && loadingRun ? <p className="muted">Loading run data...</p> : null}
              {error ? <p className="error-text">{error}</p> : null}
              {run ? (
                <div className="detail-stack guideline-detail-card">
                  <p>
                    <strong>Rule:</strong> {selected?.rule_name ?? "Tracked guideline"}
                  </p>
                  {selected?.rule_type ? (
                    <p>
                      <strong>Type:</strong> {formatRuleType(selected.rule_type)}
                    </p>
                  ) : null}
                  <p>
                    <strong>Status:</strong> {run.status}
                  </p>
                  <p>
                    <strong>Requested:</strong> {formatEuropeanDateTime(run.requested_at)}
                  </p>
                  {selected?.rule_text ? (
                    <p>
                      <strong>Instructions:</strong> {selected.rule_text}
                    </p>
                  ) : null}
                  <p>
                    <strong>Execution ID:</strong> <code>{run.check_id}</code>
                  </p>
                  {run.finished_at ? (
                    <p>
                      <strong>Finished:</strong> {formatEuropeanDateTime(run.finished_at)}
                    </p>
                  ) : null}
                  {run.failure_reason ? (
                    <p>
                      <strong>Failure:</strong> {run.failure_reason}
                    </p>
                  ) : null}
                </div>
              ) : null}

              {results ? (
                <>
                  <p className="muted">Flagged items: {flaggedCount}</p>
                  <div className="table-wrap guideline-results-table">
                    <table>
                      <thead>
                        <tr>
                          <th>Contract</th>
                          <th>Outcome</th>
                          <th>Confidence</th>
                          <th>Summary</th>
                        </tr>
                      </thead>
                      <tbody>
                        {resultRows.map((item) => (
                          <tr
                            key={`${item.document_id}-${item.outcome}`}
                            className={`guideline-result-row guideline-result-row-${item.outcome}`}
                          >
                            <td>
                              {item.contractId && item.contractName ? (
                                <Link to={`/contracts/${encodeURIComponent(item.contractId)}/edit`}>
                                  {item.contractName}
                                </Link>
                              ) : item.contractName ? (
                                item.contractName
                              ) : (
                                <code>{item.document_id}</code>
                              )}
                            </td>
                            <td>{item.outcome}</td>
                            <td>{Math.round(item.confidence * 100)}%</td>
                            <td>
                              {item.summary ?? "-"}
                              {item.evidence?.map((snippet, index) => (
                                <div key={`${snippet.page_number}-${index}`} className="evidence-block">
                                  <small>Page {snippet.page_number}</small>
                                  <p>{snippet.snippet_text}</p>
                                </div>
                              ))}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </>
              ) : null}
            </section>
          </div>
        </section>
      </div>
    </section>
  );
}

function formatRunLabel(item: StoredCheckRun) {
  return item.rule_name?.trim() || "Guideline run";
}

function formatRunStatusEmoji(status: StoredCheckRun["status"]) {
  switch (status) {
    case "queued":
      return "🕒";
    case "running":
      return "⏳";
    case "completed":
      return "✅";
    case "failed":
      return "❌";
    default:
      return "•";
  }
}

function formatRuleType(ruleType: NonNullable<StoredCheckRun["rule_type"]>) {
  return ruleType === "keyword_match" ? "Strict keyword check" : "LLM contract review";
}
