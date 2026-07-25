# Reproductive Aging and Cycle Irregularity

Sources: S11 (STRAW+10, Harlow 2012), S12 (STRAW, Soules 2001), S05 (Treloar
1967), S10 (Cole 2009).

## Findings

- STRAW+10 is the reference staging system for reproductive aging:
  reproductive stages (early, peak, late), menopausal transition (early,
  late), and postmenopause (early, late) (S11).
- The menopausal transition is *defined by cycle changes*: a persistent
  difference of 7+ days between consecutive cycles marks early transition;
  skipped cycles or amenorrhea of 60+ days marks late transition (S11).
- Cycle length distributions shift across life: more variable after menarche
  and during transition, tighter in mid-reproductive years (S05, S10, S13).

## Schema impact

- `account_settings.life_stage` enum mirrors STRAW+10 stages plus `unknown`.
  It is a user-selected setting, never inferred by the server.
- The schema imposes **no canonical cycle model**: cycles may be irregular,
  very long, skipped, or absent. `cycles.end_date` and `length_days` are
  nullable; validation bounds are permissive (S03, S05).
- The estimate engine degrades gracefully: high variability or few cycles
  yields wider windows and lower confidence labels instead of refusing to
  serve or fabricating regularity (S09, S11).
- Perimenopause support is therefore structural (nullable fields, generous
  bounds, variation-aware estimates), not a feature flag.
