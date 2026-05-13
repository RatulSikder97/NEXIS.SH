import type { Metadata } from "next";

import { LegalPage, type LegalSection } from "@/components/marketing/LegalPage";

export const metadata: Metadata = {
  title: "Terms of service",
  description: "The contract that governs your use of NEXIS.",
};

const SECTIONS: LegalSection[] = [
  {
    id: "acceptance",
    title: "Acceptance of these Terms",
    body: [
      {
        kind: "p",
        text: "These Terms of Service (the “Terms”) govern your use of the NEXIS platform, marketing website, command-line interface, and any related services we offer (collectively, the “Service”). By creating an account, connecting an integration, or otherwise using the Service, you agree to these Terms on behalf of yourself and any organisation you are acting for.",
      },
      {
        kind: "p",
        text: "If you are entering into these Terms on behalf of an organisation, you represent that you have authority to bind that organisation. If you do not have that authority, do not use the Service.",
      },
      {
        kind: "p",
        text: "We may revise these Terms from time to time. The revised version will be posted at /terms and will become effective thirty days after posting; we will email each Workspace Owner before material changes take effect. Continued use after the effective date constitutes acceptance.",
      },
    ],
  },
  {
    id: "service",
    title: "What the Service does",
    body: [
      {
        kind: "p",
        text: "The Service runs a fleet of automated agents that detect anomalies in your software systems, propose patches, validate those patches in isolated environments, and route the resulting diffs through engineer approval. The exact set of agents available to a given Workspace depends on the subscription tier.",
      },
      {
        kind: "p",
        text: "The Service interacts with third-party systems (GitHub, Sentry, Datadog, ArgoCD, Slack, PagerDuty, and others) using credentials you provide. Your use of those third-party systems is governed by those third parties’ own terms; nothing in these Terms modifies or supersedes those.",
      },
    ],
  },
  {
    id: "accounts",
    title: "Accounts, Workspaces, and roles",
    body: [
      {
        kind: "ul",
        items: [
          "Account — a single user identity tied to one email address. Accounts can belong to multiple Workspaces.",
          "Workspace — a billing and access boundary. All Workspaces have a Workspace Owner, who is responsible for billing, access policies, and acceptance of these Terms on the organisation’s behalf.",
          "Project — a scoped unit of work inside a Workspace. Severity routing, approval policies, and audit retention are configured per Project.",
          "Roles — Owner, Approver, Member, Viewer. Roles determine what each Account can do within a given Workspace; see /docs/rbac for the matrix.",
        ],
      },
      {
        kind: "p",
        text: "You are responsible for keeping your credentials secure and for any activity that occurs under your Account. Notify us immediately at security@nexis.dev if you suspect compromise.",
      },
    ],
  },
  {
    id: "use",
    title: "Acceptable use",
    body: [
      {
        kind: "p",
        text: "The Service is built for incident response in software systems you own or have permission to administer. You may not use the Service to:",
      },
      {
        kind: "ul",
        items: [
          "Probe or attack systems you do not have explicit authorisation to access.",
          "Reverse-engineer the Service, except where such restriction is prohibited by law.",
          "Train any general-purpose machine-learning model on the Service’s outputs.",
          "Resell, white-label, or sublicense the Service without our prior written consent.",
          "Knowingly upload malware, illegal content, or data subject to export restrictions you are not authorised to export.",
          "Use the Service in a way that violates applicable law, including laws on privacy, intellectual property, and economic sanctions.",
        ],
      },
    ],
  },
  {
    id: "billing",
    title: "Fees and billing",
    body: [
      {
        kind: "p",
        text: "Paid tiers are billed monthly in arrears. Fees consist of a flat subscription component and metered usage components (LLM tokens, successful recoveries). The exact rates for your tier are published at /pricing and surfaced in /console/billing.",
      },
      {
        kind: "p",
        text: "We charge in U.S. Dollars by default. Enterprise customers may invoice in additional currencies subject to commercial agreement. Late payments accrue interest at 1.5% per month or the maximum rate permitted by law, whichever is less.",
      },
      {
        kind: "p",
        text: "All fees are non-refundable except where required by law. If you dispute a charge, contact billing@nexis.dev within 60 days; we will investigate in good faith and issue a credit or refund where appropriate.",
      },
    ],
  },
  {
    id: "ip",
    title: "Intellectual property",
    body: [
      {
        kind: "p",
        text: "You retain ownership of your source code, configuration, and any other content you submit to the Service. By using the Service you grant NEXIS a narrow licence to process that content for the sole purpose of providing the Service.",
      },
      {
        kind: "p",
        text: "We retain ownership of the Service itself, including all software, models, infrastructure, and documentation. Outputs the Service generates on your behalf — diffs, tests, runbook entries — are yours, subject to your continued payment of fees.",
      },
    ],
  },
  {
    id: "data",
    title: "Customer data and privacy",
    body: [
      {
        kind: "p",
        text: "Our handling of your data is governed by our Privacy Policy at /privacy. Highlights:",
      },
      {
        kind: "ul",
        items: [
          "We do not sell customer content to third parties.",
          "We do not train general-purpose models on customer content.",
          "Customer content is processed in the region attached to your Workspace.",
          "Audit logs and incident metadata are retained per your tier; customer content is processed in memory and not stored at rest.",
        ],
      },
    ],
  },
  {
    id: "warranty",
    title: "Warranty disclaimer",
    body: [
      {
        kind: "p",
        text: "THE SERVICE IS PROVIDED “AS IS” AND “AS AVAILABLE”. NEXIS DISCLAIMS ALL IMPLIED WARRANTIES, INCLUDING WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, AND NON-INFRINGEMENT, TO THE MAXIMUM EXTENT PERMITTED BY LAW.",
      },
      {
        kind: "p",
        text: "We do not warrant that the Service will be uninterrupted, error-free, or that it will detect every anomaly, generate the optimal patch, or eliminate the need for human review. The Service is a powerful tool — not a substitute for engineering judgment.",
      },
    ],
  },
  {
    id: "liability",
    title: "Limitation of liability",
    body: [
      {
        kind: "p",
        text: "TO THE MAXIMUM EXTENT PERMITTED BY LAW, NEXIS’S TOTAL LIABILITY ARISING OUT OF OR RELATED TO THESE TERMS WILL NOT EXCEED THE AMOUNT PAID BY YOU TO NEXIS IN THE TWELVE MONTHS BEFORE THE CLAIM AROSE. NEXIS WILL NOT BE LIABLE FOR ANY INDIRECT, INCIDENTAL, CONSEQUENTIAL, OR PUNITIVE DAMAGES.",
      },
      {
        kind: "p",
        text: "Some jurisdictions do not allow the exclusion of certain warranties or the limitation of liability for consequential damages; those exclusions may not apply to you.",
      },
    ],
  },
  {
    id: "termination",
    title: "Termination",
    body: [
      {
        kind: "p",
        text: "You may terminate your Workspace at any time from /console/billing. Termination takes effect at the end of the current billing period; you remain responsible for any usage fees accrued during that period.",
      },
      {
        kind: "p",
        text: "We may suspend or terminate your access to the Service if you materially breach these Terms, fall behind on payment by more than 30 days, or pose a safety or security risk to the Service or its other users. We will provide reasonable notice except in cases of urgent risk.",
      },
    ],
  },
  {
    id: "law",
    title: "Governing law",
    body: [
      {
        kind: "p",
        text: "These Terms are governed by the laws of the State of Delaware, without regard to its conflict-of-law principles. Disputes will be brought in the state and federal courts located in Wilmington, Delaware, and you consent to personal jurisdiction there.",
      },
      {
        kind: "p",
        text: "For Enterprise customers, the governing law and venue may be modified by separate written agreement.",
      },
    ],
  },
  {
    id: "misc",
    title: "Miscellaneous",
    body: [
      {
        kind: "ul",
        items: [
          "Severability — if any provision is found unenforceable, the rest of these Terms remain in effect.",
          "Assignment — you may not assign these Terms without our written consent; we may assign them in connection with a merger, acquisition, or sale of substantially all of our assets.",
          "Entire agreement — these Terms, together with the Privacy Policy and any Order Form, constitute the entire agreement between you and NEXIS regarding the Service.",
          "Notices — we will send notices to the email address attached to your Account; you can send notices to legal@nexis.dev.",
        ],
      },
    ],
  },
];

export default function TermsPage() {
  return (
    <LegalPage
      eyebrow="LEGAL"
      title="Terms of service"
      effective="May 12, 2026"
      sections={SECTIONS}
    />
  );
}
