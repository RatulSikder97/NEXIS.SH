import { getSiteUrl } from "@/lib/site";

const DESCRIPTION =
  "Nexis is autonomous engineering software that detects CI/CD and production pipeline failures, diagnoses root cause, synthesizes verified patches, and queues them for human approval—cutting MTTR for platform and SRE teams.";

export function JsonLd() {
  const url = getSiteUrl();

  const graph = [
    {
      "@type": "WebSite",
      "@id": `${url}/#website`,
      name: "Nexis",
      url,
      description: DESCRIPTION,
      inLanguage: "en-US",
      publisher: { "@id": `${url}/#organization` },
    },
    {
      "@type": "Organization",
      "@id": `${url}/#organization`,
      name: "Nexis",
      url,
      logo: `${url}/logo.svg`,
    },
    {
      "@type": "SoftwareApplication",
      "@id": `${url}/#software`,
      name: "Nexis",
      applicationCategory: "DeveloperApplication",
      operatingSystem: "Web",
      description: DESCRIPTION,
      url,
      offers: {
        "@type": "Offer",
        price: "0",
        priceCurrency: "USD",
        availability: "https://schema.org/PreOrder",
        description: "Early access program",
      },
      publisher: { "@id": `${url}/#organization` },
    },
  ];

  const payload = {
    "@context": "https://schema.org",
    "@graph": graph,
  };

  return (
    <script
      type="application/ld+json"
      dangerouslySetInnerHTML={{ __html: JSON.stringify(payload) }}
    />
  );
}
