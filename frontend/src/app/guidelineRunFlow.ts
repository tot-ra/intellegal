import { apiClient, type CheckResultItem, type DocumentResponse } from "../api/client";
import {
  addAuditEvent,
  setStoredResults,
  upsertStoredRun,
  type StoredGuidelineRule
} from "./localState";
import {
  describeGuidelineRule,
  matchesKeywordTerm,
  normalizeGuidelineRule
} from "./guidelineRules";

type RunGuidelineRuleOptions = {
  rule: StoredGuidelineRule;
  documentIds: string[];
  documents?: DocumentResponse[];
  scope?: "all" | "selected" | "contract";
};

export async function runGuidelineRule({
  rule,
  documentIds,
  documents,
  scope = "selected"
}: RunGuidelineRuleOptions) {
  const normalizedRule = normalizeGuidelineRule(rule);

  if (normalizedRule.rule_type === "keyword_match") {
    const allDocuments = documents ?? (await apiClient.listDocuments({ limit: 200, offset: 0 })).items;
    const targetDocuments = allDocuments.filter((document) => documentIds.includes(document.id));
    const resultItems: CheckResultItem[] = await Promise.all(
      targetDocuments.map(async (document) => {
        const response = await apiClient.getDocumentText(document.id);
        const missingTerms = normalizedRule.required_terms.filter((term) => !matchesKeywordTerm(response.text, term));
        const forbiddenMatches = normalizedRule.forbidden_terms.filter((term) => matchesKeywordTerm(response.text, term));
        const passed = missingTerms.length === 0 && forbiddenMatches.length === 0;
        const summaryParts: string[] = [];

        if (!response.has_text) {
          summaryParts.push("No extracted text is available for this file yet.");
        }
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
      document_ids: documentIds,
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

    return { checkId, executionMode: "local" as const };
  }

  const idempotencyKey = globalThis.crypto?.randomUUID?.() ?? `check-${Date.now()}`;
  const response = await apiClient.startLLMReviewCheck(
    {
      document_ids: documentIds,
      instructions: normalizedRule.instructions
    },
    { idempotencyKey }
  );

  upsertStoredRun({
    check_id: response.check_id,
    check_type: response.check_type,
    execution_mode: "remote",
    status: response.status,
    requested_at: new Date().toISOString(),
    document_ids: documentIds,
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
      document_count: String(documentIds.length),
      rule_name: normalizedRule.name
    }
  });

  return { checkId: response.check_id, executionMode: "remote" as const };
}

export function formatGuidelineRunStatusEmoji(status: "queued" | "running" | "completed" | "failed") {
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

export function formatGuidelineRuleType(ruleType: StoredGuidelineRule["rule_type"]) {
  return ruleType === "keyword_match" ? "Strict keyword check" : "LLM contract review";
}
