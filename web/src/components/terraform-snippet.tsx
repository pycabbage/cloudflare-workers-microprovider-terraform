import { codeToHtml } from "shiki"

/**
 * The canonical usage snippet for this provider, faithfully reproduced from
 * docs/provider/usage.md.
 */
const HCL_SOURCE = `terraform {
  required_providers {
    cfworkers = {
      source = "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider"
    }
  }
}

provider "cfworkers" {}`

/**
 * Renders the HCL usage snippet with build-time syntax highlighting via
 * Shiki. This is an async Server Component: `codeToHtml` runs once during
 * the static export build, and the resulting HTML (a `<pre><code>` tree
 * with per-token `--shiki-light`/`--shiki-dark` CSS variables, no client JS
 * shipped) is embedded directly via `dangerouslySetInnerHTML`.
 *
 * Dual light/dark themes are baked in via Shiki's CSS-variable mechanism
 * (`themes: { light, dark }` with `defaultColor: false`, so no inline
 * `color` is set and Shiki's own background is omitted entirely, leaving
 * the surrounding `bg-muted/50` wrapper to blend with the shadcn/ui Card).
 * The small stylesheet below maps those variables to `color`, toggled by
 * the `.dark` class selector to match this project's class-based dark mode
 * strategy (see `@custom-variant dark` in globals.css).
 */
export async function TerraformSnippet() {
  const html = await codeToHtml(HCL_SOURCE, {
    lang: "hcl",
    themes: {
      light: "github-light",
      dark: "github-dark",
    },
    defaultColor: false,
  })

  return (
    <div className="tf-snippet overflow-x-auto rounded-lg bg-muted/50 p-4 text-sm leading-relaxed [&_pre]:font-mono">
      <style>{`
        .tf-snippet .shiki,
        .tf-snippet .shiki span {
          color: var(--shiki-light);
        }
        .dark .tf-snippet .shiki,
        .dark .tf-snippet .shiki span {
          color: var(--shiki-dark);
        }
      `}</style>
      <div dangerouslySetInnerHTML={{ __html: html }} />
    </div>
  )
}
