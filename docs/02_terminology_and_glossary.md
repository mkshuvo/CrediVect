# 02 — Terminology & Financial Glossary

This document provides definitive definitions for every financial, mathematical, regulatory, and distributed systems term used in CrediVect.

---

## 🏛️ 1. Financial & Credit Underwriting Terms

### Probability of Default ($PD$)
The mathematical likelihood (from $0.000000$ to $1.000000$, or $0\%$ to $100\%$) that a borrower will fail to meet their contractual debt obligations over a specific time horizon (typically 12 months). CrediVect's LightGBM model outputs a calibrated $PD$ as its core quantitative prediction.

### Credit Score (300–850 Scale)
The standardized three-digit integer representation of creditworthiness. Higher scores denote lower risk. CrediVect calculates this by mapping the log-odds of defaulting to the traditional score range:
$$\text{Score} = \text{Offset} - \text{Factor} \times \ln\left(\frac{PD}{1 - PD}\right)$$

### Knockout / Hard Policy Rules
Deterministic, non-negotiable binary pass/fail criteria evaluated *before* any statistical scoring begins. Examples:
- Age $< 18$ years.
- Active bankruptcy within the trailing 36 months.
- Matches on OFAC / international sanctions watchlists.
- Stated Debt-to-Income ($DTI$) $> 60\%$.
Failing any knockout instantly terminates the application as `DECLINED` without wasting money on external credit bureaus or machine learning inference.

### Debt-to-Income ($DTI$)
The ratio of an applicant's total recurring monthly debt obligations to their gross monthly income:
$$DTI = \frac{\text{Total Monthly Debt Obligation}}{\text{Gross Monthly Income}} \times 100$$
In CrediVect, $DTI$ is stored in **Basis Points** (e.g., $34.50\% = 3,450\text{ bps}$). CrediVect caps maximum approved loan amounts so that total projected $DTI$ never exceeds $43.00\%$ ($4,300\text{ bps}$).

### Revolving Credit Utilization
The proportion of total available credit card limits currently drawn by the borrower:
$$\text{Utilization} = \frac{\text{Current Revolving Balances}}{\text{Total Revolving Credit Limits}} \times 100$$
Utilization above $30\%$ ($3,000\text{ bps}$) triggers negative score adjustments; utilization above $75\%$ indicates severe credit distress.

### Hard Inquiries & Inquiry Velocity
A hard inquiry is recorded on a consumer's credit file when a financial institution pulls full credit data for a lending decision. A high volume of hard inquiries within a 6-month window ("Inquiry Velocity") signals credit-seeking desperation and lowers the applicant's score.

### Adverse Action Notice & Reason Codes
Under federal law, whenever a lender denies an applicant or grants credit on less favorable terms than requested, the lender must issue a formal **Adverse Action Notice**. This notice must legally disclose the credit score used, the reporting bureau, and the **top 4 key factors (Reason Codes)** that hurt the score the most (e.g., `AA01: Proportion of balances to credit limits is too high`).

### Risk Bands / Tiers
Categorization thresholds that determine underwriting policy:
- **Super-Prime** (Score $\ge 780, PD < 1.5\%$): Instant approval, baseline APR margin ($+200\text{ bps}$), maximum requested limit.
- **Prime** ($720 \le \text{Score} < 780, PD < 3.5\%$): Instant approval, risk-adjusted margin ($+450\text{ bps}$).
- **Near-Prime** ($660 \le \text{Score} < 720, 3.5\% \le PD < 7.0\%$): Route to Underwriter Review queue with recommended credit limit cap.
- **Subprime** ($600 \le \text{Score} < 660, 7.0\% \le PD < 12.0\%$): Automated decline or high collateral requirement.
- **High Risk** (Score $< 600, PD \ge 12.0\%$): Automated decline with Adverse Action Notice payload.

### Basis Points (BPS)
A unit of measure equal to one hundredth of one percentage point ($0.01\% = 0.0001$).
- $100\text{ bps} = 1.0\%$.
- An interest rate margin of $12.45\%$ is stored as integer `1245`. Basis points eliminate all fractional rounding errors in rate tables.

### Minor Currency Units (Cents)
The smallest divisible denomination of a currency (e.g., cents for USD, EUR, GBP). All monetary values in CrediVect are stored as 64-bit signed integers (`BIGINT` / `int64`) in minor units (e.g., $\$15,000.00$ is stored as `1500000`).

---

## ⚖️ 2. Regulatory & Legal Compliance Terms

### FCRA (Fair Credit Reporting Act — 15 U.S.C. § 1681)
United States federal legislation regulating the collection, accuracy, privacy, and disclosure of consumer credit data. In credit scoring software, the FCRA strictly mandates the generation and delivery of **Adverse Action Notices** with the top 4 exact mathematical reasons whenever an applicant is declined.

### ECOA (Equal Credit Opportunity Act — 15 U.S.C. § 1691 / Regulation B)
Federal civil rights law prohibiting creditors from discriminating against applicants on the basis of race, color, religion, national origin, sex, marital status, age, or receipt of public assistance. ECOA mandates:
1. **Protected Attributes Exclusion**: Demographic features cannot exist in feature vectors.
2. **No Proxy Discrimination**: Neutral variables that act as proxies for protected classes (e.g., specific zip codes) are prohibited.
3. **No "Black Box" Defense**: In 2022/2023, the CFPB officially ruled that lenders cannot excuse lack of explainability by claiming their AI is a "black box."

### Disparate Impact & The Four-Fifths (80%) Rule
A legal doctrine where an underwriting rule is disproportionately adverse to a protected group, even in the absence of discriminatory intent. The 80% rule states that the selection rate for any protected group must be at least $80\%$ of the selection rate for the highest-performing group.

### Basel II / III Accords
International regulatory frameworks governing bank capital requirements and risk measurement. Basel mandates that internal ratings-based (IRB) credit scoring engines demonstrate statistical calibration, out-of-time validation, and auditable governance for default probabilities ($PD$).

---

## 🧠 3. Machine Learning & Quantitative Modeling Terms

### Weight of Evidence (WoE)
A statistical transformation that converts non-linear continuous or categorical features into monotonic log-odds:
$$\text{WoE}_i = \ln\left(\frac{\% \text{Non-Defaulters}_i}{\% \text{Defaulters}_i}\right)$$
WoE standardizes scales, isolates outliers, and ensures linear interpretability required for regulatory scorecards.

### Information Value (IV)
A statistic measuring the overall predictive power of a feature in separating defaulting from non-defaulting borrowers:
$$\text{IV} = \sum_{i=1}^K \left( \% \text{Non-Defaulters}_i - \% \text{Defaulters}_i \right) \times \text{WoE}_i$$
- $\text{IV} < 0.02$: Unpredictive (dropped).
- $0.10 \le \text{IV} < 0.30$: Medium predictive power.
- $\text{IV} \ge 0.30$: Strong predictive power.

### Monotonic Constraints
An algorithmic enforcement in tree-based algorithms (LightGBM) guaranteeing that a feature's effect on predicted default risk only moves in a single logical direction (e.g., as $DTI$ increases, default risk must monotonically increase, never decrease). This ensures models behave rationally and pass bank audits.

### TreeSHAP (SHapley Additive exPlanations)
A game-theoretic algorithm (originating from Lloyd Shapley's Nobel Prize work) that computes the exact marginal contribution of each feature to an individual applicant's score in polynomial time ($< 10\text{ms}$). TreeSHAP provides the mathematical attribution that CrediVect maps directly to FCRA Adverse Action reason codes.

### Probability Calibration (Platt Scaling)
A post-processing sigmoid transformation mapping uncalibrated machine learning tree margins into true empirical probabilities:
$$PD = \frac{1}{1 + e^{A \cdot f(x) + B}}$$
Ensures that if CrediVect assigns a $5.0\%\ PD$ to a cohort, exactly $5\%$ of those borrowers default in reality.

### Population Stability Index (PSI)
A statistical divergence metric tracking whether the distribution of applicants scored today has shifted away from the baseline population on which the model was trained:
$$\text{PSI} = \sum_{k=1}^K \left( \text{Actual}_k - \text{Expected}_k \right) \times \ln\left( \frac{\text{Actual}_k}{\text{Expected}_k} \right)$$
- $\text{PSI} < 0.10$: Stable distribution (no action).
- $0.10 \le \text{PSI} < 0.25$: Moderate drift (warning alert).
- $\text{PSI} \ge 0.25$: Significant population drift (critical alert; model recalibration required).

---

## ⚙️ 4. Distributed Systems & Architecture Terms

### Temporal.io & Durable Execution
A distributed orchestration platform that executes stateful workflows as ordinary code. If a worker process crashes, a server dies, or an external API times out, the workflow automatically resumes on another worker node with its full state and local variables preserved.

### Temporal Activity
A single, retriable, idempotent unit of work within a Temporal workflow (e.g., fetching Experian credit reports or sending a Kafka message). Activities manage network retries, exponential backoffs, and circuit breaking.

### Temporal Signal
An asynchronous event transmitted from an external system into a running Temporal workflow to wake it up or change its state. In CrediVect, when an underwriter clicks "Approve" on their dashboard, the action fires a `ReviewerDecisionSignal` into the sleeping workflow.

### Human-in-the-Loop (HITL)
An architectural pattern where automated software routes borderline edge cases to human specialists for evaluation before finalizing decisions.

### Apache Kafka / Redpanda
A distributed event streaming platform handling real-time publish-subscribe event logs. Redpanda is a C++ Kafka-compatible engine that eliminates JVM and Zookeeper overhead.

### DragonflyDB
A modern, multi-threaded drop-in replacement for Redis. It delivers $25\times$ higher throughput and sub-millisecond tail latencies for rate limiting and 24-hour idempotency key validation.

### Time-Ordered UUIDv7 (RFC 9562)
A 128-bit identifier format that embeds a 48-bit millisecond Unix timestamp in its most significant bits. It provides distributed, collision-free ID generation while preserving B-Tree index locality in PostgreSQL, achieving insert speeds matching sequential integer keys.

### Merkle Hash Chaining (Audit Ledger)
A cryptographic technique where each row in the audit ledger includes the SHA-256 hash of the previous row:
$$\text{entry\_hash} = \text{SHA-256}(\text{previous\_entry\_hash} + \text{payload\_hash} + \text{timestamp})$$
If anyone manually modifies historical database rows, the cryptographic chain breaks, providing mathematically verifiable audit integrity.
