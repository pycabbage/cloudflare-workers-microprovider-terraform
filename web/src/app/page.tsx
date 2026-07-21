import { ExternalLink } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TerraformSnippet } from "@/components/terraform-snippet"

const REPO_URL = "https://github.com/pycabbage/cloudflare-workers-microprovider-terraform"

export default function Home() {
  return (
    <div className="flex flex-1 flex-col items-center bg-zinc-50 dark:bg-black">
      <main className="flex w-full max-w-2xl flex-1 flex-col gap-10 px-6 py-16 sm:py-24">
        <header className="flex flex-col gap-4">
          <Badge variant="secondary" className="w-fit font-mono">
            cloudflare-workers-microprovider
          </Badge>
          <h1 className="text-3xl font-semibold tracking-tight text-black sm:text-4xl dark:text-zinc-50">
            A Terraform provider for one thing:{" "}
            <span className="text-zinc-500 dark:text-zinc-400">workers.dev</span>
          </h1>
          <a
            href={REPO_URL}
            className="inline-flex w-fit items-center gap-1.5 text-sm font-medium text-zinc-950 hover:underline dark:text-zinc-50"
          >
            View on GitHub
            <ExternalLink className="size-3.5" />
          </a>
        </header>

        <Card>
          <CardHeader>
            <CardTitle>Usage</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <TerraformSnippet />
            <p className="text-sm text-muted-foreground">
              Requires Terraform &gt;= 1.1 (plugin protocol version 6).
            </p>
          </CardContent>
        </Card>

        <footer className="mt-auto flex flex-col gap-1 border-t border-zinc-200 pt-6 text-xs text-zinc-500 dark:border-zinc-800 dark:text-zinc-500">
          <span>github.com/pycabbage/cloudflare-workers-microprovider-terraform</span>
        </footer>
      </main>
    </div>
  )
}
