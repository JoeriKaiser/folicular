# Research Source Register

Every domain constant, range, and enum in the folicular schema traces to an
entry here. A source is evidence for a data-structure decision, never
permission to diagnose, screen, or make medical claims.

Companion register: the Android client keeps product/content sources in
`luteal/docs/research/SOURCE_REGISTER.md`. Physiology and terminology sources
live here because the backend schema is their consumer.

## Status definitions

- **Candidate:** identified, not yet reviewed for a decision.
- **Verified:** citation, DOI/URL, and publication context checked.
- **Implemented:** a schema or API decision cites this source.
- **Retired:** no longer current or appropriate.

## Physiology and terminology (inform the schema)

| ID | Source | Reference | Status | Review date | Decision informed |
|----|--------|-----------|--------|-------------|-------------------|
| S01 | FIGO terminology for uterine bleeding | Fraser IS, Critchley HOD, Broder M, Munro MG. *The FIGO Recommendations on Terminologies and Definitions for Normal and Abnormal Uterine Bleeding.* Semin Reprod Med 2011;29(3):242-246. doi:[10.1055/s-0031-1287662](https://doi.org/10.1055/s-0031-1287662) | Implemented | 2026-07-21 | Cycle frequency/duration reference ranges; `cycles.length_days` plausibility bounds; regularity as variation, not a fixed 28-day assumption; bleeding observation vocabulary (`spotting` as intermenstrual bleeding flag) |
| S02 | FIGO PALM-COEIN classification | Munro MG, Critchley HOD, Fraser IS, et al. *The FIGO classification of causes of abnormal uterine bleeding in the reproductive years.* Fertil Steril 2011;95(7):2204-2208. doi:[10.1016/j.fertnstert.2011.03.079](https://doi.org/10.1016/j.fertnstert.2011.03.079) | Implemented | 2026-07-21 | Bleeding stored as neutral self-observation with no causal attribution; no cause taxonomy in schema |
| S03 | ACOG Committee Opinion No. 651 | *Menstruation in Girls and Adolescents: Using the Vital Sign.* Obstet Gynecol 2015;126(6):e1-e6 (reaffirmed). doi:[10.1097/aog.0000000000001215](https://doi.org/10.1097/aog.0000000000001215) | Implemented | 2026-07-21 | Wider adolescent cycle range (21-45 days) justifies permissive `length_days` bounds and `life_stage`-aware validation instead of one canonical cycle |
| S04 | Large-scale real-world cycle data | Bull JR, Rowland SP, Scherwitzl EB, Scherwitzl R, Danielsson KG, Harper J. *Real-world menstrual cycle characteristics of more than 600,000 menstrual cycles.* npj Digital Medicine 2019;2:83. doi:[10.1038/s41746-019-0152-7](https://doi.org/10.1038/s41746-019-0152-7) | Implemented | 2026-07-21 | 28-day cycles are a minority (~12% of observed cycles; mean ~29 days); estimates must be ranges derived from the user's own history, never a population default |
| S05 | Classic cycle variability study | Treloar AE, Boynton RE, Behn BG, Brown BW. *Variation of the human menstrual cycle through reproductive life.* Int J Fertil 1967;12(1-2):77-126. | Verified | 2026-07-21 | Cycle length variability is normal and age-dependent; schema must tolerate irregular, changing, and missing cycles (perimenopause support) |
| S06 | Cycle length distribution study | Chiazze L Jr, Brayer FT, Macisco JJ Jr, Parker MP, Duffy BJ. *The Length and Variability of the Human Menstrual Cycle.* JAMA 1968;203(6):377-380. doi:[10.1001/jama.1968.03140060001001](https://doi.org/10.1001/jama.1968.03140060001001) | Verified | 2026-07-21 | Wide observed spread of cycle lengths (most cycles 25-36 days, tails far beyond); plausibility filter for estimates must be generous, not normative |
| S07 | Cycle phase physiology | Mihm M, Gangooly S, Muttukrishna S. *The normal menstrual cycle in women.* Animal Reproduction Science 2011;124(3-4):229-236. doi:[10.1016/j.anireprosci.2010.08.030](https://doi.org/10.1016/j.anireprosci.2010.08.030) | Implemented | 2026-07-21 | Follicular phase length is the main driver of cycle variability; luteal phase is comparatively stable (~12-14 days); ovulation timing is anchored backward from next menstruation, not fixed at "day 14" - shapes `cyclecalc` |
| S08 | Phase variability study | Fehring RJ, Schneider M, Raviele K. *Variability in the Phases of the Menstrual Cycle.* J Obstet Gynecol Neonatal Nurs 2006;35(3):376-384. doi:[10.1111/j.1552-6909.2006.00051.x](https://doi.org/10.1111/j.1552-6909.2006.00051.x) | Implemented | 2026-07-21 | Ovulation and fertile-window timing vary widely even among regular cycles; estimates are wide windows with uncertainty labels, never point predictions |
| S09 | Cycle regularity analysis | Creinin MD, Keverline S, Meyn LA. *How regular is regular? An analysis of menstrual cycle regularity.* Contraception 2004;70(4):289-292. doi:[10.1016/j.contraception.2004.04.012](https://doi.org/10.1016/j.contraception.2004.04.012) | Verified | 2026-07-21 | Consecutive-cycle variation of several days is common even within "normal" ranges; confidence labels degrade with variability |
| S10 | Cycle variabilities across life | Cole LA, Ladner DG, Byrn FW. *The normal variabilities of the menstrual cycle.* Fertil Steril 2009;91(2):522-527. doi:[10.1016/j.fertnstert.2007.11.073](https://doi.org/10.1016/j.fertnstert.2007.11.073) | Verified | 2026-07-21 | Variability differs by age and reproductive history; no single canonical cycle model; supports `life_stage` setting |
| S11 | STRAW+10 reproductive aging stages | Harlow SD, Gass M, Hall JE, et al. *Executive summary of the Stages of Reproductive Aging Workshop + 10.* Fertil Steril 2012;97(4):843-851. doi:[10.1016/j.fertnstert.2012.01.128](https://doi.org/10.1016/j.fertnstert.2012.01.128) | Implemented | 2026-07-21 | `account_settings.life_stage` enum values; menopausal transition defined by cycle-length change and skipping, so schema must allow irregular and absent cycles without forcing a canonical model |
| S12 | Original STRAW workshop | Soules MR, Sherman S, Parrott E, et al. *Executive summary: Stages of Reproductive Aging Workshop (STRAW).* Menopause 2001;8(6):402-407. doi:[10.1097/00042192-200111000-00004](https://doi.org/10.1097/00042192-200111000-00004) | Verified | 2026-07-21 | Background for S11 |
| S13 | Cycle length reference monograph | Vollman RF. *The Menstrual Cycle.* Major Problems in Obstetrics and Gynecology, Vol 7. Philadelphia: WB Saunders; 1977. | Verified | 2026-07-21 | Age-stratified cycle length distributions; background for permissive validation bounds |

## Conditions and observation vocabulary (tracking focus only)

These sources inform neutral vocabulary and configurable tracking. They must
never be used to infer, screen for, or announce a condition.

| ID | Source | Reference | Status | Review date | Decision informed |
|----|--------|-----------|--------|-------------|-------------------|
| S20 | WHO endometriosis fact sheet | World Health Organization. *Endometriosis.* Fact sheet, 2023. https://www.who.int/news-room/fact-sheets/detail/endometriosis | Verified | 2026-07-21 | Neutral terminology for the `endometriosis` tracking focus; pain and bleeding observations are configurable, not diagnostic |
| S21 | WHO PCOS fact sheet | World Health Organization. *Polycystic ovary syndrome.* Fact sheet. https://www.who.int/news-room/fact-sheets/detail/polycystic-ovary-syndrome | Verified | 2026-07-21 | Neutral terminology for the `pcos` tracking focus; irregular-cycle inclusion as first-class data, not anomaly |
| S22 | NHS pre-menstrual syndrome | NHS. *Pre-menstrual syndrome.* https://www.nhs.uk/conditions/pre-menstrual-syndrome/ | Verified | 2026-07-21 | Observation vocabulary for the `pms`/`pmdd` tracking focuses and base catalog (mood, fatigue, bloating, cramping, abdominal pain, muscle aches, backache, nausea, digestive changes, sleep, breast tenderness, headache, skin) |
| S23 | NHS periods overview | NHS. *Periods.* https://www.nhs.uk/conditions/periods/ | Verified | 2026-07-21 | Plain-language reference for bleeding duration and product-related fields |
| S24 | HAS endometriosis care pathway | Haute Autorité de Santé. *Parcours de soins - Endométriose.* 2017 (updated). https://www.has-sante.fr/jcms/c_2721628/fr/parcours-de-soins-endometriiose | Candidate | 2026-07-21 | French care-context vocabulary; review before any user-facing educational content |
| S25 | DSM-5 PMDD criteria (reference only) | American Psychiatric Association. *DSM-5.* 2013. Premenstrual dysphoric disorder criteria require prospective daily charting over at least two cycles. | Candidate | 2026-07-21 | Justifies storing prospective daily observations with dates and severities; explicitly NOT implemented as detection logic |
| S26 | ACOG premenstrual syndrome guidance | ACOG. *Premenstrual Syndrome (PMS).* https://www.acog.org/womens-health/faqs/premenstrual-syndrome-pms | Verified | 2026-08-13 | Premenstrual lifestyle and nutrition support: hydration, sodium moderation, complex carbohydrates |

## Privacy and data protection

| ID | Source | Reference | Status | Review date | Decision informed |
|----|--------|-----------|--------|-------------|-------------------|
| S30 | CNIL health hub | CNIL. *Santé.* https://www.cnil.fr/fr/sante | Verified | 2026-07-21 | Menstrual/symptom data are "données de santé": minimization, encryption at rest in deployment, no secondary use |
| S31 | CNIL GDPR basics | CNIL. *RGPD : de quoi parle-t-on ?* https://www.cnil.fr/fr/rgpd-de-quoi-parle-t-on | Verified | 2026-07-21 | French privacy baseline vocabulary for docs and consent language |
| S32 | WHO menstrual health statement | World Health Organization. *WHO statement on menstrual health and rights.* 2022. https://www.who.int/news/item/22-06-2022-who-statement-on-menstrual-health-and-rights | Candidate | 2026-07-21 | Inclusive, rights-based framing; review before user-facing content |

## Required follow-up

- Re-verify each DOI/URL and publication status before citing in user-facing
  content; citation verification here confirms existence and metadata, not
  full-text review of every figure.
- Numbers quoted in topic notes are approximate summaries of abstracts and
  summaries; confirm exact figures against full text before publishing them
  in the app.
- Add French public-health sources (Santé publique France, HAS) wherever they
  cover the exact decision.
- Schedule periodic review; record jurisdiction and update dates.
- Obtain domain review before shipping anything that could be read as health
  guidance.
