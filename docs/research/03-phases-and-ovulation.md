# Cycle Phases, Ovulation, and Biomarker Observations

Sources: S07 (Mihm 2011), S08 (Fehring 2006), S04 (Bull 2019).

## Findings

- The cycle comprises menstrual, follicular, ovulatory, and luteal phases.
  **Follicular phase length is the main source of cycle-to-cycle variability**;
  the **luteal phase is comparatively stable**, typically about 12-14 days
  (S07).
- Consequently, ovulation is better estimated **backward from the next
  menstruation** (next menses minus luteal length) than forward from the last
  one ("day 14" is a myth for many cycles) (S07, S08).
- Even among well-characterized cycles, the day of ovulation and the fertile
  window vary widely between women and between cycles (S08). Calendar-based
  timing alone is a weak signal.
- Basal body temperature shows a biphasic shift after ovulation, and cervical
  mucus patterns change across the cycle; these are retrospective or
  probabilistic signals, not certainties, and are user-observed with varying
  quality (background: sympto-thermal method literature; see S08 for phase
  variability context).

## Schema and Domain Impact

- `CyclePhase` values (`menstrual | follicular | ovulatory | luteal`) are
  descriptive labels only; neither the client nor API ever infers ovulation as
  a fact from calendar dates.
- Cycle estimates and fertile window calculations are computed exclusively on
  the client (Luteal) using decrypted records, anchoring ovulation estimates
  backward from next menstruation (`next_menstruation_central - luteal_days`,
  luteal constant 13, widened by cycle variability per S07, S08).
- All ovulation and fertile-window outputs are **windows with uncertainty
  labels**, never point predictions (S08).
- `BiomarkerObservationData` stores BBT (with time and `disturbed` quality
  flag), cervical fluid category, and cervix position/firmness as optional
  daily self-observations sealed under E2EE.
