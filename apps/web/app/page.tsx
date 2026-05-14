// Marketing landing. New section order matches the spec — Hero / TrustedStrip
// / Problem / Comparison / HowItWorks / Agents / Testimonials / LiveDemo /
// IntegrationsTeaser / Metrics / FAQ / CTA. Server component so the Hero can
// read the nexis_session cookie to render the "Skip to dashboard" link for
// already-signed-in visitors.
import { cookies } from "next/headers";

import Footer from "@/components/layout/Footer";
import Navbar from "@/components/layout/Navbar";
import Agents from "@/components/sections/Agents";
import Comparison from "@/components/sections/Comparison";
import CTA from "@/components/sections/CTA";
import FAQTeaser from "@/components/sections/FAQTeaser";
import Hero from "@/components/sections/Hero";
import HowItWorks from "@/components/sections/HowItWorks";
import IntegrationsTeaser from "@/components/sections/IntegrationsTeaser";
import LiveDemoEmbed from "@/components/sections/LiveDemoEmbed";
import Metrics from "@/components/sections/Metrics";
import Problem from "@/components/sections/Problem";
import Testimonials from "@/components/sections/Testimonials";
import TrustedStrip from "@/components/sections/TrustedStrip";

export default async function Page() {
  const c = await cookies();
  const signedIn = Boolean(c.get("nexis_session"));

  return (
    <>
      <Navbar />
      <main id="main">
        <Hero signedIn={signedIn} />
        <TrustedStrip />
        <Problem />
        <Comparison />
        <HowItWorks />
        <Agents />
        <Testimonials />
        <LiveDemoEmbed />
        <IntegrationsTeaser />
        <Metrics />
        <FAQTeaser />
        <CTA />
      </main>
      <Footer />
    </>
  );
}
