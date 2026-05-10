/** Shared motion tokens — transform + opacity only for GPU-friendly defaults */

export const easeSoftOut = [0.22, 1, 0.36, 1] as const;

export const reveal = {
  hidden: { opacity: 0, y: 22 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.52, ease: easeSoftOut },
  },
} as const;

export const staggerPresets = {
  hero: {
    hidden: {},
    visible: {
      transition: {
        staggerChildren: 0.065,
        delayChildren: 0.2,
      },
    },
  },
  section: {
    hidden: {},
    visible: {
      transition: {
        staggerChildren: 0.075,
        delayChildren: 0.08,
      },
    },
  },
} as const;

export type StaggerPreset = keyof typeof staggerPresets;
