import { type FormEvent, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import {
  addAuditEvent,
  listStoredGuidelineRules,
  setStoredResults,
  upsertStoredRun,
  type StoredGuidelineRule
} from "../app/localState";
import { apiClient, type CheckResultItem, type DocumentResponse } from "../api/client";
import {
  describeGuidelineRule,
  matchesKeywordTerm,
  normalizeGuidelineRule
} from "../app/guidelineRules";

type Scope = "all" | "selected";

export function GuidelineRunPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [documents, setDocuments] = useState<DocumentResponse[]>([]);
  const [rules, setRules] = useState<StoredGuidelineRule[]>(listStoredGuidelineRules());
  const [scope, setScope] = useState<Scope>(() => (searchParams.get("scope") === "selected" ? "selected" : "all"));
  const [selectedIds, setSelectedIds] = useState<string[]>(() => searchParams.getAll("documentId"));
  const [selectedRuleId, setSelectedRuleId] = useState(() => searchParams.get("ruleId") ?? "");
  const [submitting, setSubmitting] = useState(false);
  const [loadingDocs, setLoadingDocs] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setRules(listStoredGuidelineRules());
  }, []);

  useEffect(() => {
    const requestedScope = searchParams.get("scope");
    const nextScope: Scope = requestedScope === "selected" ? "selected" : "all";
    setScope(nextScope);
    setSelectedIds(searchParams.getAll("documentId"));
    setSelectedRuleId(searchParams.get("ruleId") ?? "");
  }, [searchParams]);

  useEffect(() => {
    if (scope !== "selected" || documents.length > 0) {
      setLoadingDocs(false);
      return;
    }

    let cancelled = false;

    const loadDocuments = async () => {
      setLoadingDocs(true);
      try {
        const response = await apiClient.listDocuments({ limit: 200, offset: 0 });
        if (!cancelled) {
          setDocuments(response.items);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to load documents.");
        }
      } finally {
        if (!cancelled) {
          setLoadingDocs(false);
        }
      }
    };

    void loadDocuments();

    return () => {
      cancelled = true;
    };
  }, [documents.length, scope]);

  const selectedRule = useMemo(
    () => rules.find((rule) => rule.id === selectedRuleId) ?? rules[0] ?? null,
    [rules, selectedRuleId]
  );

  const selectedDocumentNames = useMemo(() => {
    if (scope !== "selected") {
      return [];
    }
    return documents.filter((document) => selectedIds.includes(document.id)).map((document) => document.filename);
  }, [documents, scope, selectedIds]);

  const startRun = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!selectedRule) {
      setError("Create a guideline rule before starting an execution.");
      return;
    }

    if (scope === "selected" && selectedIds.length === 0) {
      setError("Select at least one document when using selected scope.");
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      const documentIds = scope === "selected" ? selectedIds : undefined;
      const normalizedRule = normalizeGuidelineRule(selectedRule);

      if (normalizedRule.rule_type === "keyword_match") {
        const allDocuments =
          documents.length > 0 ? documents : (await apiClient.listDocuments({ limit: 200, offset: 0 })).items;
        const targetDocuments =
          scope === "selected" ? allDocuments.filter((document) => selectedIds.includes(document.id)) : allDocuments;
        const resultItems: CheckResultItem[] = await Promise.all(
          targetDocuments.map(async (document) => {
            const response = await apiClient.getDocumentText(document.id);
            const missingTerms = normalizedRule.required_terms.filter((term) => !matchesKeywordTerm(response.text, term));
            const forbiddenMatches = normalizedRule.forbidden_terms.filter((term) =>
              matchesKeywordTerm(response.text, term)
            );
            const passed = missingTerms.length === 0 && forbiddenMatches.length === 0;
            const summaryParts: string[] = [];

            if (missingTerms.length > 0) {
              summaryParts.push(`Missing: ${missingTerms.join(", ")}`);
            }
            if (forbiddenMatches.length > 0) {
              summaryParts.push(`Forbidden matches: ${forbiddenMatches.join(", ")}`);
            }
            if (summaryParts.length === 0) {
              summaryParts.push("All strict keyword checks passed.");
            }

            return {
              document_id: document.id,
              outcome: passed ? "match" : "missing",
              confidence: 1,
              summary: summaryParts.join(". "),
              evidence: []
            } satisfies CheckResultItem;
          })
        );

        const checkId = `local-keyword-${Date.now()}`;
        const requestedAt = new Date().toISOString();

        upsertStoredRun({
          check_id: checkId,
          check_type: "clause_presence",
          execution_mode: "local",
          status: "completed",
          requested_at: requestedAt,
          finished_at: requestedAt,
          rule_id: normalizedRule.id,
          rule_name: normalizedRule.name,
          rule_type: normalizedRule.rule_type,
          rule_text: describeGuidelineRule(normalizedRule)
        });
        addAuditEvent({
          type: "check.started",
          message: `Started guideline "${normalizedRule.name}"`,
          metadata: {
            check_id: checkId,
            scope,
            document_count: String(targetDocuments.length),
            rule_name: normalizedRule.name
          }
        });

        setStoredResults({
          check_id: checkId,
          status: "completed",
          items: resultItems,
          updated_at: requestedAt
        });

        navigate(`/guidelines?checkId=${encodeURIComponent(checkId)}`);
      } else {
        const idempotencyKey = globalThis.crypto?.randomUUID?.() ?? `check-${Date.now()}`;
        const response = await apiClient.startClausePresenceCheck(
          {
            document_ids: documentIds,
            required_clause_text: normalizedRule.instructions
          },
          { idempotencyKey }
        );

        upsertStoredRun({
          check_id: response.check_id,
          check_type: response.check_type,
          execution_mode: "remote",
          status: response.status,
          requested_at: new Date().toISOString(),
          rule_id: normalizedRule.id,
          rule_name: normalizedRule.name,
          rule_type: normalizedRule.rule_type,
          rule_text: describeGuidelineRule(normalizedRule)
        });

        addAuditEvent({
          type: "check.started",
          message: `Started guideline "${normalizedRule.name}"`,
          metadata: {
            check_id: response.check_id,
            scope,
            document_count: String(documentIds?.length ?? documents.length),
            rule_name: normalizedRule.name
          }
        });

        navigate(`/guidelines?checkId=${encodeURIComponent(response.check_id)}`);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to start guideline.");
    } finally {
      setSubmitting(false);
    }
  };

  const createRuleParams = new URLSearchParams();
  if (scope === "selected") {
    createRuleParams.set("scope", "selected");
    for (const documentId of selectedIds) {
      createRuleParams.append("documentId", documentId);
    }
  }

  return (
    <section className="page">
      <header className="page-header">
        <div>
          <h2>Run Guideline</h2>
          <p className="muted">Choose an existing rule and run it against all contracts or a selected set.</p>
        </div>
        <div className="page-actions">
          <Link to="/guidelines" className="button-link secondary">
            Back to Guidelines
          </Link>
          <Link
            to={createRuleParams.toString() ? `/guidelines/new?${createRuleParams.toString()}` : "/guidelines/new"}
            className="button-link"
          >
            New Rule
          </Link>
        </div>
      </header>

      <form className="panel" onSubmit={startRun}>
        <div className="guideline-form">
          <div className="form-grid guideline-form-grid">
            <label className="guideline-field">
              <span className="field-label">Scope</span>
              <select value={scope} onChange={(event) => setScope(event.target.value as Scope)}>
                <option value="all">All contracts</option>
                <option value="selected">Selected contracts</option>
              </select>
            </label>
            {scope === "selected" ? (
              <div className="checkbox-list">
                {loadingDocs ? <p className="muted">Loading documents...</p> : null}
                {selectedDocumentNames.length > 0 ? (
                  selectedDocumentNames.map((filename) => (
                    <div key={filename} className="checkbox-row">
                      {filename}
                    </div>
                  ))
                ) : (
                  <p className="muted">No preselected documents were provided.</p>
                )}
              </div>
            ) : null}
          </div>

          {rules.length > 0 ? (
            <div className="form-grid guideline-form-grid">
              <label className="guideline-field">
                <span className="field-label">Guideline Rule</span>
                <select value={selectedRule?.id ?? ""} onChange={(event) => setSelectedRuleId(event.target.value)}>
                  {rules.map((rule) => (
                    <option key={rule.id} value={rule.id}>
                      {rule.name}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          ) : (
            <p className="muted">
              No guideline rules yet. Create a rule first so you can run it against contracts.
            </p>
          )}

          {selectedRule ? (
            <div className="guideline-type-explainer">
              <strong>{selectedRule.name}</strong>
              <p>{describeGuidelineRule(selectedRule)}</p>
            </div>
          ) : null}
        </div>

        {error ? <p className="error-text">{error}</p> : null}
        <button type="submit" disabled={submitting || !selectedRule}>
          {submitting ? "Starting..." : "Run Guideline"}
        </button>
      </form>
    </section>
  );
}
