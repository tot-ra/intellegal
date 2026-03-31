import type { CheckResultItem, CheckRunResponse, CheckType } from "../api/client";
import {
  normalizeGuidelineRule,
  type GuidelineRuleType,
  type StoredGuidelineRule as GuidelineRuleRecord
} from "./guidelineRules";

const CHECK_RUNS_KEY = "ldi.checkRuns";
const GUIDELINE_RULES_KEY = "ldi.guidelineRules";
const AUDIT_EVENTS_KEY = "ldi.auditEvents";
const RUN_RESULTS_KEY = "ldi.runResults";

type RunStatus = CheckRunResponse["status"];

export type StoredCheckRun = {
  check_id: string;
  check_type: CheckType;
  execution_mode?: "remote" | "local";
  status: RunStatus;
  requested_at: string;
  rule_id?: string;
  rule_name?: string;
  rule_type?: GuidelineRuleType;
  rule_text?: string;
  finished_at?: string;
  failure_reason?: string;
};

export type StoredGuidelineRule = GuidelineRuleRecord;

export type StoredRunResults = {
  check_id: string;
  status: RunStatus;
  items: CheckResultItem[];
  updated_at: string;
};

export type AuditEvent = {
  id: string;
  timestamp: string;
  type: "document.uploaded" | "contract.created" | "check.started" | "check.updated" | "results.loaded" | "run.tracked";
  message: string;
  metadata?: Record<string, string>;
};

function readJson<T>(key: string, fallback: T): T {
  if (
    typeof window === "undefined" ||
    typeof window.localStorage === "undefined" ||
    typeof window.localStorage.getItem !== "function"
  ) {
    return fallback;
  }

  const value = window.localStorage.getItem(key);
  if (!value) {
    return fallback;
  }

  try {
    return JSON.parse(value) as T;
  } catch {
    return fallback;
  }
}

function writeJson<T>(key: string, value: T) {
  if (
    typeof window === "undefined" ||
    typeof window.localStorage === "undefined" ||
    typeof window.localStorage.setItem !== "function"
  ) {
    return;
  }

  window.localStorage.setItem(key, JSON.stringify(value));
}

export function readLocalJson<T>(key: string, fallback: T): T {
  return readJson(key, fallback);
}

export function writeLocalJson<T>(key: string, value: T) {
  writeJson(key, value);
}

export function listStoredRuns(): StoredCheckRun[] {
  const runs = readJson<StoredCheckRun[]>(CHECK_RUNS_KEY, []);
  return [...runs]
    .map((run) => ({ ...run, execution_mode: run.execution_mode ?? "remote" }))
    .sort((a, b) => b.requested_at.localeCompare(a.requested_at));
}

export function upsertStoredRun(run: StoredCheckRun) {
  const runs = readJson<StoredCheckRun[]>(CHECK_RUNS_KEY, []);
  const previous = runs.find((item) => item.check_id === run.check_id);
  const next = runs.filter((item) => item.check_id !== run.check_id);
  next.push({ ...previous, ...run });
  writeJson(CHECK_RUNS_KEY, next);
}

export function getStoredResults(checkId: string): StoredRunResults | null {
  const resultsMap = readJson<Record<string, StoredRunResults>>(RUN_RESULTS_KEY, {});
  return resultsMap[checkId] ?? null;
}

export function setStoredResults(value: StoredRunResults) {
  const resultsMap = readJson<Record<string, StoredRunResults>>(RUN_RESULTS_KEY, {});
  resultsMap[value.check_id] = value;
  writeJson(RUN_RESULTS_KEY, resultsMap);
}

export function listStoredGuidelineRules(): StoredGuidelineRule[] {
  const rules = readJson<StoredGuidelineRule[]>(GUIDELINE_RULES_KEY, []);
  return [...rules].map(normalizeGuidelineRule).sort((a, b) => b.updated_at.localeCompare(a.updated_at));
}

export function getStoredGuidelineRule(ruleId: string): StoredGuidelineRule | null {
  const rules = readJson<StoredGuidelineRule[]>(GUIDELINE_RULES_KEY, []);
  const rule = rules.find((item) => item.id === ruleId);
  return rule ? normalizeGuidelineRule(rule) : null;
}

export function upsertStoredGuidelineRule(rule: StoredGuidelineRule) {
  const rules = readJson<StoredGuidelineRule[]>(GUIDELINE_RULES_KEY, []);
  const next = rules.filter((item) => item.id !== rule.id);
  next.push(rule);
  writeJson(GUIDELINE_RULES_KEY, next);
}

export function deleteStoredGuidelineRule(ruleId: string) {
  const rules = readJson<StoredGuidelineRule[]>(GUIDELINE_RULES_KEY, []);
  const next = rules.filter((item) => item.id !== ruleId);
  writeJson(GUIDELINE_RULES_KEY, next);
}

export function listAuditEvents(): AuditEvent[] {
  const events = readJson<AuditEvent[]>(AUDIT_EVENTS_KEY, []);
  return [...events].sort((a, b) => b.timestamp.localeCompare(a.timestamp));
}

export function addAuditEvent(event: Omit<AuditEvent, "id" | "timestamp">) {
  const events = readJson<AuditEvent[]>(AUDIT_EVENTS_KEY, []);
  const next: AuditEvent = {
    ...event,
    id: globalThis.crypto?.randomUUID?.() ?? `evt-${Date.now()}`,
    timestamp: new Date().toISOString()
  };

  events.push(next);
  writeJson(AUDIT_EVENTS_KEY, events);
}
