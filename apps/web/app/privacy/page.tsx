import type { Metadata } from "next";

import { LegalPage, type LegalSection } from "@/components/marketing/LegalPage";

export const metadata: Metadata = {
  title: "Privacy policy",
  description: "How NEXIS collects, uses, and protects your data.",
};

const SECTIONS: LegalSection[] = [
  {
    id: "overview",
    title: "Overview",
    body: [
      {
        kind: "p",
        text: "This Privacy Policy explains how NEXIS Eco, Inc. (“NEXIS”, “we”, “us”) collects, stores, uses, and shares information when you use the NEXIS platform, our marketing website, and any related services. We believe privacy practices should be plain enough that the person whose data is at stake can actually read them, so this document avoids unnecessary jargon and links to the underlying systems where useful.",
      },
      {
        kind: "p",
        text: "Throughout this policy, capitalised terms like “Service” and “Workspace” carry the meaning given in our Terms of Service. If anything below conflicts with the Terms, the Terms control — except where this policy gives you more protection, in which case this policy controls.",
      },
      {
        kind: "p",
        text: "We update this policy as the platform evolves. When we make a material change, we will notify every Workspace Owner by email at least fourteen days before the change takes effect and post a redline at /privacy/changes. Continued use after the effective date means you accept the new policy.",
      },
    ],
  },
  {
    id: "categories",
    title: "Categories of information we collect",
    body: [
      {
        kind: "p",
        text: "We collect three broad categories of information. We try to collect the minimum we need to run the Service safely and to comply with our legal obligations, and we delete each category on the schedule described below in § Data retention.",
      },
      {
        kind: "ul",
        items: [
          "Account information — name, email, password hash (Argon2id), workspace memberships, and MFA enrolment state.",
          "Operational telemetry — incident metadata, agent hand-off logs, integration probe results, and audit log rows produced when the Service runs on your behalf.",
          "Customer content — the source code, diffs, schemas, and configurations that NEXIS reads from your integrations to power the recovery loop. We do not retain customer content beyond what is necessary to complete the current recovery, with one exception: audit log rows retain references (not content) to facilitate post-incident review.",
        ],
      },
      {
        kind: "p",
        text: "We never sell, rent, or otherwise commercially share customer content with third parties. We do not train any general-purpose model on customer content. Agent prompts are templated such that customer content is only sent to a model provider during the specific recovery that requires it.",
      },
    ],
  },
  {
    id: "uses",
    title: "How we use information",
    body: [
      {
        kind: "p",
        text: "We use the information we collect for the following purposes:",
      },
      {
        kind: "ul",
        items: [
          "Operating the Service — routing incidents, executing agent runs, generating patches, validating fixes, and routing approvals.",
          "Security — detecting abuse, rate-limiting, investigating compromise, and complying with court orders that meet our legal-process policy at /security#legal-process.",
          "Billing — metering token usage, recovery counts, and producing the line-itemed invoices visible at /console/billing.",
          "Product improvement — aggregating non-identifying telemetry (latency distributions, error rates, severity distributions) to improve agent quality and reliability.",
          "Communications — sending you product updates and security notices. You can opt out of non-essential email; you cannot opt out of security and billing notices while you have an active Workspace.",
        ],
      },
    ],
  },
  {
    id: "subprocessors",
    title: "Subprocessors",
    body: [
      {
        kind: "p",
        text: "We disclose every subprocessor we use to power the Service. When we add a new subprocessor, we publish a notice at /security/subprocessors at least fourteen days before the change takes effect. Workspace Owners can subscribe to a feed of subprocessor changes at the same URL.",
      },
      {
        kind: "table",
        headers: ["Vendor", "Purpose", "Region"],
        rows: [
          ["Amazon Web Services (AWS)", "Compute, storage, networking", "us-east-1, eu-west-1"],
          ["Stripe", "Billing & payment processing", "Global"],
          ["Anthropic", "LLM provider (Claude)", "us-east, configurable"],
          ["Postmark", "Transactional email delivery", "us-east"],
          ["Honeycomb", "Our own observability (no customer content)", "us-east"],
        ],
      },
    ],
  },
  {
    id: "retention",
    title: "Data retention",
    body: [
      {
        kind: "p",
        text: "We retain different categories of data for different periods. The shortest retention applies to customer content; the longest applies to audit log metadata, which we keep for compliance reasons. Free-tier Workspaces have a shorter retention window than paid Workspaces.",
      },
      {
        kind: "table",
        headers: ["Category", "Free retention", "Paid retention"],
        rows: [
          ["Audit logs", "7 days", "90 days (extendable on Enterprise)"],
          ["Incident metadata", "30 days", "365 days"],
          ["Agent hand-off logs", "7 days", "30 days"],
          ["Billing records", "7 years", "7 years"],
          ["Account information", "until deletion + 30 day grace", "until deletion + 30 day grace"],
        ],
      },
      {
        kind: "p",
        text: "Customer content is processed in memory during a recovery and is not stored at rest. When a recovery completes (or is aborted), the agent runtime releases its content references and the data falls out of cache within minutes.",
      },
    ],
  },
  {
    id: "rights",
    title: "Your rights",
    body: [
      {
        kind: "p",
        text: "Depending on where you live, you may have legal rights over your personal data, including the right to access, correct, delete, or export it. We honour these rights for every user, regardless of jurisdiction.",
      },
      {
        kind: "ul",
        items: [
          "Access — export a JSON archive of your account information and audit logs at /console/account/export.",
          "Correct — update your name, email, and workspace memberships from /console/account.",
          "Delete — request deletion at /console/account/delete. Account information is queued for permanent deletion 30 days later, less anything we are legally required to retain (billing records).",
          "Restrict — pause agent runs and integration probes from /console/account; useful during legal holds.",
          "Object — contact privacy@nexis.dev for any processing you believe falls outside this policy.",
        ],
      },
    ],
  },
  {
    id: "international",
    title: "International transfers",
    body: [
      {
        kind: "p",
        text: "NEXIS operates production infrastructure in two regions: us-east-1 (default) and eu-west-1 (available on Enterprise). Customer content is processed in the region attached to the Workspace; cross-region transfer happens only when explicitly configured by the Workspace Owner.",
      },
      {
        kind: "p",
        text: "When data crosses jurisdictions we rely on appropriate safeguards, including Standard Contractual Clauses for transfers from the EEA to the United States. Our Data Protection Addendum is available on request at legal@nexis.dev.",
      },
    ],
  },
  {
    id: "security",
    title: "Security",
    body: [
      {
        kind: "p",
        text: "We hold information in encrypted form at rest (AES-256) and in transit (TLS 1.3). Production secrets live in HashiCorp Vault and are scoped to ephemeral workloads. Our security practices are described in detail at /security; this section covers privacy-relevant security commitments.",
      },
      {
        kind: "ul",
        items: [
          "Argon2id password hashing with per-user salt and tuned cost parameters.",
          "Hardware-backed MFA available for Workspace Owners and SSO-enrolled accounts.",
          "Continuous vulnerability scanning across all production container images.",
          "Annual penetration test by an independent third party; report available under NDA.",
          "SOC 2 Type I in progress; Type II planned for fiscal year 2026.",
        ],
      },
    ],
  },
  {
    id: "children",
    title: "Children",
    body: [
      {
        kind: "p",
        text: "The NEXIS Service is not directed at children under the age of 16. We do not knowingly collect personal data from children. If you believe a child has provided us personal data, email privacy@nexis.dev and we will delete it.",
      },
    ],
  },
  {
    id: "contact",
    title: "Contacting us",
    body: [
      {
        kind: "p",
        text: "Privacy questions can be sent to privacy@nexis.dev. For legal process, see /security#legal-process. For data subject requests, the fastest path is the in-app tooling described in § Your rights; we will fulfil verified requests within 30 days.",
      },
    ],
  },
];

export default function PrivacyPage() {
  return (
    <LegalPage
      eyebrow="LEGAL"
      title="Privacy policy"
      effective="May 12, 2026"
      sections={SECTIONS}
    />
  );
}
