"use client";

import dynamic from "next/dynamic";

const Problem = dynamic(() => import("@/components/sections/Problem"), {
  ssr: false,
});
const HowItWorks = dynamic(
  () => import("@/components/sections/HowItWorks"),
  {
    ssr: false,
  }
);
const Agents = dynamic(() => import("@/components/sections/Agents"), {
  ssr: false,
});
const Metrics = dynamic(() => import("@/components/sections/Metrics"), {
  ssr: false,
});
const CTA = dynamic(() => import("@/components/sections/CTA"), {
  ssr: false,
});

export default function BelowFold() {
  return (
    <>
      <Problem />
      <HowItWorks />
      <Agents />
      <Metrics />
      <CTA />
    </>
  );
}

