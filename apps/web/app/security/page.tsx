import type { Metadata } from "next";

import { LegalPage, type LegalSection } from "@/components/marketing/LegalPage";

export const metadata: Metadata = {
  title: "Security",
  description: "How NEXIS protects your data, your systems, and your trust.",
};

const SECTIONS: LegalSection[] = [
  {
    id: "principles",
    title: "Our principles",
    body: [
      {
        kind: "p",
        text: "Security at NEXIS starts from a single premise: the Service operates on production systems, often via privileged credentials, and therefore must hold itself to a higher bar than a typical SaaS workflow product. Every architectural decision we make is filtered through that lens. This page is intended to give security reviewers, compliance teams, and engineering buyers a clear, current view of how we work.",
      },
      {
        kind: "ul",
        items: [
          "Least privilege — every credential, every API call, every agent run.",
          "Short-lived secrets — production credentials live in Vault and are scoped to ephemeral workloads.",
          "Defence in depth — we do not rely on any single control to keep customer content safe.",
          "Transparency — we publish subprocessors, retention windows, and incident histories.",
          "Verifiability — every claim on this page is backed by an artefact we can produce on request.",
        ],
      },
    ],
  },
  {
    id: "infrastructure",
    title: "Infrastructure",
    body: [
      {
        kind: "p",
        text: "The Service runs on Amazon Web Services in two regions today (us-east-1 default, eu-west-1 on Enterprise) with active-active deployment across three Availability Zones in each region. All control-plane services are containerised, immutable, and continuously rotated; the underlying nodes are replaced on a 14-day cycle to reduce dwell time for any compromised image.",
      },
      {
        kind: "table",
        headers: ["Layer", "Technology", "Notes"],
        rows: [
          ["Compute", "AWS EKS (Kubernetes)", "Pod Security Standards (restricted), node IAM via IRSA"],
          ["Networking", "AWS VPC + Cilium", "All east-west traffic mutual-TLS, default deny"],
          ["Data plane", "AWS RDS Postgres + S3", "AES-256 at rest, TLS 1.3 in transit, customer-managed KMS available"],
          ["Secrets", "HashiCorp Vault", "Per-workload AppRoles, short-lived dynamic credentials"],
          ["Observability", "Honeycomb + OTel", "No customer content shipped to third parties"],
        ],
      },
    ],
  },
  {
    id: "agents",
    title: "Agent runtime isolation",
    body: [
      {
        kind: "p",
        text: "Each agent run executes inside an ephemeral Docker container with no inbound network and an outbound allow-list scoped to the integrations the run actually needs. Containers are destroyed at the end of every run; nothing persists locally. The shadow pipeline that validates patches operates in a separately scoped, network-isolated Kubernetes namespace so that broken candidates cannot reach production data planes.",
      },
      {
        kind: "p",
        text: "Customer source code is loaded into memory at the start of a run and is referenced by deterministic content hashes for the duration of that run. Content references fall out of cache within minutes of the run completing. We never write customer source code to durable storage.",
      },
    ],
  },
  {
    id: "access",
    title: "Access control",
    body: [
      {
        kind: "p",
        text: "Our internal access to the Service is governed by the same principles we ask our customers to apply: per-environment SSO, short-lived credentials, MFA on every privileged surface, and continuous logging of every privileged action. Production database access is gated by break-glass approvals that require two engineers to acknowledge.",
      },
      {
        kind: "ul",
        items: [
          "All employees on SSO + hardware-key MFA (no SMS).",
          "Production data only accessible via audited break-glass workflows.",
          "All deploys are signed and verified at admission time (cosign + sigstore).",
          "Quarterly access reviews; access lapses immediately on offboarding.",
        ],
      },
    ],
  },
  {
    id: "data",
    title: "Data handling",
    body: [
      {
        kind: "p",
        text: "Our privacy policy covers what we collect and why. From a security perspective, the most important commitments are:",
      },
      {
        kind: "ul",
        items: [
          "Customer source code is processed in memory only — never persisted to durable storage.",
          "Audit logs persist references (content hashes, paths, commit SHAs) but not raw content.",
          "Customer-content cache evicts within minutes of run completion.",
          "All data at rest is encrypted with AES-256 keys managed in AWS KMS; Enterprise customers may supply their own KMS keys.",
        ],
      },
    ],
  },
  {
    id: "vulnerability",
    title: "Vulnerability management",
    body: [
      {
        kind: "p",
        text: "We scan every container image at build time and every dependency at PR open. Findings rated High or Critical block merge. We patch known-exploited vulnerabilities within 24 hours of disclosure for production-facing services and within 7 days for internal tooling.",
      },
      {
        kind: "p",
        text: "We commission an independent penetration test annually. The most recent report (2026 Q1) is available under NDA — email security@nexis.dev to request it.",
      },
    ],
  },
  {
    id: "incident",
    title: "Incident response",
    body: [
      {
        kind: "p",
        text: "We run a 24/7 on-call rotation for the Service itself. Severity-0 incidents — those that affect data integrity, confidentiality, or availability for multiple Workspaces — are escalated to the security lead within five minutes of detection. Workspace Owners are notified within 60 minutes of an incident being confirmed.",
      },
      {
        kind: "p",
        text: "Post-incident reviews are published at /security/incidents within 14 days of resolution. Incidents that affect customer data trigger contractual breach notification under our Data Protection Addendum (available on request).",
      },
    ],
  },
  {
    id: "compliance",
    title: "Compliance",
    body: [
      {
        kind: "p",
        text: "We are SOC 2 Type I in progress (audit window opens 2026 Q3). SOC 2 Type II is planned for fiscal year 2027. ISO 27001 and HIPAA are on our roadmap subject to customer demand.",
      },
      {
        kind: "p",
        text: "For organisations with their own compliance requirements, we are happy to complete security questionnaires (SIG, CAIQ, custom) and to sign mutually agreed Business Associate or Data Processing Addenda.",
      },
    ],
  },
  {
    id: "disclosure",
    title: "Responsible disclosure",
    body: [
      {
        kind: "p",
        text: "We welcome reports from independent security researchers and operate a coordinated disclosure programme. Submit reports to security@nexis.dev encrypted with our PGP key (fingerprint at /security/pgp). We commit to:",
      },
      {
        kind: "ul",
        items: [
          "Acknowledging your report within two business days.",
          "Providing a triage update within five business days.",
          "Crediting you in our public hall-of-fame at /security/researchers (with permission).",
          "Not taking legal action for good-faith research that follows the rules at /security/safe-harbour.",
        ],
      },
    ],
  },
  {
    id: "legal-process",
    title: "Legal process",
    body: [
      {
        kind: "p",
        text: "We require legal process before disclosing customer data to a government authority. Subpoenas, court orders, and warrants should be served on our registered agent of record; full instructions at /security/legal-process. We will notify the affected Workspace Owner before disclosing data unless legally prohibited.",
      },
    ],
  },
  {
    id: "contact",
    title: "Contacting our security team",
    body: [
      {
        kind: "p",
        text: "Email security@nexis.dev. For sensitive material, please use our PGP key. We monitor the inbox 24/7 and triage every report.",
      },
    ],
  },
];

export default function SecurityPage() {
  return (
    <LegalPage
      eyebrow="SECURITY"
      title="How we protect your data, your systems, and your trust"
      effective="May 12, 2026"
      sections={SECTIONS}
    />
  );
}
