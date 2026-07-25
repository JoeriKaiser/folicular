# Normal Menstrual Cycle Parameters

Sources: S01 (FIGO 2011), S03 (ACOG 651), S04 (Bull 2019), S05 (Treloar 1967),
S06 (Chiazze 1968), S09 (Creinin 2004), S10 (Cole 2009), S13 (Vollman 1977).

## Findings

- **Cycle day 1** is the first day of full menstrual flow. Preceding spotting
  does not start a cycle (S01).
- **Frequency (cycle length):** FIGO defines the normal adult range as
  24-38 days (S01). ACOG notes 21-45 days is common in adolescents and
  21-35 days typical for adults (S03).
- **Duration of bleeding:** normally 8 days or less (S01).
- **Regularity:** FIGO describes regularity as the variation between the
  longest and shortest cycle over 12 months; variation of roughly 2-20 days
  is considered regular, above that irregular (S01).
- **The 28-day cycle is not the norm:** in >600,000 real-world cycles, mean
  length was about 29 days and only about 12% of cycles were 28 days long
  (S04). Classic studies report most cycles between roughly 25-36 days with
  long tails (S05, S06, S13).
- **Variability is normal:** consecutive cycles commonly differ by several
  days even in women whose cycles fall inside the normal range (S09), and
  variability changes across reproductive life (S05, S10).

## Schema impact

- `cycles.start_date` is CD1 (first day of full flow); spotting is recorded
  separately in `bleeding_observations` with `intermenstrual` flag.
- `cycles.length_days` uses generous plausibility bounds (10-200) rather than
  normative ones; out-of-range cycles are real data, not errors, and are
  filtered only inside the estimate engine.
- No default 28-day assumption anywhere: estimates derive from the user's own
  recorded starts, and with insufficient history the API returns
  `insufficient` rather than a population default (S04).
- Regularity is never stored as a boolean fact about a person; it is computed
  from variation over recent cycles when needed (S01, S09).
