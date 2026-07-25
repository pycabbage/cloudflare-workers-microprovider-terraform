package provider

import (
	"context"

	cloudflare "github.com/cloudflare/cloudflare-go/v7"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	tfprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

type cfWorkersProvider struct{}

var (
	_ tfprovider.Provider              = (*cfWorkersProvider)(nil)
	_ tfprovider.ProviderWithFunctions = (*cfWorkersProvider)(nil)
)

func New() tfprovider.Provider {
	return &cfWorkersProvider{}
}

func (p *cfWorkersProvider) Metadata(_ context.Context, _ tfprovider.MetadataRequest, resp *tfprovider.MetadataResponse) {
	resp.TypeName = "cfworkers"
}

func (p *cfWorkersProvider) Schema(_ context.Context, _ tfprovider.SchemaRequest, resp *tfprovider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Provides two independent capabilities: a cfworkers_subdomain data " +
			"source that reads the account-level workers.dev subdomain via the Cloudflare " +
			"API (credentials resolved from the environment exactly like the official " +
			"cloudflare provider - CLOUDFLARE_API_TOKEN, CLOUDFLARE_API_KEY+CLOUDFLARE_EMAIL, " +
			"or CLOUDFLARE_API_USER_SERVICE_KEY), and a jsonc_decode function that parses " +
			"JSON-with-comments (JSONC) text, such as a wrangler.jsonc read via file(), into " +
			"a decoded value - pure parsing, no credentials or API calls involved.",
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

func (p *cfWorkersProvider) Functions(_ context.Context) []func() function.Function {
	return []func() function.Function{
		NewJSONCDecodeFunction,
	}
}
