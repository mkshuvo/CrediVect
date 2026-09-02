# CrediVect — Project Documentation Suite

Welcome to the **CrediVect** engineering and domain knowledge base. This documentation captures the product vision, financial standards, domain terminology, execution mechanics, and system architecture.

---

## 📚 Document Index

0. **[00. Master Implementation Plan](./00_final_implementation_plan.md)** (or in root: PLAN.md)
   - The complete, authoritative technical specification and implementation plan.
   - Pinned zero-vulnerability Docker manifest (all 9 services).
   - Package dependency vulnerability audit (Next.js 16.3.4, React 19.2.8, MUI v6).
   - Pluggable auth architecture (Local session -> Clerk / Auth0).
   - Target repository scaffolding & execution roadmap.

1. **[01. Product Vision & Story](./01_product_vision_and_story.md)**
   - The 1-line core purpose of CrediVect.
   - The "AuraPay" case study: why legacy fintech underwriting systems fail.
   - The 4 architectural pillars solving the crisis.

2. **[02. Terminology & Financial Glossary](./02_terminology_and_glossary.md)**
   - Complete dictionary of credit underwriting concepts ($PD$, Credit Score, DTI, BPS, Minor Units).
   - Regulatory frameworks (FCRA, ECOA, CFPB, Adverse Action).
   - Machine learning algorithms (WoE Scorecard, LightGBM, TreeSHAP, PSI/CSI drift).
   - Distributed systems patterns (Temporal durable workflows, Kafka streaming, Dragonfly caching, Merkle audit chains).

3. **[03. Step-by-Step Execution Flow](./03_execution_flow_in_chunks.md)**
   - The end-to-end loan application lifecycle broken down into 8 bite-sized chunks.
   - Concrete walkthrough following applicant Alex Chen from submission to disbursement.

4. **[04. Financial Engineering & Data Invariants](./04_financial_engineering_standards.md)**
   - The "Zero-Float" Golden Rule (IEEE-754 ban, integer minor cents, basis points, Banker's Rounding).
   - Primary key architecture: Time-ordered UUIDv7 (RFC 9562).
   - Uber LedgerStore append-only immutability & cryptographic Merkle hash chaining.
   - Bitemporal timestamps and Optimistic Concurrency Control (OCC).
   - Complete Entity-Relationship (ER) diagram and SQL data types.

5. **[05. Implementation Walkthrough & Verification](./05_implementation_walkthrough.md)**
   - Verification report across Python, Go, and Next.js tiers.
   - Test suites, security audits (0 npm vulnerabilities), and latency benchmarks.
