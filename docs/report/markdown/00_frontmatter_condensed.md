```{=openxml}
<w:p><w:pPr><w:jc w:val="center"/></w:pPr></w:p>
<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:after="60"/></w:pPr><w:r><w:rPr><w:color w:val="2563EB"/><w:sz w:val="18"/><w:spacing w:val="40"/></w:rPr><w:t>NEXIS</w:t></w:r></w:p>
<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:after="60"/></w:pPr><w:r><w:rPr><w:sz w:val="28"/></w:rPr><w:t>University of Dhaka</w:t></w:r></w:p>
<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:after="400"/></w:pPr><w:r><w:rPr><w:sz w:val="28"/></w:rPr><w:t>Institute of Information Technology (IIT)</w:t></w:r></w:p>
<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:after="200"/></w:pPr><w:r><w:rPr><w:sz w:val="22"/></w:rPr><w:t>Project Report On</w:t></w:r></w:p>
<w:p><w:pPr><w:jc w:val="center"/><w:ind w:left="1300" w:right="1300"/><w:pBdr><w:bottom w:val="single" w:sz="16" w:color="2563EB" w:space="12"/></w:pBdr><w:spacing w:after="0"/></w:pPr></w:p>
<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:before="220" w:after="220"/></w:pPr><w:r><w:rPr><w:b/><w:sz w:val="30"/></w:rPr><w:t>&#8220;Nexis: A Multi-Agent Autonomous Engineering Platform with Closed-Loop Fault Recovery&#8221;</w:t></w:r></w:p>
<w:p><w:pPr><w:jc w:val="center"/><w:ind w:left="1300" w:right="1300"/><w:pBdr><w:bottom w:val="single" w:sz="16" w:color="2563EB" w:space="12"/></w:pBdr><w:spacing w:after="400"/></w:pPr></w:p>
<w:tbl>
<w:tblPr><w:tblStyle w:val="Table"/><w:tblW w:w="5000" w:type="dxa"/><w:jc w:val="center"/><w:tblBorders><w:top w:val="single" w:sz="4" w:color="D1D5DB"/><w:left w:val="single" w:sz="4" w:color="D1D5DB"/><w:bottom w:val="single" w:sz="4" w:color="D1D5DB"/><w:right w:val="single" w:sz="4" w:color="D1D5DB"/><w:insideH w:val="none"/><w:insideV w:val="none"/></w:tblBorders><w:tblCellMar><w:top w:w="180" w:type="dxa"/><w:bottom w:w="180" w:type="dxa"/><w:left w:w="220" w:type="dxa"/><w:right w:w="220" w:type="dxa"/></w:tblCellMar></w:tblPr>
<w:tblGrid><w:gridCol w:w="5000"/></w:tblGrid>
<w:tr><w:tc>
<w:tcPr><w:tcW w:w="5000" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="F7F9FC"/></w:tcPr>
<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:after="100"/></w:pPr><w:r><w:rPr><w:sz w:val="20"/><w:color w:val="6B7280"/></w:rPr><w:t>SUBMITTED BY</w:t></w:r></w:p>
<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:after="30"/></w:pPr><w:r><w:rPr><w:b/><w:sz w:val="22"/></w:rPr><w:t>Ratul Sikder</w:t></w:r></w:p>
<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:sz w:val="20"/></w:rPr><w:t>Exam Roll: 2506102</w:t></w:r></w:p>
</w:tc></w:tr>
</w:tbl>
<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:before="300" w:after="200"/></w:pPr><w:r><w:rPr><w:sz w:val="22"/></w:rPr><w:t>Supervised By: _______________________________</w:t></w:r></w:p>
<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:after="400"/></w:pPr><w:r><w:rPr><w:sz w:val="20"/></w:rPr><w:t>Institute of Information Technology (IIT), University of Dhaka</w:t></w:r></w:p>
<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:sz w:val="20"/><w:color w:val="6B7280"/></w:rPr><w:t>Date: 11th August, 2026 &#8212; Condensed Edition</w:t></w:r></w:p>
<w:p><w:r><w:br w:type="page"/></w:r></w:p>
```

**Declaration.** This is to declare that this project is my original work. No part of this work has been submitted elsewhere, partially or entirely, for the award of any other degree or diploma.

**Signature Page.**

| | | |
|---|---|---|
| Project | : | Nexis: A Multi-Agent Autonomous Engineering Platform with Closed-Loop Fault Recovery |
| Student Name | : | Ratul Sikder |
| Exam Roll | : | 2506102 |
| Institute | : | Institute of Information Technology (IIT), University of Dhaka |
| Date of Submission | : | 11th August, 2026 |
| Supervised By | : | _______________________________ |
| **Supervisor's Approval** | : | _______________________________ |

# Abstract

Modern software engineering teams spend an estimated 40–60% of their time on maintenance, debugging, and manual incident response rather than building new capability. Existing AI-assisted developer tools — code completion, automated test generation, deployment dashboards — address individual tasks in isolation; none model the collaborative structure of a real engineering team, and none close the loop between detecting a production failure and autonomously repairing it. **Nexis** addresses both gaps at once. It models a software engineering organisation as nine role-specialised autonomous agents across two cooperating layers — a five-agent Execution Team (Architect, Backend, Quality Assurance (QA), DevOps, Data Engineer) and a four-agent Self-Healing Loop (Sentinel, Pathfinder, Synthesiser, Validator) — durably orchestrated by Temporal and gated by a severity-routed human-in-the-loop Approval Gate that supports Approve, Reject, and Modify (Reinforcement Learning from Human Feedback (RLHF)) decisions. This report documents the complete, working system: a multi-tenant control plane (Go (Golang), PostgreSQL with Row-Level Security (RLS), Neo4j, Redis), a Docker shadow-validation sandbox, a real GitHub-App-mediated GitOps deployment path, a from-scratch Docker preview-deploy engine, and a Next.js console spanning authentication, Role-Based Access Control (RBAC), incident observability, billing, and multi-provider integrations. Two live end-to-end recovery runs against a local Large Language Model (LLM) are reported verbatim, alongside a live, verified proof of the preview-deploy engine. What is deliberately out of scope — a fully credentialed production deployment to the registered `nexis.sh` domain, and the human-subject NASA Task Load Index (NASA-TLX) study — is stated plainly rather than implied.

**Keywords:** Multi-agent systems, autonomous software engineering, self-healing systems, closed-loop fault recovery, root-cause analysis, LLM-guided program repair, DevOps automation, Temporal workflow orchestration, human-in-the-loop AI.

*This is the condensed edition of the full project report. The complete 107-page edition with exhaustive feature-by-feature screenshots is available as a companion document.*

```{=openxml}
<w:p><w:r><w:br w:type="page"/></w:r></w:p>
```
