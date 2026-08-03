/** GREMLYN_OS Design System — Color Tokens */

export const colors = {
  surface: {
    containerLowest: "#000000",
    background: "#0e0e0e",
    containerLow: "#131313",
    container: "#1a1919",
    containerHigh: "#201f1f",
    containerHighest: "#262626",
    bright: "#2c2c2c",
  },
  primary: "#8eff71",
  primaryDim: "#2be800",
  primaryFixed: "#2ff801",
  secondary: "#ff7168",
  secondaryDim: "#e2242a",
  secondaryContainer: "#c00018",
  tertiary: "#83ff95",
  tertiaryDim: "#3be66b",
  error: "#ff7351",
  onSurface: "#ffffff",
  onSurfaceVariant: "#adaaaa",
  outline: "#777575",
  outlineVariant: "#494847",
  onPrimary: "#0d6100",
  onSecondary: "#4a0004",
} as const;

export type AccentColor = "primary" | "secondary";
