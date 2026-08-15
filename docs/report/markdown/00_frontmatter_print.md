```{=openxml}
<w:p><w:pPr><w:spacing w:after="500"/></w:pPr></w:p>
<w:p><w:pPr><w:spacing w:after="400"/></w:pPr><w:r><w:rPr><w:b/><w:sz w:val="28"/></w:rPr><w:t>Nexis: A Multi-Agent Autonomous Engineering Platform with Closed-Loop Fault Recovery</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="200"/></w:pPr><w:r><w:t>by</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="400"/></w:pPr><w:r><w:rPr><w:sz w:val="22"/></w:rPr><w:t>Ratul Sikder</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="40"/></w:pPr><w:r><w:t>Submitted to the Institute of Information Technology (IIT)</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="400"/></w:pPr><w:r><w:t>in partial fulfillment of the degree requirements</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="40"/></w:pPr><w:r><w:t>at the</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="200"/></w:pPr><w:r><w:rPr><w:sz w:val="22"/></w:rPr><w:t>UNIVERSITY OF DHAKA</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="500"/></w:pPr><w:r><w:t xml:space="preserve">August 2026 </w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="500"/></w:pPr><w:r><w:t>&#169; University of Dhaka 2026. All rights reserved.</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="0"/></w:pPr><w:r><w:rPr><w:i/><w:sz w:val="18"/></w:rPr><w:t>The author hereby grants the Institute of Information Technology (IIT), University of Dhaka, permission to reproduce and distribute copies of this project report, in whole or in part, for academic purposes.</w:t></w:r></w:p>
<w:p><w:r><w:br w:type="page"/></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="400"/></w:pPr><w:r><w:rPr><w:b/><w:sz w:val="23"/></w:rPr><w:t>Nexis: A Multi-Agent Autonomous Engineering Platform with Closed-Loop Fault Recovery</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="150"/></w:pPr><w:r><w:t>by</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="300"/></w:pPr><w:r><w:rPr><w:sz w:val="22"/></w:rPr><w:t>Ratul Sikder</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="40"/></w:pPr><w:r><w:t>Submitted to the Institute of Information Technology (IIT) on 11 August 2026,</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="350"/></w:pPr><w:r><w:t>in partial fulfillment of the degree requirements</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="400"/></w:pPr><w:r><w:rPr><w:sz w:val="19"/></w:rPr><w:t>This is to declare that this project is the author's original work. No part of it has been submitted elsewhere, in whole or in part, for the award of any other degree or diploma, and the plagiarism policy stated by the supervisor has been maintained.</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="20"/></w:pPr><w:r><w:t>Certified by ...................................................................................</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="20"/><w:ind w:left="600" w:firstLine="0"/></w:pPr><w:r><w:rPr><w:sz w:val="18"/></w:rPr><w:t>_______________________________</w:t></w:r></w:p>
<w:p><w:pPr><w:spacing w:after="0"/><w:ind w:left="600" w:firstLine="0"/></w:pPr><w:r><w:rPr><w:sz w:val="18"/></w:rPr><w:t>Project Supervisor</w:t></w:r></w:p>
```

# Abstract

Modern software engineering teams spend an estimated 40 to 60 percent of their time on maintenance, debugging, and manual incident response, rather than on building new capability. A number of AI-assisted developer tools already exist, such as code completion assistants, automated test generators, and deployment dashboards, but each of these addresses only an individual task in isolation. None of them model the collaborative structure of a real engineering team, and none of them close the loop between detecting a production failure and actually repairing it. **Nexis** has been designed to address both of these gaps together. The system models a software engineering organisation as nine role-specialised autonomous agents, arranged across two cooperating layers: a five-agent Execution Team (Architect, Backend, Quality Assurance (QA), DevOps, and Data Engineer) and a four-agent Self-Healing Loop (Sentinel, Pathfinder, Synthesiser, and Validator). These agents are durably orchestrated using Temporal, and every automated repair is gated by a severity-routed, human-in-the-loop Approval Gate that supports Approve, Reject, and Modify (Reinforcement Learning from Human Feedback (RLHF)) decisions. This report documents the complete, working system, which includes a multi-tenant control plane built with Go (Golang), PostgreSQL with Row-Level Security (RLS), Neo4j, and Redis; a Docker shadow-validation sandbox; a real GitHub-App-mediated GitOps deployment path; and a from-scratch Docker preview-deploy engine. It also includes a Next.js console that spans authentication, Role-Based Access Control (RBAC), incident observability, billing, and multi-provider integrations. Two live end-to-end recovery runs against a local Large Language Model (LLM) have been reported verbatim, alongside a live and verified proof of the preview-deploy engine. What has been deliberately left out of scope, namely a fully credentialed production deployment to the registered `nexis.sh` domain and the human-subject NASA Task Load Index (NASA-TLX) study, has been stated plainly rather than left implied.

**Keywords:** Multi-agent systems, autonomous software engineering, self-healing systems, closed-loop fault recovery, root-cause analysis, LLM-guided program repair, DevOps automation, Temporal workflow orchestration, human-in-the-loop AI.
