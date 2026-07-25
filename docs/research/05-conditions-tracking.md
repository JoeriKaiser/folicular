# Condition-Adjacent Tracking (PMDD/PMS, Endometriosis, PCOS)

Sources: S20 (WHO endometriosis), S21 (WHO PCOS), S22 (NHS PMS), S24 (HAS
endometriosis), S25 (DSM-5 reference only).

## Findings

- **PMDD/PMS:** clinical PMDD assessment requires *prospective* daily charting
  across at least two symptomatic cycles; retrospective recall is unreliable
  (S25, S22). The app's role is to enable prospective daily observation with
  dates and severities - nothing more.
- **Endometriosis:** commonly involves pelvic pain (including non-menstrual
  pain), heavy or irregular bleeding, and fatigue; presentation varies widely
  (S20, S24).
- **PCOS:** frequently involves irregular or absent cycles (S21); irregular
  cycles must be first-class data, not treated as missing or broken input.

## Schema impact

- `account_settings.tracking_focus` is a JSON array of user-selected focuses:
  `pms | pmdd | endometriosis | pcos | custom`. These configure which
  observation prompts matter to the user. They are **never** derived from
  data, and no endpoint may treat them as, or combine them into, a diagnosis.
- Daily observation tables (`daily_entries`, `symptom_logs`,
  `bleeding_observations`, `biomarker_observations`) are generic and
  prospective: date-keyed, severity-scaled (1-5), with free-text notes. This
  structure is what condition-aware charting needs without encoding any
  condition logic (S25).
- `symptom_definitions` allows custom symptoms per account so tracking can be
  personalized (e.g., pain locations relevant to endometriosis) without the
  server asserting anything about them.
- No screening rules, thresholds, pattern detectors, or "you might have"
  outputs exist in the API. Co-occurrence views, if ever added, must use
  neutral language and stay client-side.
