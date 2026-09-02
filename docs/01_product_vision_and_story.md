# 01 — Product Vision & Story: The CrediVect Narrative

## 🎯 The 1-Line Core Purpose

> **CrediVect is an institutional credit risk scoring platform that automates loan underwriting in real time using machine learning, durable workflow orchestration, and legally mandated explainability.**

At its core, CrediVect is the automated financial judge that decides:
1. **"Do we approve or decline this applicant for a loan?"** (Instant Yes, Instant No, or Route to Human Underwriter).
2. **"How much money can we safely lend them?"** (Credit limit sizing based on verified ability to pay).
3. **"What interest rate (APR) should we charge?"** (Risk-based pricing: lower risk borrowers get prime rates; higher risk pay an appropriate risk margin).
4. **"If we say NO, why?"** (Mathematically explaining the exact factors that pulled down the score to comply with consumer credit law).

---

# 📖 The CrediVect Story: Modernizing the Broken Underwriting Pipeline

### The Backdrop: The "Black Box & Brittle Pipe" Crisis
Imagine **AuraPay**, a rapidly scaling digital neo-bank launching instant personal credit lines. Within months, their loan volume surges 10x. But their existing credit origination system is built on a messy web of synchronous REST endpoints and cron jobs. 

Three severe systemic failures emerge:

1. **The 3rd-Party Timeout Blackout**: Credit bureau APIs (Experian, TransUnion, Plaid) frequently suffer 504 gateway timeouts or rate limits. When a bureau hangs, AuraPay’s backend crashes, dropping loan applications mid-flight and leaving customers staring at infinite loading spinners.
2. **The Regulatory Nightmare (FCRA & ECOA Violations)**: AuraPay’s data science team deployed an unconstrained deep learning model that boosts approval rates, but it’s an uninterpretable black box. When the Consumer Financial Protection Bureau (CFPB) audits them, AuraPay cannot explain *why* an applicant was declined, leading to severe regulatory fines for lack of legal **Adverse Action reason codes**.
3. **The Manual Review Black Hole**: When an applicant falls into the "gray zone" (near-prime), their file is dumped into a shared email inbox or spreadsheet. Underwriters take days to review it, during which the applicant abandons the app for a competitor.

---

## The Breakthrough: Enter CrediVect

AuraPay builds **CrediVect** (*Credit + Vector*): a decoupled, event-driven, institutional credit decisioning engine engineered around **Durable Orchestration** and **Transparent Machine Learning**.

CrediVect is architected on four core pillars:

```
[Borrower Application]
         │
         ▼
 ┌──────────────┐     Durable Saga & Retries
 │  Temporal.io │ ──────────────────────────────► [External Bureaus (Experian / Plaid)]
 └──────┬───────┘
        │
        │ Ingests Feature Vectors
        ▼
 ┌──────────────┐     Dual Engine: WoE Scorecard (300-850) + LightGBM
 │  CrediVect   │ ──────────────────────────────► [SHAP Adverse Action Reason Codes]
 └──────┬───────┘
        │
        │ Publishes Decision Events
        ▼
 ┌──────────────┐     Audit Lake, Core Banking,
 │ Apache Kafka │ ──────────────────────────────► [Real-Time Model Drift / PSI Monitor]
 └──────────────┘
```

---

## Act by Act: A Day in the Life of a Loan

To see CrediVect in action, follow **Alex Chen**, a freelance designer applying for a $12,000 line of credit at 11:42 PM on a Saturday:

### Act 1: The Resilient Intake (Temporal Workflow)
1. Alex hits "Submit Application" on the mobile app.
2. The API doesn't run fragile synchronous scripts. Instead, it triggers a **Temporal `UnderwritingWorkflow`** instance (`loan-wf-alex-chen-9821`).
3. **Activity 1 (Bureau Fetch)**: Temporal calls Experian. Experian returns a transient `503 Service Unavailable`. Instead of failing the loan, Temporal’s exponential retry policy waits 1.5 seconds, retries, and fetches the credit report cleanly. Alex never sees an error.

### Act 2: High-Dimensional Scoring (The CrediVect Brain)
4. Temporal hands the enriched financial payload to the **CrediVect Core Engine**.
5. **Knockout Rules**: Age >= 18, no bankruptcy in past 36 months -> **Pass**.
6. **Feature Engineering**: Calculates Debt-to-Income ($DTI = 34\%$), Revolving Credit Utilization ($42\%$), and Inquiries in last 6 months ($2$).
7. **Dual-Model Inference**:
   - **Baseline WoE Scorecard**: Computes an interpretable credit score of **678** (Near-Prime).
   - **LightGBM Model**: Evaluates non-linear interactions, calculating a calibrated Probability of Default ($PD$) of **4.8%**.
8. **SHAP Explainability Engine**: Computes local Shapley values. The top negative risk factors are:
   - *Factor 1*: High revolving balance relative to limit.
   - *Factor 2*: Insufficient length of continuous employment.

### Act 3: The Human-in-the-Loop Pause (Temporal Signal)
9. Because Alex’s score is 678 (within the **Near-Prime Review Band: 660–699**), CrediVect doesn't reject him. 
10. The Temporal workflow enters a durable sleep state, registering a timer for up to **48 hours** and emitting an underwriter task.
11. On Sunday morning, an underwriter reviews Alex's secondary bank cash-flow data on their dashboard and clicks **"Approve with $8,000 limit at 12.4% APR"**.
12. The dashboard fires a **Temporal Signal** (`ReviewerDecisionSignal`) directly into the sleeping workflow, instantly waking it up to complete the disbursement saga.

### Act 4: Real-Time Event Telemetry (Apache Kafka)
13. Once finalized, CrediVect emits structured events to **Apache Kafka**:
    - `credit.application.scored` -> Consumed by the **Model Monitoring Service**, updating the **Population Stability Index (PSI)** in real-time to detect macro-economic borrower shifts.
    - `underwriting.decision.approved` -> Consumed by the Core Banking System to create the loan account and trigger automated fund transfer.
    - `compliance.audit.recorded` -> Appends an immutable snapshot of all inputs, model versions, and SHAP weights to the PostgreSQL compliance ledger.

---

## 🏆 Why This Story Matters (The Pitch)

If an investor, fintech CTO, or recruiter asks: *"What makes CrediVect special compared to standard credit scoring tutorials?"*

| Typical Hobby / Junior Project | CrediVect Enterprise Platform |
| :--- | :--- |
| Single Jupyter Notebook with `train_test_split` | Production-grade REST/gRPC microservice with calibrated $PD$ and 300–850 scoring |
| Direct synchronous API calls (crashes on 3rd party failure) | **Temporal.io** durable execution handling bureau retries and 72-hr human underwriting sagas |
| Black-box predictions with zero legal justification | **SHAP-driven adverse action codes** meeting FCRA/ECOA regulatory requirements |
| Static database writes | **Apache Kafka** event streaming feeding core banking, audit lakes, and telemetry |
| Deploys model and forgets it | Real-time **PSI (Population Stability Index)** monitoring for feature and concept drift |

This gives **CrediVect** a compelling story: **It is not just a statistical formula; it is the resilient nervous system of a modern digital lending institution.**
