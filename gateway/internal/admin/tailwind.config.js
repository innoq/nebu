/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./templates/**/*.html'],
  plugins: [require('daisyui')],
  daisyui: {
    themes: [
      {
        obsidian: {
          // DaisyUI semantic token → Obsidian hex value (UX-DR1)
          "primary":          "#f97316",  // color-primary (buttons, active nav)
          "primary-content":  "#fff7ed",  // color-primary-text
          "secondary":        "#374151",  // bg-raised (secondary actions)
          "secondary-content":"#f9fafb",
          "accent":           "#f59e0b",  // status-warn (accent/highlight)
          "accent-content":   "#1f2937",
          "neutral":          "#374151",  // bg-raised
          "neutral-content":  "#9ca3af",
          "base-100":         "#111827",  // bg-base (page background)
          "base-200":         "#1f2937",  // bg-surface (cards, panels, sidebar)
          "base-300":         "#374151",  // bg-raised / bg-border (hover, overlays)
          "base-content":     "#f9fafb",  // text-primary
          "success":          "#22c55e",  // status-ok
          "success-content":  "#052e16",
          "warning":          "#f59e0b",  // status-warn
          "warning-content":  "#431407",
          "error":            "#ef4444",  // status-error
          "error-content":    "#450a0a",
          "info":             "#6b7280",  // status-neutral
          "info-content":     "#f9fafb",
        },
      },
    ],
    darkTheme: "obsidian",
    logs: false,
  },
  theme: {
    extend: {
      // Additional Obsidian tokens as CSS custom properties for direct use in templates
      colors: {
        "primary-hover":    "#ea580c",  // color-primary-hover
        "primary-subtle":   "#431407",  // color-primary-subtle
        "text-secondary":   "#9ca3af",  // text-secondary (meta, timestamps)
        "text-disabled":    "#4b5563",  // text-disabled
        "status-ok-bg":     "#052e16",
        "status-warn-bg":   "#431407",
        "status-error-bg":  "#450a0a",
      },
      // Typography scale from UX-DR2
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'monospace'],
      },
    },
  },
};
