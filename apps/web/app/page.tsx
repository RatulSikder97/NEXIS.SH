import Footer from "@/components/layout/Footer";
import Navbar from "@/components/layout/Navbar";
import Hero from "@/components/sections/Hero";
import TrustedStrip from "@/components/sections/TrustedStrip";
import Problem from "@/components/sections/Problem";
import HowItWorks from "@/components/sections/HowItWorks";
import Agents from "@/components/sections/Agents";
import LiveDemoEmbed from "@/components/sections/LiveDemoEmbed";
import Metrics from "@/components/sections/Metrics";
import CTA from "@/components/sections/CTA";

export default function Page() {
  return (
    <>
      <Navbar />
      <main>
        <Hero />
        <TrustedStrip />
        <Problem />
        <HowItWorks />
        <Agents />
        <LiveDemoEmbed />
        <Metrics />
        <CTA />
      </main>
      <Footer />
    </>
  );
}
