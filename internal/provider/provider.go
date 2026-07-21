package provider

import (
	"context"

	cloudflare "github.com/cloudflare/cloudflare-go/v7"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	tfprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

type cfWorkersProvider struct{}

var _ tfprovider.Provider = (*cfWorkersProvider)(nil)

func New() tfprovider.Provider {
	return &cfWorkersProvider{}
}

func (p *cfWorkersProvider) Metadata(_ context.Context, _ tfprovider.MetadataRequest, resp *tfprovider.MetadataResponse) {
	resp.TypeName = "cfworkers"
}

func (p *cfWorkersProvider) Schema(_ context.Context, _ tfprovider.SchemaRequest, resp *tfprovider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the account-level workers.dev subdomain. " +
			"Credentials are resolved from the environment exactly like the official " +
			"cloudflare provider (CLOUDFLARE_API_TOKEN, CLOUDFLARE_API_KEY+CLOUDFLARE_EMAIL, " +
			"CLOUDFLARE_API_USER_SERVICE_KEY).",
	}
}

func (p *cfWorkersProvider) Configure(_ context.Context, _ tfprovider.ConfigureRequest, resp *tfprovider.ConfigureResponse) {
	client := cloudflare.NewClient()
	resp.DataSourceData = client
}

func (p *cfWorkersProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewWorkersSubdomainDataSource,
	}
}

func (p *cfWorkersProvider) Resources(_ context.Context) []func() resource.Resource {
	return nil
}
