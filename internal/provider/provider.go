package provider

import (
	"context"

	cloudflare "github.com/cloudflare/cloudflare-go/v7"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	tfprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

type cfSubdomainProvider struct{}

var _ tfprovider.Provider = (*cfSubdomainProvider)(nil)

func New() tfprovider.Provider {
	return &cfSubdomainProvider{}
}

func (p *cfSubdomainProvider) Metadata(_ context.Context, _ tfprovider.MetadataRequest, resp *tfprovider.MetadataResponse) {
	resp.TypeName = "cfsubdomain"
}

func (p *cfSubdomainProvider) Schema(_ context.Context, _ tfprovider.SchemaRequest, resp *tfprovider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the account-level workers.dev subdomain. " +
			"Credentials are resolved from the environment exactly like the official " +
			"cloudflare provider (CLOUDFLARE_API_TOKEN, CLOUDFLARE_API_KEY+CLOUDFLARE_EMAIL, " +
			"CLOUDFLARE_API_USER_SERVICE_KEY).",
	}
}

func (p *cfSubdomainProvider) Configure(_ context.Context, _ tfprovider.ConfigureRequest, resp *tfprovider.ConfigureResponse) {
	// cloudflare.NewClient() が CLOUDFLARE_API_TOKEN /
	// CLOUDFLARE_API_KEY + CLOUDFLARE_EMAIL /
	// CLOUDFLARE_API_USER_SERVICE_KEY を自動で読む。
	// 認証方式の分岐はすべてSDK任せ。
	client := cloudflare.NewClient()
	resp.DataSourceData = client
}

func (p *cfSubdomainProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewWorkersSubdomainDataSource,
	}
}

func (p *cfSubdomainProvider) Resources(_ context.Context) []func() resource.Resource {
	return nil
}
