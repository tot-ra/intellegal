export type GuidelineRuleType = "llm_review" | "keyword_match";

export type GuidelineRuleTypeDisplay = {
  icon: string;
  label: string;
  tone: "strict" | "llm";
};

export type StoredGuidelineRule = {
  id: string;
  name: string;
  rule_type?: GuidelineRuleType;
  instructions: string;
  auto_run_on_new_contract?: boolean;
  required_terms?: string[];
  forbidden_terms?: string[];
  created_at: string;
  updated_at: string;
};

export type NormalizedGuidelineRule = {
  id: string;
  name: string;
  rule_type: GuidelineRuleType;
  instructions: string;
  auto_run_on_new_contract: boolean;
  required_terms: string[];
  forbidden_terms: string[];
  created_at: string;
  updated_at: string;
};

export function normalizeGuidelineRule(rule: StoredGuidelineRule): NormalizedGuidelineRule {
  return {
    ...rule,
    rule_type: rule.rule_type ?? "llm_review",
    auto_run_on_new_contract: rule.auto_run_on_new_contract ?? false,
    required_terms: dedupeTerms(rule.required_terms ?? []),
    forbidden_terms: dedupeTerms(rule.forbidden_terms ?? [])
  };
}

export function parseKeywordTerms(value: string): string[] {
  return dedupeTerms(
    value
      .split(/\r?\n|,/)
      .map((item) => item.trim())
      .filter(Boolean)
  );
}

export function formatKeywordTerms(terms: string[]): string {
  return terms.join("\n");
}

export function buildKeywordInstructions(requiredTerms: string[], forbiddenTerms: string[]): string {
  const parts: string[] = [];

  if (requiredTerms.length > 0) {
    parts.push(`Must contain: ${requiredTerms.join(", ")}`);
  }
  if (forbiddenTerms.length > 0) {
    parts.push(`Must not contain: ${forbiddenTerms.join(", ")}`);
  }

  return parts.join(". ");
}

export function normalizeKeywordMatchText(value: string): string {
  return value
    .toLocaleLowerCase()
    .replace(/\s+/g, " ")
    .trim();
}

export function matchesKeywordTerm(text: string, term: string): boolean {
  const normalizedText = normalizeKeywordMatchText(text);
  const normalizedTerm = normalizeKeywordMatchText(term);

  if (!normalizedTerm) {
    return false;
  }

  return normalizedText.includes(normalizedTerm);
}

export function describeGuidelineRule(rule: StoredGuidelineRule): string {
  const normalized = normalizeGuidelineRule(rule);

  if (normalized.rule_type === "keyword_match") {
    return buildKeywordInstructions(normalized.required_terms, normalized.forbidden_terms);
  }

  return normalized.instructions;
}

export function getGuidelineRuleTypeDisplay(ruleType: GuidelineRuleType): GuidelineRuleTypeDisplay {
  if (ruleType === "keyword_match") {
    return {
      icon: "🔎",
      label: "Strict keyword check",
      tone: "strict"
    };
  }

  return {
    icon: "🧠",
    label: "LLM contract review",
    tone: "llm"
  };
}

function dedupeTerms(terms: string[]): string[] {
  const seen = new Set<string>();
  const next: string[] = [];

  for (const term of terms) {
    const trimmed = term.trim();
    const key = trimmed.toLocaleLowerCase();
    if (!trimmed || seen.has(key)) {
      continue;
    }
    seen.add(key);
    next.push(trimmed);
  }

  return next;
}
