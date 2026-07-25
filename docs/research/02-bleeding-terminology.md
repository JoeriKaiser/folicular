# Bleeding Observation Terminology

Sources: S01 (FIGO 2011), S02 (PALM-COEIN), S23 (NHS Periods).

## Findings

- FIGO standardizes terminology: **heavy menstrual bleeding (HMB)** replaces
  "menorrhagia"; bleeding occurring between cycles is **intermenstrual
  bleeding (IMB)**, colloquially spotting (S01).
- FIGO's PALM-COEIN system classifies *causes* of abnormal uterine bleeding
  into structural (Polyp, Adenomyosis, Leiomyoma, Malignancy) and
  non-structural (Coagulopathy, Ovulatory dysfunction, Endometrial,
  Iatrogenic, Not classified) categories (S02). This is a clinical
  classification tool; a tracking app must not implement it as detection.
- Flow volume is practically unmeasurable by users; product counts and
  self-rated flow (light/normal/heavy) are the realistic self-observation
  signals (S01, S23).

## Schema impact

- `bleeding_observations.flow` enum: `none | spotting | light | medium |
  heavy` - self-rated, aligned with client `BleedingIntensity`.
- `intermenstrual` flag captures FIGO IMB without labeling the user.
- `product_count` is optional and bounded; it supports personal patterns, not
  volume diagnosis.
- No PALM-COEIN taxonomy exists in the schema. Bleeding rows are neutral
  observations with no cause field and no causal language in any response.
