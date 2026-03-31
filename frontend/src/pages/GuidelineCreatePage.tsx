import { type FormEvent, useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { addAuditEvent, upsertStoredGuidelineRule } from "../app/localState";
import {
  buildKeywordInstructions,
  parseKeywordTerms,
  type GuidelineRuleType
} from "../app/guidelineRules";

export function GuidelineCreatePage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [ruleName, setRuleName] = useState("Estonian legal entity");
  const [ruleType, setRuleType] = useState<GuidelineRuleType>("llm_review");
  const [ruleText, setRuleText] = useState(
    "Check whether the contracting company is clearly identified as an entity operating in the Estonian legal space. Review the company details, legal form, registration references, governing law, and any wording that confirms the company belongs to the Estonian legal framework."
  );
  const [requiredTermsText, setRequiredTermsText] = useState("osaühing\naktsiaselts");
  const [forbiddenTermsText, setForbiddenTermsText] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (searchParams.get("name")) {
      setRuleName(searchParams.get("name") ?? "");
    }
  }, [searchParams]);

  const startCheck = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (ruleName.trim().length < 3) {
      setError("Enter a rule name with at least 3 characters.");
      return;
    }

    const requiredTerms = parseKeywordTerms(requiredTermsText);
    const forbiddenTerms = parseKeywordTerms(forbiddenTermsText);

    if (ruleType === "llm_review" && ruleText.trim().length < 10) {
      setError("Enter rule instructions with at least 10 characters.");
      return;
    }

    if (ruleType === "keyword_match" && requiredTerms.length === 0 && forbiddenTerms.length === 0) {
      setError("Add at least one required or forbidden keyword.");
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      const now = new Date().toISOString();
      const ruleId = globalThis.crypto?.randomUUID?.() ?? `rule-${Date.now()}`;
      upsertStoredGuidelineRule({
        id: ruleId,
        name: ruleName.trim(),
        rule_type: ruleType,
        instructions:
          ruleType === "llm_review"
            ? ruleText.trim()
            : buildKeywordInstructions(requiredTerms, forbiddenTerms),
        required_terms: ruleType === "keyword_match" ? requiredTerms : [],
        forbidden_terms: ruleType === "keyword_match" ? forbiddenTerms : [],
        created_at: now,
        updated_at: now
      });

      addAuditEvent({
        type: "run.tracked",
        message: `Created guideline rule "${ruleName.trim()}"`,
        metadata: {
          rule_name: ruleName.trim()
        }
      });

      const params = new URLSearchParams();
      params.set("ruleId", ruleId);
      if (searchParams.get("scope") === "selected") {
        params.set("scope", "selected");
        for (const documentId of searchParams.getAll("documentId")) {
          params.append("documentId", documentId);
        }
      }

      navigate(`/guidelines/run?${params.toString()}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create guideline rule.");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <section className="page">
      <header className="page-header">
        <div>
          <h2>New Guideline Rule</h2>
          <p className="muted">Create a reusable rule that you can run against different contract selections.</p>
        </div>
        <div className="page-actions">
          <Link to="/guidelines" className="button-link secondary">
            Back to Guidelines
          </Link>
        </div>
      </header>

      <form className="panel" onSubmit={startCheck}>
        <div className="guideline-form">
          <label className="guideline-field">
            <span className="field-label">Rule Name</span>
            <input value={ruleName} onChange={(event) => setRuleName(event.target.value)} required />
          </label>

          <div className="form-grid guideline-form-grid">
            <label className="guideline-field">
              <span className="field-label">Rule Type</span>
              <select value={ruleType} onChange={(event) => setRuleType(event.target.value as GuidelineRuleType)}>
                <option value="llm_review">LLM evaluates the whole contract</option>
                <option value="keyword_match">Strict keyword check</option>
              </select>
            </label>

            {ruleType === "llm_review" ? (
              <label className="guideline-field guideline-field-wide">
                <span className="field-label">Rule Instructions</span>
                <textarea
                  value={ruleText}
                  onChange={(event) => setRuleText(event.target.value)}
                  rows={7}
                  required
                />
              </label>
            ) : (
              <>
                <label className="guideline-field">
                  <span className="field-label">Must Contain Words or Phrases</span>
                  <textarea
                    value={requiredTermsText}
                    onChange={(event) => setRequiredTermsText(event.target.value)}
                    rows={6}
                    placeholder={"payment terms\nEstonian law"}
                  />
                </label>
                <label className="guideline-field">
                  <span className="field-label">Must Not Contain Words or Phrases</span>
                  <textarea
                    value={forbiddenTermsText}
                    onChange={(event) => setForbiddenTermsText(event.target.value)}
                    rows={6}
                    placeholder={"draft only\nunlimited liability"}
                  />
                </label>
                <div className="guideline-type-explainer guideline-field-wide">
                  <strong>Strict keyword matching</strong>
                  <p>
                    Each phrase is matched against the extracted contract text without caring about uppercase or
                    lowercase letters.
                  </p>
                </div>
              </>
            )}
          </div>
        </div>

        {error ? <p className="error-text">{error}</p> : null}
        <button type="submit" disabled={submitting}>{submitting ? "Saving..." : "Save Rule"}</button>
      </form>
    </section>
  );
}
